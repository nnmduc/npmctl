# npmctl

A single-binary CLI for [Nginx Proxy Manager](https://nginxproxymanager.com), built so an
AI coding agent can drive it without quietly breaking production.

Written against **NPM 2.15.1**. The OpenAPI document is vendored, and
`npmctl schema check` tells you when a newer NPM has drifted from it.

```bash
npmctl host list
npmctl host get app.example.com
npmctl host update app.example.com --forward-port 8081 --dry-run
```

## Why it exists

Nginx Proxy Manager has a perfectly good web UI. What it does not have is a way to let a
script — or an agent — make changes with guardrails. A proxy host is somebody's website,
and deleting a Let's Encrypt certificate performs a real revocation.

So the design priority here is **safety over features**:

- Every mutation needs two factors, one of them outside `argv`.
- `--dry-run` previews any write and sends no mutating request.
- Every mutation writes a pre-image first, and `npmctl undo` replays it.
- Mutating HTTP methods are never retried.
- Exit codes are a stable contract, so an agent can tell "refused" from "failed".

## Install

```bash
go install github.com/nnmduc/npmctl/cmd/npmctl@latest
```

Or download a binary from [Releases](https://github.com/nnmduc/npmctl/releases). Builds
are static (`CGO_ENABLED=0`) for macOS (arm64, amd64), Linux (amd64, arm64) and Windows
(amd64).

## Quickstart

```bash
# Log in once. A 30-day token is minted and stored; the password is discarded.
npmctl auth login --url https://npm.example.com

npmctl auth status        # profile, identity, credential backend, expiry
npmctl health             # is the instance up
npmctl host list          # table on a terminal, JSON when piped
```

Writes need both factors:

```bash
export NPMCTL_ALLOW_WRITE=1
npmctl host create --domain app.example.com \
  --forward-host 10.0.0.9 --forward-port 8080 --yes
```

## The write gate

A mutation runs only with **both** `NPMCTL_ALLOW_WRITE=1` in the environment and `--yes`
on the command line. Missing either one exits **3**.

`--yes` alone is four characters an agent can type. The environment variable is not part
of the command line, so an agent composing a command cannot supply it — which keeps the
host tool's permission prompt in front of a human.

Every mutation passes through one gate, in this order:

1. **Resolve and preview** — reads to turn a domain into an ID and fetch current state.
2. **Dependency scan** — before any delete. Refuses without `--cascade-ack` when other
   objects reference the target.
3. **Authorize** — both factors, plus per-tier extras.
4. **Compare-and-swap** — re-reads immediately before writing and aborts if `modified_on`
   moved. NPM offers no ETag, so this is the only way to notice a concurrent edit.
5. **Capture a pre-image** — written to the undo journal *before* the request.
6. **Execute.**
7. **Verify** — re-reads and checks `meta.nginx_online`. NPM answers 200 for a write whose
   nginx reload failed; that case exits **8** and prints `nginx_err` verbatim.

| Tier | Applies to | Additional requirement |
|---|---|---|
| normal | create, update, enable, disable | none beyond the two factors |
| destructive | any delete, including `cert rm` | dependency scan, `--cascade-ack` when dependents exist |
| privileged | `advanced_config` writes, `undo apply` | an interactive terminal and a typed confirmation |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic error |
| 2 | usage error |
| 3 | **refused** — a safety gate declined; do not retry blindly |
| 4 | auth failure |
| 5 | not found |
| 6 | NPM returned an error |
| 7 | network error — **the write may have applied**; verify with a read |
| 8 | **applied, but nginx reload failed** — the site may be down |
| 9 | interactive re-authentication required |

## Undo

Every mutation records the prior state before it runs.

```bash
npmctl undo list             # recent pre-images, newest first
npmctl undo show <entry>     # render one (redacted for display)
npmctl undo apply <entry>    # replay it — this is a WRITE and re-enters the gate
```

`undo apply` gets no exemption: two factors, compare-and-swap, its own pre-image, and the
post-write health check all still apply.

**One thing undo cannot reverse:** deleting a Let's Encrypt certificate revokes it with
the certificate authority. The journal restores the database row; the certificate material
is gone.

## Certificates and rate limits

Let's Encrypt allows 5 duplicate certificates per week. npmctl:

- never retries a certificate operation automatically;
- records attempts per domain set and refuses a 4th within 7 days (`--force` overrides);
- reports one of `ISSUED`, `NOT PRESENT` or `INDETERMINATE` — never "may have succeeded",
  because an ambiguous answer is what makes people retry.

## Honest threat model

Read this before pointing npmctl at production.

**What the gates protect against:** accident and casual error. A mistyped delete, an agent
that misunderstands a request, a dry-run misread as a completed write.

**What they do not protect against:** an attacker, or an agent, that already has your
credential. Anything running as your user can read the token from the keyring or the 0600
file and call the NPM API directly. No CLI-side gate can prevent that, and this tool does
not claim otherwise.

**The only real boundary is server-side.** Create a dedicated NPM user with only the
permissions the task needs, and use that identity for automation:

1. In the NPM web UI, add a user for automation.
2. Grant only the permissions it requires (for example proxy hosts, but not certificates).
3. `npmctl -p automation auth login --url https://npm.example.com --identity that-user@example.com`

Then a mistake — or a compromise — is bounded by what that account can do, rather than by
what a CLI flag chose to allow.

### Sensitive files at rest

| Path | Contents |
|---|---|
| OS keyring, or `~/.config/npmctl/credentials.json` | the bearer token, mode 0600 |
| `~/.local/state/npmctl/undo/` | pre-images, mode 0600, **unredacted**, swept after 30 days |
| `~/.local/share/npmctl/certs/` | downloaded archives, mode 0600, **including private keys** |

The undo journal is deliberately **not** scrubbed. A pre-image containing `[redacted]`
could not be restored, which would defeat its only purpose — so a certificate pre-image
holds `dns_provider_credentials` in plaintext. That is the same trust level as the
credential file sitting beside it. Entries older than 30 days are deleted on the next run.

Secrets *are* redacted everywhere they could be observed: every renderer, `--dry-run`
bodies, and the `-v` transport log.

### `advanced_config`

`advanced_config` accepts raw nginx directives with no validation beyond `nginx -t`. A
valid-but-hostile block can serve NPM's `/data` volume over HTTP, which contains
`database.sqlite` and `keys.json` — the key that signs every token. npmctl therefore
requires a separate `--allow-advanced-config` flag, an interactive terminal, and shows a
line diff first. The Agent Skill prohibits it outright.

## TLS to your NPM instance

A self-signed NPM certificate does not require turning verification off:

```bash
npmctl --ca-cert ~/.config/npmctl/npm-ca.pem host list
npmctl --pin-sha256 <base64-sha256-of-public-key> host list
```

`--insecure` exists, is per-profile, and is refused for `auth login` — sending a password
over a connection you declined to verify is exactly the case an interceptor wants.

## Profiles

```yaml
# ~/.config/npmctl/config.yaml
default_profile: prod
profiles:
  prod:
    url: https://npm.example.com
    identity: me@example.com
  lab:
    url: https://127.0.0.1:18181
    identity: lab-admin@npmctl.test    # a distinct identity — never share prod creds
    ca_cert: ~/.config/npmctl/lab-ca.pem
```

Credentials are keyed by `(profile, url, identity)`. Repointing a profile at a new URL
invalidates its stored credential rather than replaying it against a different host.

## Use with AI agents

```bash
# Auto-detects installed agents (Claude Code, Antigravity/AGY, Codex, OpenCode, Cursor, Gemini CLI)
npmctl skill install

# Target specific agent(s)
npmctl skill install --agent agy,codex
npmctl skill install --all

# Install to project workspace (.agents/skills) and append pointer to AGENTS.md
npmctl skill install --project --agents-md ./AGENTS.md
```

The skill pre-approves **read commands and `--dry-run` only**. No write is pre-approved,
so each one re-enters the agent's normal permission flow and a human sees it. Installing is
idempotent and never overwrites files you have edited — it reports them instead.

The command reference is generated from the binary by `npmctl docs`, so it cannot drift.

## Not in v1

User management (`/users`), `login-as`, password changes and `settings set` are **absent
from the binary**, not merely hidden. A command that does not exist cannot be mis-gated or
reached by an agent that ignores its instructions. `settings list|get` are available.

## Development

```bash
make build      # static binary
make test       # hermetic: no network, no Docker
make lint       # go vet + gofmt check
```

The test suite is hermetic by default. An opt-in smoke test runs against a disposable NPM
instance — see [docs/lab-instance.md](docs/lab-instance.md). Please exercise changes there
before production; several behavioural contracts (silent TLS flag coercion, `expand`
encoding, access lists never returning passwords) are invisible to the schema and were
found only by running against a real instance.

## License

MIT — see [LICENSE](LICENSE).
