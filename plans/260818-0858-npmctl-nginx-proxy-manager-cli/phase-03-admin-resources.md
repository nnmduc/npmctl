---
phase: 3
title: "Read-Only Admin and Schema Drift"
status: completed
priority: P2
effort: "1 day"
dependencies: [1, 2]
---

# Phase 3: Read-Only Admin and Schema Drift

# [RED TEAM] Findings 5,8,12,14,15 applied 2026-08-18. audit-log pagination removed (doesn't exist); schema-check normalization added.
<!-- Updated: Validation Session 1 — V3 trimmed phase to read-only: /users surface and PUT /settings/{id} deferred to v2; V7 E2E smoke test moved to P2 -->

## Overview

Close out v1 with the observability and drift-detection surface: `audit-log`, `report hosts`, `health`, read-only `settings`, and `npmctl schema check` — the drift detector that partially compensates for fixtures-only testing (D9/R12).

**Scope trimmed in validation (V3).** The original phase chased full 68-operation parity and said so honestly in its own overview: *"most of this phase is low daily value, and it supplies most of the tool's attack surface."* That argument won. The `/users` surface and `PUT /settings/{id}` are **deferred to v2**:

| Deferred to v2 | Why |
|---|---|
| `GET\|POST /users`, `GET\|PUT\|DELETE /users/{id}` | Low daily value for a proxy-management CLI |
| `PUT /users/{id}/auth` (`set-password`) | One call seizes the admin account — `current` is optional. Removing the command removes the path |
| `POST /users/{id}/login` (`login-as`) | Returns an unrevocable ~1d RS256 JWT. For an agent-driven CLI, printing it *is* the exfiltration |
| `POST\|GET\|DELETE /users/{id}/2fa`, `/2fa/enable`, `/2fa/backup-codes` | Poorly documented (was R14); `DELETE` takes the TOTP `code` as a query param |
| `PUT /settings/{id}` | Free-form setting IDs, version-dependent, no safe default validation |

Scope is a stronger control than tiering: a command that does not exist cannot be misgated, mis-tiered, or reached by an agent that ignores its instructions. **Everything in this phase is a read.** Consequences: this phase no longer modifies `internal/cli/guard.go`, and the P2 → P3 sequencing constraint narrows to `internal/cli/root.go` alone.

## Requirements

**Functional**
- `npmctl audit-log list|get`
- `npmctl report hosts`
- `npmctl health` — `GET /` (`operationId: health`), missing from the original endpoint map
- `npmctl settings list|get` — **read only**
- `npmctl schema get|check`

**Non-functional**
- **No write operations ship in this phase.** A test asserts the deferred commands are absent from the cobra tree
- `schema check` runs offline with `--from-file`
- Redaction assertions are endpoint-specific here, not merely inherited from P1

## Architecture

### Endpoint map

| Command | Endpoint |
|---|---|
| `audit-log list\|get` | `GET /audit-log`, `/{id}` |
| `report hosts` | `GET /reports/hosts` |
| `settings list\|get` | `GET /settings`, `GET /settings/{id}` |
| `schema get` | `GET /schema` |
| `health` | `GET /` (`operationId: health`) — **was missing from the original map** |

### `audit-log` — credential leak and no pagination

Two corrections carried from the red team, both still load-bearing:

1. **No `--limit` / `--offset`.** `internal/audit-log.js` hardcodes `.limit(100)`; the validator whitelist is `{expand, query}` only. Support `--expand user` and `--query` and nothing else. The original plan invented both flags.
2. **It leaks DNS provider credentials.** `omissions()` is `["is_deleted", "owner.is_deleted", "meta.dns_provider_credentials"]`, applied by `create` (`certificate.js:225`) but **not** by `renew` (`:916` passes `meta: updatedCertificate` raw). So any DNS-01 renewal persists the provider API token into `audit_log.meta`.

The P1 scrubber (`internal/output/redact.go`) covers this, since `dns_provider_credentials` is on the denylist — but the test must assert it **specifically for this endpoint**. This is the one endpoint in the phase with real agent value, it is in the skill's `allowed-tools`, and it will be called. An inherited guarantee that nothing exercises is not a guarantee.

### `settings` — read only

`GET /settings` and `GET /settings/{id}`. Setting IDs are free-form strings in the schema (`settingID: {type: string, minLength: 1}`) and the deployed set is small and version-dependent, so do not model per-setting types and do not hardcode an ID list — the original draft referenced `oauth-auth`, which may not exist.

`settings set` (`PUT /settings/{id}`) is deferred. Reading is enough to answer "how is this instance configured"; writing needs per-setting validation this phase deliberately does not build.

### `schema check` — R12/R15 mitigation

```
npmctl schema check [--from-file testdata/schema-2.15.1.json]
```

