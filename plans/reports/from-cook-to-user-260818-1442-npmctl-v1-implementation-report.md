# npmctl v1 — implementation report

**Plan:** `plans/260818-0858-npmctl-nginx-proxy-manager-cli/plan.md` (4 phases, all complete)
**Repo:** https://github.com/nnmduc/npmctl — **private**, `main`, 3 commits
**Verified against:** live NPM 2.15.1 (disposable Docker lab, since removed)

## Result

| Metric | Value |
|---|---|
| Operations implemented | 54 of 68 (14 deliberately deferred) |
| Tests | 231 passing, hermetic (no network, no Docker) |
| Packages | 12 |
| Source / test LOC | 9,435 / 4,948 |
| `go vet`, `gofmt` | clean |
| Files over 200 LOC | none |
| Release targets built | 5/5 static, `CGO_ENABLED=0` |

`npmctl schema check` against the live instance: **68/68 operations, zero findings**.

## Safety controls, each with a test

- Two-factor write gate: `NPMCTL_ALLOW_WRITE=1` **and** `--yes`; either alone exits 3.
- `--dry-run` issues no mutating request — asserted by HTTP method, not request count.
- Pre-image captured before every mutation; `undo apply` replays it through the same gate.
- Mutating methods never retried (counting RoundTripper test).
- Compare-and-swap on `modified_on` (no ETag exists).
- Post-write nginx verification → exit 8 with `nginx_err` verbatim.
- Redaction in the serializer, so all renderers, dry-run bodies and `-v` are covered.
- Credentials keyed by `(profile, url, identity)`; 30-day token stored, not the password.
- 8-concurrent-process credential-file test (flock + atomic rename).

## Found only by running against real NPM

Four defects fixtures could not have caught:

1. **`?expand` silently dropped.** NPM parses it as `typeof req.query.expand === "string"`.
   Repeated `expand=a&expand=b` becomes an array, fails the check, and is ignored — no
   error, just missing data. Now one comma-separated value.
2. **NPM silently forces four TLS flags off** (`cleanSslHstsData`): no certificate clears
   `ssl_forced` and `http2_support`; no `ssl_forced` clears `hsts_enabled`; no HSTS clears
   `hsts_subdomains` — still returning 200. npmctl now compares request against response and
   warns per flag with the missing prerequisite. **New control, not in the plan**, added
   because a silent no-op on HSTS is a correctness problem.
3. **`stream create` forwarded nothing** — `tcp_forwarding` was omitted unless the flag was
   explicitly set, so NPM defaulted it false against documented behaviour.
4. **NPM 2.15.1 seeds no default admin** — needs `INITIAL_ADMIN_EMAIL` /
   `INITIAL_ADMIN_PASSWORD`. The plan's lab recipe would not have worked.

Two defects in npmctl's own code, caught by its tests: an env-supplied token treated as
expired (would have broken all CI use), and a keyring probe that *wrote* to the OS keychain
on every run, raising a GUI prompt under a test HOME.

## Added beyond the plan

- **Plaintext credential guard.** `auth login` refuses to send a password over `http://` or
  with TLS verification off, unless `--allow-plaintext` is passed. Added on learning the
  target instance is `http://10.161.206.88:81` — otherwise the admin password and every
  subsequent bearer token cross the network in cleartext.
- **TLS coercion warning** (item 2 above).

## Deviations

| Planned | Shipped | Why |
|---|---|---|
| `testdata/schema-2.15.1.json` | `internal/schemadata/` | `schema check` needs it at runtime; `go:embed` cannot reach outside its package. One source, not two. |
| `skill/` at repo root | `internal/skill/files/` | Same constraint. |
| Repo public (D2/V2) | **private** | `plans/` names an internal production server. Reversible; public is one command. |

A project-local `.claude/.ckignore` was added: the shared baseline matches `build` and
`target` anywhere in a Bash command, blocking `go build`.

## Not verified

1. **Agent-driven acceptance** — whether Claude Code drives a full lifecycle from `SKILL.md`
   alone with a prompt per write. Mechanical parts are asserted; end-to-end needs a human
   session.
2. **Real ACME issuance** — Let's Encrypt cannot validate `.test`. All three issuance states
   are fixture-tested and the cooldown was exercised live, but no real order was placed.
3. **`goreleaser` run and `go install` from the repo** — need a pushed tag.

## Open questions

1. Make the repo public? It needs `plans/` removed or accepted first, and enables the
   Homebrew tap in `.goreleaser.yaml` (which points at `nnmduc/homebrew-tap`).
2. `~/go/bin` is **not** on your `PATH`, so `npmctl` is installed but not callable by name.
3. First production login will be refused until you pass `--allow-plaintext`, because the
   target is HTTP. Intended — confirm you accept cleartext credentials on that network, or
   put NPM behind HTTPS.
