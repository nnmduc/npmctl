---
phase: 1
title: "Core Foundation"
status: completed
priority: P1
effort: "3-4 days"
dependencies: []
---

# Phase 1: Core Foundation

# [RED TEAM] Findings 3,4,5,6,7,8,9,12,13,14 applied 2026-08-18. R1 meta-builder machinery REMOVED — see plan.md § Corrections.
<!-- Updated: Validation Session 1 — V1 module path fixed; V5 `npmctl undo` restore added; V6 journal retention added; V3 Privileged tier trimmed -->

## Overview

Scaffold the repo and build every cross-cutting concern: HTTP client, credential handling, config/profiles, output rendering with redaction, the write gate, and the undo journal. Prove it end-to-end by shipping `auth`, `host` (proxy-hosts), and `version`.

Everything in P2/P3 repeats the patterns established here. Get these right or the repetition amplifies the mistakes — the red team found two invalid request payloads in P2 that came from transcribing prose instead of the schema.

## Requirements

**Functional**
- `npmctl auth login|logout|status|whoami` — incl. 2FA challenge, 30d token minting
- `npmctl host list|get|create|update|rm|enable|disable`
- `npmctl version` (binary version + `GET /api/version/check`) and `GET /` health pre-flight
- `npmctl undo list|show|apply` — replay a captured pre-image back through `Guard` (V5)
- Profiles: multiple NPM instances, `-p/--profile`, credentials keyed per profile
- Output: table on TTY, JSON when piped, `-o json|table|yaml` override, **redaction in the serializer**

**Non-functional**
- Every file < 200 LOC
- No network in tests
- **No secret reaches any output stream** — not stdout, stderr, `-v`, or dry-run
- Binary builds with `CGO_ENABLED=0`

## Architecture

### Package layout

```
~/Projects/peter/npmctl/
  cmd/npmctl/main.go          # thin: cli.Execute()
  internal/
    npmapi/
      client.go               # transport, bearer, retry (READS ONLY), timeout
      errors.go               # NPM error → typed Go error
      types.go                # shared: ID, Timestamps
      tokens.go               # POST /tokens (+2FA), GET /tokens?expiry=
      proxyhosts.go           # CRUD + enable/disable
      health.go               # GET / (operationId: health)
      version.go
    auth/
      store.go                # Store interface, keyed by (profile,url,identity)
      keyring.go              # zalando/go-keyring
      file.go                 # 0600 + flock + atomic rename
      env.go                  # NPMCTL_* vars
      session.go              # token lifecycle
    config/
      profiles.go             # ~/.config/npmctl/config.yaml
    cli/
      root.go                 # cobra root, global flags
      guard.go                # THE write gate + undo journal + health verify
      auth.go host.go version.go undo.go
    output/
      tty.go json.go table.go yaml.go
      redact.go               # secret scrubber — applied to ALL renderers
    undo/
      journal.go              # pre-image capture + 30d retention sweep
      restore.go              # pre-image → Op, replayed through Guard
  testdata/
    schema-2.15.1.json        # vendored DEREFERENCED schema
    proxy-host-*.json, nginx-err-*.json
```

### Auth — verified API shapes

`POST /api/tokens` — body `{"identity": "<email>", "secret": "<password>", "scope": "user"}`.
Response is `oneOf`:
- `{"expires": "...", "token": "..."}`
- **2FA challenge** `{requires_2fa, challenge_token}` at HTTP **200**, challenge TTL 5m → `POST /api/tokens/2fa` with `{challenge_token, code}`. **`code` is a string** — preserve leading zeros, never parse as int.

`GET /api/tokens?expiry=30d` with bearer → refreshed `{expires, token}`. The GET route has **no validator**; `getFreshToken` honors `expiry`, and `parseDatePeriod` accepts `^([0-9]+)(y|Q|M|w|d|h|m|s|ms)$`. Default is `1d`.

### Credential design (D7 amended — token, not password)