1. Fetch live `GET /api/schema` (server-dereferenced) or read `--from-file`.
2. **Normalize before diffing** — NPM mutates `info.version` and `servers[0].url` per request from the `Origin` header, so a naive diff always reports drift. Strip both.
3. Diff paths, **methods**, request-body property sets, `required`, `enum`, and `additionalProperties`. Property-set diffing is what would have caught the cert `meta` and ACL field bugs.
4. Exit 0 when equivalent, 1 on drift.
5. Report drift in **deferred** paths (`/users/*`, `PUT /settings/{id}`) as informational, not failing — v1 does not implement them, so their drift cannot break v1.

**Honest limit:** `schema check` cannot detect *behavioral* drift. Partial-update semantics, the `meta` merge/overwrite cycle, and revoke-on-delete are implementation contracts invisible to the schema. That gap is covered by the **lab instance and opt-in E2E smoke test, which V7 moved into P2** — at the point where the dangerous code is written rather than one phase later.

### Parity accounting

AC2 no longer claims all 68 operations. The checklist enumerates **path × method** against the vendored schema and splits into two lists: **implemented** and **deferred-with-reason**. A path-list checklist passes while missing methods — that failure mode is what produced the original `GET /` omission, and it stays guarded.

## Related Code Files

- Create: `internal/npmapi/{settings,auditlog,reports,schema}.go`
- Create: `internal/cli/{settings,auditlog,report,schema}.go`
- Create: `internal/schema/diff.go`
- Create: `testdata/{setting,audit-log,report}-*.json`, `testdata/schema-drifted.json`, `testdata/audit-log-with-dns-creds.json`
- Modify: `internal/cli/root.go` — register groups (**after P2**)
- **Not modified:** `internal/cli/guard.go` — this phase ships no writes (V3)
- **Not created:** `internal/npmapi/users.go`, `internal/cli/user.go` — deferred to v2

## Implementation Steps

1. `settings.go` — list/get only.
2. `auditlog.go` — `--expand`/`--query` only; **no limit/offset**.
3. `reports.go`; wire `health.go` (built in P1) to a `health` command.
4. `schema.go` + `internal/schema/diff.go` with `info.version`/`servers[0].url` normalization and deferred-path classification.
5. Vendor the **dereferenced** schema (from a live `/api/schema`, ideally the P2 lab instance) into `testdata/schema-2.15.1.json`.
6. Build the path × method parity checklist with its implemented/deferred split.
7. Fixtures + tests, including the absence test for deferred commands.

## Success Criteria

- [x] **Parity checklist enumerates path × method** against the vendored schema and classifies every operation as implemented or deferred-with-reason — no path-only accounting
- [x] **Deferred commands are absent from the binary** — test walks the cobra tree and asserts no `user`, `login-as`, or `settings set` command exists
- [x] `GET /` health command exists and is used as a pre-flight check before mutations
- [x] **Every command in this phase is a read** — test asserts no command in the group constructs a `Guard` op
- [x] **`audit-log list` output contains no DNS provider credential** — fixture `audit-log-with-dns-creds.json` asserts redaction at this endpoint specifically
- [x] `audit-log` exposes only `--expand`/`--query`; no invented pagination flags
- [x] `settings list|get` return values; no write path exists
- [x] `schema check --from-file testdata/schema-drifted.json` exits 1 and names the drift
- [x] `schema check` against the unmodified fixture exits 0 **despite** mutated `info.version`/`servers[0].url`
- [x] Diff detects a changed request-body property set (regression guard for the P2 payload bugs)
- [x] Drift confined to deferred paths is reported as informational and exits 0
- [x] `go test ./...` hermetic

## Risk Assessment

| Risk | Mitigation |
|---|---|
| ~~Admin takeover via `set-password`~~ | **Removed by scope (V3)** — the command does not exist in v1 |
| ~~`login-as` token in transcript~~ | **Removed by scope (V3)** — the command does not exist in v1 |
| ~~R14 2FA endpoints undocumented~~ | **Removed by scope (V3)** — no `/users` endpoint ships |
| **DNS credentials via `audit-log`** | P1 scrubber + endpoint-specific assertion test (this endpoint is in the skill grant and will be called) |
| **R12 behavioral drift invisible to schema** | Stated as a limit; covered by the P2 lab instance + opt-in E2E smoke test (V7) |
| **R15 false-positive drift** | Normalize `info.version` + `servers[0].url` before diffing |
| Parity claim overstates coverage | Checklist splits implemented vs deferred; AC2 reworded to the v1 surface |
| Deferred surface silently returns | Absence test on the cobra tree |
| P2/P3 `root.go` conflict | Explicit dependency on P2 (`guard.go` conflict no longer applies) |
