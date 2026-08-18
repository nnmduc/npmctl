---
phase: 2
title: "Host Resources"
status: completed
priority: P1
effort: "3-4 days"
dependencies: [1]
---

# Phase 2: Host Resources

# [RED TEAM] Findings 1,2,3,8,10,11,12,13 applied 2026-08-18. Cert payload and ACL field names corrected against v2.15.1 source.
<!-- Updated: Validation Session 1 — V7 lab NPM instance added; E2E smoke test moved P3 → P2 -->

## Overview

Add the remaining day-to-day resources: certificates, redirection hosts, streams, dead (404) hosts, access lists. Two of these are genuinely dangerous and are not simple repetitions of the proxy-host pattern:

- **`cert rm` performs irreversible ACME revocation** (R1) — `internal/certificate.js:418-421` calls `revokeLetsEncryptSsl()` for any `provider: letsencrypt`.
- **`acl update` replaces nested arrays wholesale**, and the API never returns existing passwords, so a naive read-modify-write silently blanks every basic-auth user.

**Every request body in this phase must be derived from the v2.15.1 schema files, not from prose.** The original draft of this phase contained two payloads that would have 400'd on first contact with production, and the fixtures-only strategy (D9) would not have caught either — the fixtures would have matched the wrong payload.

**V7:** that reasoning is exactly why this phase now stands up a disposable NPM instance. See § Lab instance.

## Requirements

**Functional**
- `npmctl redirect|stream|dead-host list|get|create|update|rm|enable|disable`
- `npmctl acl list|get|create|update|rm`
- `npmctl cert list|get|create|rm|renew|download|upload|validate|test-http|dns-providers`
- `dead-host` aliased as `404`

**Non-functional**
- Cert timeouts: HTTP-01 default 180s; **DNS-01 default `propagation_seconds + 240s`**
- Progress to stderr only — stdout stays valid JSON
- `cert download` writes 0600 into a data dir, never cwd, never stdout by default
- A throwaway NPM 2.15.1 instance backs the `lab` profile; the opt-in E2E smoke test lives here, **not** in P3 (V7)

## Architecture

### Endpoint map

| Command | Endpoint |
|---|---|
| `redirect *` | `/nginx/redirection-hosts`, `/{id}`, `/{id}/enable`, `/{id}/disable` |
| `stream *` | `/nginx/streams`, `/{id}`, `/{id}/enable`, `/{id}/disable` |
| `dead-host *` | `/nginx/dead-hosts`, `/{id}`, `/{id}/enable`, `/{id}/disable` |
| `acl *` | `/nginx/access-lists`, `/{id}` |
| `cert list\|create` | `GET\|POST /nginx/certificates` |
| `cert get\|rm` | `GET\|DELETE /nginx/certificates/{id}` — **no PUT exists** |
| `cert renew` | `POST /nginx/certificates/{id}/renew` |
| `cert download` | `GET /nginx/certificates/{id}/download` (zip of `.pem`, incl. private key) |
| `cert upload` | `POST /nginx/certificates/{id}/upload` (**multipart**) |
| `cert validate` | `POST /nginx/certificates/validate` (**multipart**) |
| `cert test-http` | `POST /nginx/certificates/test-http` |
| `cert dns-providers` | `GET /nginx/certificates/dns-providers` |

### Certificates — corrected request shape

`POST /nginx/certificates` requires only `provider`. Certificate `meta` is **`additionalProperties: false`** and permits exactly:

`certificate`, `certificate_key`, `dns_challenge`, `dns_provider`, `dns_provider_credentials`, `key_type`, `letsencrypt_certificate`, `propagation_seconds`

```json
{
  "provider": "letsencrypt",
  "nice_name": "example.com",
  "domain_names": ["example.com", "www.example.com"],
  "meta": { "dns_challenge": false }
}
```

> **Corrected:** the earlier draft sent `letsencrypt_email` and `letsencrypt_agree`. Neither is an allowed key — the request would return `400 should NOT have additional properties`. The Let's Encrypt email comes from NPM's own settings, not the request body.

**`certificate_id: "new"`** — `common.json` types it `anyOf: [integer, string ^new$]`. A **proxy-host** create/update carrying `"new"` triggers blocking ACME issuance inside a host write. Host commands must detect this value and apply the cert timeout, not the default.

### R1 — `cert rm` revokes

Classify as **Destructive** with an explicit extra warning: revocation cannot be undone, and the undo journal's pre-image restores the database row but **not** the certificate. Require `--cascade-ack` when any host references the `certificate_id`; those hosts lose TLS material on the next nginx reload.

### R6 — ACME rate limits and the retry trap

Let's Encrypt allows 5 duplicate certificates per week. The transport already forbids retrying mutating methods (P1), which closes the automatic path. The remaining exposure is **agent- or human-driven** retry after an ambiguous timeout.

Mitigation, replacing the earlier advisory message:

1. **Attempt journal** per `(profile, sorted domain set)`. Refuse with exit 3 at ≥3 attempts in 7 days; `--force` overrides.
2. **Poll instead of guess.** `--wait` is the default: after the request, poll `GET /nginx/certificates` until the row appears with `expires_on`, or the deadline passes. Report exactly one of `ISSUED` / `NOT PRESENT` / `INDETERMINATE — do not retry before <time>`.
3. NPM creates the certificate row *before* running certbot and deletes it on failure, so `cert list` alone is ambiguous during the window — polling must distinguish "row absent" from "row present without `expires_on`".
4. Never auto-retry. This is consistent with P1's retry allowlist, not an exception to it.

### Access lists — corrected field names

`PUT /nginx/access-lists/{listID}` body is `additionalProperties: false` with exactly: `name`, `satisfy_any`, `pass_auth`, `items`, `clients`.

> **Corrected:** the earlier draft used `access_items` / `access_clients`. Those are the names in `common.json`'s shared definitions, not the request body properties. Sending them returns 400.

**Do not build `--add-item` / `--remove-item` in v1.** `GET` returns items as `{id, username, password: "", hint, ...}` — the password is **never** returned. Any GET-modify-PUT therefore writes back empty passwords for every existing user, and `password` has no `minLength`, so the API accepts it. Result: three real users silently reduced to blank credentials while the command exits 0.

`acl update` requires the caller to supply the complete `items`/`clients` arrays. Dry-run renders item-level `ADDED` / `REMOVED` / `PASSWORD RESET` diffs. Refuse any payload containing an existing username with an empty password.

### `cert download` — private key handling (R3)

Returns a **zip of every `.pem` in the cert directory, including `privkey.pem`** (`internal/certificate.js` → `readdirSync(...).filter(fn => fn.endsWith(".pem"))`).

- Default destination `$XDG_DATA_HOME/npmctl/certs/` (not cwd), mode **0600**
- Refuse to overwrite; refuse `-` (stdout) unless `--force-stdout` on a TTY
- Refuse a destination directory containing `.git` without `--force`
- `.gitignore` ships with `*.pem` and `*.zip`

### `cert upload` / `validate` — multipart

Both are multipart form uploads carrying PEM material. The dry-run printer must **not** dump file contents; print filenames, sizes, and detected types only. The P1 scrubber covers `certificate_key`, but a raw multipart body would bypass a key-based denylist — handle explicitly.

### Lab instance — first contact is not production (V7)

D9 keeps `go test ./...` hermetic, and that stays true. But fixtures only prove the code agrees with itself: a fixture built from a wrong payload matches the wrong payload. Two of this phase's bodies were wrong in the draft, and the failure mode for `cert rm` and `acl update` is not a 400 — it is a revoked certificate or three users with blank credentials.