```
npmctl auth login --url <u>
  1. prompt identity + secret (term.ReadPassword, never argv)
  2. POST /tokens                     → token | 2FA challenge
  3. if challenge: prompt code (string) → POST /tokens/2fa
  4. GET /tokens?expiry=30d           → mint bounded token
  5. store ONLY the token; discard the password
```

`--store-password` is explicit opt-in for unattended fleets that accept the risk. Default stores no password.

**Resolution chain**, each entry keyed by `(profile, url, identity)`:

```
1. --token flag                        explicit, one-shot, never persisted
2. NPMCTL_TOKEN env                    CI, containers, headless Linux
3. OS keyring (go-keyring)             macOS Keychain / Windows CredMan / Secret Service
4. ~/.config/npmctl/credentials.json   0600, explicit fallback
```

`NPM_TOKEN` is **forbidden** — it is the npm registry's variable. All vars prefixed `NPMCTL_`.

**Profile scoping is mandatory (R10).** A credential minted for `prod` must never be sent to `lab`. Refuse stored credentials when the profile URL changes. Never transmit a password over a connection with verification disabled.

Keyring is unavailable on headless Linux (no D-Bus). Probe at login; on failure fall to the 0600 file with a one-time warning. `auth status` always prints the active backend.

**Concurrency (R13):** credential file writes use `flock` + temp file + `os.Rename`. Under the lock, keep the entry with the later `expires`. On unmarshal failure emit a distinct message and exit 9 — never rewrite from a partial parse.

### Session lifecycle

```
token cached & unexpired        → use it
token near expiry               → GET /tokens?expiry=30d to refresh
refresh fails / 401             → exit 9 (interactive re-auth required)
```

**No automatic re-login.** It is impossible on a 2FA account (the response is a challenge, not a token), and with a stored password it degrades into password-spraying production on every call after a rotation. Exit 9 tells the human to run `auth login`.

### Transport retry (R4) — reads only

Retry **only** `GET` and token endpoints, **only** on connect-time errors and 502/503/504. **Never** retry `POST`/`PUT`/`DELETE`: NPM commits before responding, so a lost response is indistinguishable from a failed write, and a retried `POST /nginx/certificates` issues a second certificate against a 5-per-week duplicate limit.

On a lost response to a mutating call: exit 7, state the write **may have applied**, and name the read command that establishes ground truth.

### Write gate (`internal/cli/guard.go`)

Every mutation routes through `Guard`. No command calls a write method directly; a test enforces this.

```go
type Op struct {
    Verb        string // create|update|delete|enable|disable
    Resource    string // "proxy-host 12 (app.example.com)"
    Method, Path string
    Body        any
    Tier        Tier   // Normal | Destructive | Privileged
    ModifiedOn  string // for compare-and-swap
}
func Guard(ctx context.Context, o Op, do func() error) error
```

**Guard owns the whole transaction**, in order:

1. **Resolve + preview.** `--dry-run` performs **reads** to resolve IDs to names and fetch the target — the invariant is "no *mutating* request", not "no request". A preview that cannot name what it deletes is not a preview.
2. **Dependency scan** before any delete: hosts/redirects/dead-hosts/streams filtered on `certificate_id`/`access_list_id`; ACLs carry `proxy_host_count`. Refuse with exit 3 unless `--cascade-ack`.
3. **Authorize.** Require `NPMCTL_ALLOW_WRITE=1` **and** `--yes`. The env var lives outside argv, so an agent composing a command line cannot supply it and the tool-permission prompt still fires.
4. **Compare-and-swap (R8).** Re-GET immediately before the write; if `modified_on` differs from what the preview saw, abort with exit 3.
5. **Capture pre-image (R12).** Append the full prior object to `~/.local/state/npmctl/undo/<profile>/<ts>-<resource>-<id>.json` (0600) **before** executing. Name the file on stderr.

   **Stored raw, not scrubbed (V6/R16).** The output scrubber must **not** be applied here — a `[redacted]` value written into a pre-image makes the entry unusable for restore, which defeats the journal's only purpose. Consequence: a certificate pre-image contains `meta.dns_provider_credentials` in plaintext at 0600. This matches the trust level of the credential file, which already holds the bearer token. Two obligations follow: a **30-day retention sweep** runs on every invocation (delete entries older than 30d before appending), and `README.md` documents `~/.local/state/npmctl/undo/` as sensitive alongside the credential store.
6. **Execute.**
7. **Verify (R2).** For config-generating mutations, re-GET and assert `meta.nginx_online != false` and `meta.nginx_err == null`. On failure print `nginx_err` verbatim to stderr and exit 8.

**Tiers:**

| Tier | Applies to | Requirement |
|---|---|---|
| Normal | host/redirect/stream/dead-host/acl create, update, enable, disable | `NPMCTL_ALLOW_WRITE=1` + `--yes` |
| Destructive | any delete; `cert rm` (irreversible ACME revoke — R1) | + dependency scan + `--cascade-ack` when dependents exist |
| Privileged | `auth *`, `advanced_config` writes, `undo apply` | + **TTY only**, typed confirmation, refuse non-TTY unconditionally |

> **V3:** `user *`, `settings set`, and `login-as` were Privileged-tier members in the original draft. The entire `/users` surface and `PUT /settings/{id}` are deferred to v2, so those commands do not exist in v1 — the tier no longer needs to defend them.

`advanced_config` (R7) additionally requires `--allow-advanced-config` and renders as a line diff. It accepts raw nginx directives with no length or pattern constraint; a malicious block can `alias` NPM's `/data` volume over HTTP, exposing `database.sqlite` and `keys.json` — the RSA key signing every token. NPM rolls back only on `nginx -t` failure, and a valid-but-hostile config passes.

### `npmctl undo` — restore (V5)

Capture without replay is evidence, not recovery. Exit 8 ("applied, but nginx unhealthy") tells the operator the site may be down; without a restore path the next step is hand-crafting a `PUT` from JSON under pressure.

```
npmctl undo list                 # journal entries for the active profile, newest first
npmctl undo show <entry>         # render the pre-image (through the scrubber — display only)
npmctl undo apply <entry>        # replay it as a write
```

`undo apply` **is a mutation and receives no exemption.** It constructs an `Op` from the pre-image and routes through `Guard` like any other write, so it inherits: `NPMCTL_ALLOW_WRITE=1` + `--yes`, the CAS check on `modified_on`, its own pre-image capture (restoring is itself undoable), and post-write nginx health verification. `--dry-run` renders the field-level diff between current state and the pre-image.

Refusals it must make:
- The resource no longer exists → exit 5, with the create command that would recreate it.
- The pre-image predates a schema change and carries a key the current `additionalProperties: false` body rejects → exit 3 rather than a 400.
- The pre-image belongs to a different profile or URL → exit 3 (R10 scoping applies to the journal too).
- A `cert rm` pre-image → restore the database row, but state plainly that **ACME revocation is not reversed** (R1). The certificate material is gone.

### Exit codes (stable contract — agents depend on these)

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | generic error |
| 2 | usage error |
| 3 | refused / needs confirmation |
| 4 | auth failure (credentials rejected) |
| 5 | not found |
| 6 | API error |
| 7 | network error — **a mutating write may have applied** |
| 8 | **applied, but nginx reload unhealthy** (`nginx_err` set) |
| 9 | **interactive re-authentication required** |

`--dry-run` exits **0 with `"dry_run": true`** in JSON output and a `DRY RUN` banner on stderr, so an agent keying on exit status cannot mistake a simulation for a completed write.

### Output redaction (R3) — in the serializer, not per call site

A single scrubber wraps **every** renderer (table, json, yaml, dry-run body, `-v` transport log). Denylist keys: `secret`, `password`, `token`, `challenge_token`, `dns_provider_credentials`, `certificate_key`, `Authorization`. Values replaced with `[redacted]`.