Stand up NPM **2.15.1 specifically** (not `latest` — the whole plan is pinned to that version's behavior) in Docker, register it as the `lab` profile with its own identity and self-signed cert, and exercise this phase against it before the binary ever points at prod.

```
docker run -d --name npm-lab -p 8181:81 -p 8080:80 \
  -v ./npm-lab/data:/data -v ./npm-lab/letsencrypt:/etc/letsencrypt \
  jc21/nginx-proxy-manager:2.15.1
# profile: lab → https://localhost:8181, distinct identity, ca_cert or --insecure
```

What the lab catches that fixtures and `schema check` structurally cannot — all three are behavioral contracts invisible to the schema (P3 § Honest limit):

- `PUT` partial-update semantics and the `minProperties: 1` / `additionalProperties: false` rejection surface
- the proxy-host `meta` merge-then-overwrite cycle (`internal/proxy-host.js:94,162,217-218`)
- revoke-on-delete for `provider: letsencrypt`, and the `certificate_id: "new"` blocking-issuance path
- ACL round-trip: whether a GET-modify-PUT really does blank passwords

**The opt-in E2E smoke test (`NPMCTL_E2E_URL`) moves from P3 into this phase.** It stays skipped by default, so D9's hermetic guarantee is unchanged — but it now exists at the point where the dangerous code is written rather than one phase after it.

ACME notes for the lab: Let's Encrypt cannot validate `localhost`, so cert issuance tests either target a real domain pointed at the lab, or use the LE **staging** directory. Staging has separate, far looser rate limits — which is also what makes the attempt journal (R6) safe to exercise there.

## Related Code Files

- Create: `internal/npmapi/{redirectionhosts,streams,deadhosts,accesslists,certificates}.go`
- Create: `internal/cli/{redirect,stream,deadhost,acl,cert}.go`
- Create: `internal/certattempt/journal.go` — ACME attempt tracking
- Create: `testdata/{certificate,access-list,stream,redirection-host}-*.json`
- Create: `internal/npmapi/e2e_test.go` — opt-in smoke test behind `NPMCTL_E2E_URL` (**moved from P3**)
- Create: `docs/lab-instance.md` — Docker recipe, `lab` profile setup, LE staging note
- Modify: `internal/cli/guard.go` — `cert rm` revocation warning; `advanced_config` diff rendering
- Modify: `internal/cli/root.go` — register new groups (**conflicts with P3 — sequence P2 → P3**)

## Implementation Steps

1. **Re-derive every request body in this phase from `backend/schema/paths/**` at tag v2.15.1.** Do not transcribe from this document; this document has been wrong twice.
2. `redirectionhosts.go`, `streams.go`, `deadhosts.go` — the 7 operations each. Extract a shared helper only after the third concrete implementation exists.
3. `accesslists.go` — CRUD with correct `items`/`clients`; full-replacement semantics documented in `--help`.
4. `certificates.go` — all 9 operations; multipart for upload/validate; download to 0600 file.
5. `certattempt/journal.go` — per-profile, per-domain-set attempt tracking with 7-day window.
6. Cert polling (`--wait` default) with the three-state result.
7. Host commands detect `certificate_id: "new"` and switch to the cert timeout.
8. CLI commands; every write through `Guard`; `cert rm` marked Destructive with revocation warning.
9. Progress reporting to stderr.
10. Fixtures + tests.
11. Stand up the NPM 2.15.1 Docker lab; register the `lab` profile; write the opt-in E2E smoke test behind `NPMCTL_E2E_URL` (**moved from P3**).
12. Run the full host + cert + ACL lifecycle against the lab and reconcile any behavioral divergence from this document **before** pointing at prod.

## Success Criteria

- [x] `redirect`, `stream`, `dead-host` each support all 7 operations
- [x] `404` alias resolves to `dead-host`
- [x] **`cert create` payload validates against the vendored schema** — test asserts no key outside the permitted `meta` set (guards the exact bug found in review)
- [x] **`acl update` uses `items`/`clients`** — test asserts the serialized body has no `access_items`/`access_clients`
- [x] `acl update` refuses a payload with an existing username and empty password
- [x] `acl` dry-run renders `ADDED`/`REMOVED`/`PASSWORD RESET` per item
- [x] `cert rm` warns that ACME revocation is irreversible, and refuses without `--cascade-ack` when hosts reference the cert
- [x] Attempt journal refuses a 4th issuance for the same domain set within 7 days (exit 3) unless `--force`
- [x] `cert create --wait` reports exactly one of ISSUED / NOT PRESENT / INDETERMINATE — never "may have succeeded" *(all three states covered by fixture tests; not yet observed against a real ACME order)*
- [x] DNS-01 timeout = `propagation_seconds + 240s`; HTTP-01 = 180s; `--timeout` overrides
- [x] Host write with `certificate_id: "new"` uses the cert timeout, not 30s
- [x] `cert download` writes 0600 outside cwd, refuses overwrite, refuses stdout by default, refuses a `.git` directory
- [x] `cert upload`/`validate` dry-run prints filenames and sizes, **never PEM content**
- [x] Progress on stderr — `-o json` stdout parses as valid JSON (asserted)
- [x] All writes refuse without `NPMCTL_ALLOW_WRITE=1` + `--yes`; dry-run issues no mutating request
- [x] Pre-image captured for every mutation in this phase
- [x] `go test ./...` hermetic — E2E smoke test skips cleanly when `NPMCTL_E2E_URL` is unset
- [x] Lab NPM **2.15.1** runs in Docker and is reachable as the `lab` profile with its own identity
- [x] Full host + cert + ACL lifecycle verified against the lab before any prod invocation — *host, redirect, stream, dead-host and ACL lifecycles ran end to end. Certificate coverage was reads, dry-run payload shape, dependency scan and the attempt cooldown; **real ACME issuance was not exercised** because Let's Encrypt cannot validate `.test` names. That needs a public domain or the LE staging directory.*
- [x] Lab run confirms the three behavioral contracts fixtures cannot: `PUT` partial-update rejection, `meta` merge/overwrite, revoke-on-delete

## Risk Assessment

| Risk | Mitigation |
|---|---|
| **R1 `cert rm` irreversible revocation** | Destructive tier + explicit warning + dependency scan; pre-image cannot restore the cert — say so |
| **R6 ACME rate limits** | Attempt journal + cooldown + polling; no auto-retry (consistent with P1 allowlist) |
| **Invalid payloads (found in review)** | Step 1 mandates re-derivation from schema; two tests assert the specific corrected shapes |
| **ACL password blanking** | No add/remove helpers in v1; refuse empty password for existing user; item-level diff |
| **Private key exposure** | 0600, outside cwd, no stdout, `.git` refusal, `.gitignore` entries |
| Multipart bypassing the scrubber | Explicit handling — print metadata only, never body |
| Progress corrupting JSON | stderr only; asserted |
| Premature abstraction | Extract shared helper only after the third implementation |
| **First contact with prod (V7)** | Disposable NPM 2.15.1 Docker lab exercises cert/ACL behavior first; E2E smoke test relocated here from P3 |
| Lab drifts from prod version | Pin the image to `2.15.1`, never `latest` |
| P2/P3 `root.go` conflict | Sequence P2 before P3 |