Per-call-site redaction is what failed here — the original plan scoped it to the login password only, leaving `cert create --dns-provider`, `audit-log`, and `cert download` printing secrets on the path the skill mandates. (Two further leak sites from that review, `user set-password --dry-run` and `login-as`, no longer ship: V3 defers the whole `/users` surface to v2. The serializer-level design is kept regardless — it is what makes the *next* command safe by default, including `undo show`.)

### Payload construction

`PUT` is a partial update, but constrained: `minProperties: 1` and `additionalProperties: false`. A map-based builder must emit only set fields and **never** an unknown key. Send only what the caller changed.

> Removed: the original "meta-safe builder" and its regression mandate. Proxy-host `meta` holds no Let's Encrypt state (`internal/proxy-host.js` has 0 hits for `letsencrypt_agree`/`dns_challenge`), the server merges it (`:94,162`) and overwrites it after each write (`:217-218`), and no `PUT /nginx/certificates/{certID}` exists. See plan.md § Corrections.

### R11 — self-signed certs

`--insecure` + per-profile `insecure_skip_verify`, default **off**, plus `--ca-cert <path>` and `--pin-sha256 <hash>` so the common homelab case is solvable **without** disabling verification. Never send a password over an unverified connection.

### Config file

```yaml
# ~/.config/npmctl/config.yaml
default_profile: prod
profiles:
  prod:
    url: https://npm.example.com
    identity: me@example.com
  lab:
    url: https://10.0.0.5:81
    identity: lab-admin@example.com   # distinct identity — never share prod creds
    ca_cert: ~/.config/npmctl/lab-ca.pem
```

## Related Code Files

- Create: `cmd/npmctl/main.go`
- Create: `internal/npmapi/{client,errors,types,tokens,proxyhosts,health,version}.go`
- Create: `internal/auth/{store,keyring,file,env,session}.go`
- Create: `internal/config/profiles.go`
- Create: `internal/cli/{root,guard,auth,host,version,undo}.go`
- Create: `internal/output/{tty,json,table,yaml,redact}.go`
- Create: `internal/undo/{journal,restore}.go`
- Create: `testdata/schema-2.15.1.json`, `testdata/proxy-host-*.json`, `testdata/nginx-err-*.json`
- Create: `go.mod`, `.gitignore` (incl. `*.pem`, `*.zip`), `Makefile`, `README.md`

## Implementation Steps

1. `cd ~/Projects/peter/npmctl && git init && go mod init github.com/nnmduc/npmctl` (directory already exists). **Blocking question 1 resolved in validation.**
2. Add deps: cobra, go-keyring, yaml.v3, gofrs/flock.
3. `client.go` — base URL join, bearer injection, TLS options (`insecure`/`ca_cert`/`pin`), per-op timeout, **retry restricted to GET + token endpoints**.
4. `errors.go` — parse `{"error":{"code","message"}}`, map status → exit code, strip `debug.stack` from user-facing output.
5. `tokens.go` — `RequestToken` handling the 2FA `oneOf` branch; `Verify2FA(challengeToken, code string)`; `MintToken(expiry)` via `GET /tokens?expiry=`.
6. `output/redact.go` **first**, then the renderers — so no renderer can be written without it.
7. `auth/` — `Store` keyed by `(profile,url,identity)`; three impls; keyring probe + fallback; `file.go` with flock + atomic rename.
8. `auth/session.go` — cache, refresh, **exit 9 instead of auto-relogin**.
9. `config/profiles.go` — load/save YAML; resolve profile ← flag ← env ← default.
10. `undo/journal.go` — append pre-image raw, 0600, per-profile dir, **30d retention sweep on each run**; `undo/restore.go` — pre-image → `Op`.
11. `cli/guard.go` — the 7-step transaction + tiers.
12. `npmapi/proxyhosts.go` + `health.go` — CRUD, enable/disable, `GET /` pre-flight.
13. `cli/{auth,host,version,undo}.go` — all writes through `Guard`, `undo apply` included.
14. Tests (see Success Criteria).
15. `Makefile`: `build test lint fmt`.

## Success Criteria

- [x] `auth login` handles the 2FA challenge branch (fixture); `code` treated as string with leading zeros preserved
- [x] `auth login` mints a 30d token via `GET /tokens?expiry=30d` and stores **no password** by default
- [x] `auth status` shows profile, identity, backend, token expiry
- [x] Expired/invalid token → **exit 9**, never an automatic re-login
- [x] Credentials are profile-scoped; changing a profile's URL invalidates stored creds (test)
- [x] Credential file writes survive 8 concurrent processes without corruption (test with flock)
- [x] **No mutating method is ever retried** — counting RoundTripper fails the test if a POST/PUT/DELETE repeats
- [x] **`--dry-run` issues zero mutating requests** but MAY read; asserted by method, not by request count
- [x] `--dry-run` exits 0 with `"dry_run": true` in JSON and a stderr banner
- [x] Writes refuse without **both** `NPMCTL_ALLOW_WRITE=1` and `--yes` (exit 3) — table-driven over all write commands
- [x] Delete refuses with exit 3 when dependents exist and `--cascade-ack` is absent
- [x] CAS: a changed `modified_on` between preview and write aborts with exit 3
- [x] **Pre-image written to the undo journal before every mutating call** (asserted)
- [x] Pre-images are stored **raw** — a test asserts a known secret survives the round-trip into the journal file (scrubber must NOT run here)
- [x] Journal entries older than 30 days are deleted on the next invocation (test with backdated file mtimes)
- [x] **`npmctl undo apply` round-trips**: host update → undo → object matches the pre-image (fixture)
- [x] `undo apply` routes through `Guard` — refuses without `NPMCTL_ALLOW_WRITE=1` + `--yes`, and captures its own pre-image (asserted)
- [x] `undo apply` on a deleted resource exits 5; on a cross-profile entry exits 3
- [x] `undo show` renders **through** the scrubber; `undo apply` reads the raw file
- [x] **A write leaving `nginx_online: false` exits 8** and prints `nginx_err` (fixture)
- [x] **Redaction test:** a DNS credential, a user password, a bearer header, and a PEM key are absent from table, json, yaml, dry-run, and `-v` output
- [x] `advanced_config` write refuses without `--allow-advanced-config`, and refuses non-TTY
- [x] `--ca-cert` validates a self-signed endpoint **without** `--insecure` (httptest TLS server)
- [x] Exit codes match the table
- [x] `go test ./...` passes with no network
- [x] `CGO_ENABLED=0 go build` produces a static binary

## Risk Assessment

| Risk | Mitigation |
|---|---|
| **R2 nginx unhealthy after 200** | Guard step 7 re-GET + exit 8; `nginx_err` fixture test |
| **R3 secret leakage** | Scrubber in the serializer, written before any renderer; four-secret absence test |
| **R4 retry duplicating writes** | Retry allowlist by method; counting-RoundTripper test |
| **R5 agent misuse** | Out-of-argv `NPMCTL_ALLOW_WRITE=1`; Privileged tier refuses non-TTY |
| **R7 advanced_config injection** | Separate flag, TTY-only, line diff, skill prohibition |
| **R8 lost update** | CAS on `modified_on` immediately before write |
| **R9 blind delete** | Dependency scan in Guard step 2 |
| **R10 cross-profile credential leak** | Keys include profile+url+identity; refuse on URL change |
| **R12 no recovery** | Undo journal before every mutation **plus `npmctl undo apply`** to replay it (cannot undo ACME revocation — warn) |
| **R16 journal secrets at rest** | Raw at 0600 (scrubbing breaks restore), 30d retention sweep, README documents the path as sensitive |
| **R13 credential file race** | flock + atomic rename; keep later `expires` |
| Keyring unavailable headless | Probe-then-fallback + one-time warning |
| Password in memory/logs | Never via argv; `term.ReadPassword`; scrubber covers `-v` |
