---
title: "npmctl — Go CLI + Agent Skill for Nginx Proxy Manager"
description: "Single-binary Go CLI wrapping the Nginx Proxy Manager v2.15.1 API (44 paths / 68 operations, less a deferred admin slice), plus a Claude/Codex/Antigravity Agent Skill. Write-gated for safe agent-driven production use."
status: completed
priority: P2
branch: ""
tags: [go, cli, nginx-proxy-manager, agent-skill, devops]
blockedBy: []
blocks: []
created: "2026-08-18T02:03:28.022Z"
createdBy: "ck:plan"
source: skill
---

# npmctl — Go CLI + Agent Skill for Nginx Proxy Manager

## Overview

Build `npmctl`: a single static Go binary wrapping the Nginx Proxy Manager (NPM) v2.15.1 REST API — **44 paths / 68 operations**, minus a deliberately deferred admin slice (see D3) — plus an Agent Skill so AI agents (Claude Code, Codex, Antigravity) can manage NPM safely.

Core constraint is **safety, not features**. The tool targets live production (dwighthanoi WP server). Mutations pass one gate requiring an out-of-argv factor plus `--yes`; `--dry-run` performs reads only and issues no mutating request. Stable exit codes let an agent reason about refusals instead of retrying blindly.

**Source:** [brainstorm report](../reports/brainstorm-design-260818-0858-npmctl-go-cli-and-agent-skill-for-nginx-proxy-manager-report.md) — decisions D1–D10 locked with user, then D5/D7/D10 amended after red-team (see below).

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Core Foundation](./phase-01-core-foundation.md) | Complete |
| 2 | [Host Resources](./phase-02-host-resources.md) | Complete |
| 3 | [Admin Resources](./phase-03-admin-resources.md) | Complete |
| 4 | [Agent Skill and Release](./phase-04-agent-skill-and-release.md) | Complete |

Sequential. P1 blocks all. **P2 and P3 are NOT independent** — both register commands in `internal/cli/root.go`, and P2 step 1 may refactor a shared resource helper P3 builds on. Run P2 → P3, or accept merge conflicts in `root.go`.

After validation V3, **P3 is read-only** (`audit-log`, `report hosts`, `health`, `settings list|get`, `schema get|check`) and no longer touches `internal/cli/guard.go`. Effort drops ~2d → ~1d.

**P1 exit = tool is already usable** (auth + full proxy-host lifecycle).

## Locked Decisions

| # | Decision |
|---|---|
| D1 | Go, single static binary |
| D2 | Repo `~/Projects/peter/npmctl`, module `github.com/nnmduc/npmctl`, binary `npmctl`; **public, MIT** |
| D3 | **[AMENDED]** Parity over the **v1 surface** — the `/users` paths and `PUT /settings/{id}` are deferred to v2 |
| D4 | Hand-written client, no codegen; vendor schema as test fixture |
| D5 | **[AMENDED]** `SKILL.md` wrapping CLI, granting **read verbs + `--dry-run` only**; MCP deferred |
| D6 | Write-gated + `--dry-run` on all mutations |
| D7 | **[AMENDED]** Store a **bounded long-lived token**, not the password |
| D8 | TTY-aware output + `-o` override |
| D9 | Hermetic fixture tests only |
| D10 | **[AMENDED]** `apply`/`diff`/`export` still v2, but a **pre-write undo journal *and* `npmctl undo` restore ship in P1** |

### Amendments (2026-08-18, post red-team)

**D7 — credentials.** `POST /tokens` takes no expiry, but **`GET /tokens?expiry=1y` does** — the GET route has no `apiValidator` and passes `req.query.expiry` into `getFreshToken`, where `parseDatePeriod` accepts `y`. The original rejection of long-lived tokens rested on a false premise. New flow: login once (completing a 2FA challenge interactively if present) → immediately mint a **30d** token → store only the token → discard the password. `--store-password` is explicit opt-in. This also fixes the fact that **auto-relogin is impossible on a 2FA account**, which made the original AC1 unachievable by construction.

**D5 — skill grant.** `allowed-tools: Bash(npmctl:*)` pre-approved the entire admin surface, suppressing the tool-permission prompt that was the real human checkpoint. Narrowed to read verbs and `--dry-run`, so any write re-enters the normal approval flow.

**D10 — recovery.** Export/apply stay in v2, but `Guard` now always captures a pre-image before any mutating call. Without it, prior state existed nowhere at the moment of a destructive write.

## Corrections From Verification

Verified against the NPM v2.15.1 **OpenAPI schema and backend source**. Several plan and brainstorm assumptions were wrong.

1. **Login payload is `{identity, secret}`**, not `{email, password}`; optional `scope: "user"`. Response is `oneOf`: a token, **or a 2FA challenge** (`{requires_2fa, challenge_token}`, challenge TTL 5m) requiring `POST /tokens/2fa`. The 2FA `code` is a **string** (leading zeros survive).
2. **`POST /tokens` has no expiry parameter, but `GET /tokens?expiry=` does** and is unvalidated. Token default is `1d`.
3. **The original R1 was aimed at a non-existent risk.** Proxy-host `meta` does **not** hold Let's Encrypt state — `grep letsencrypt_agree|dns_challenge internal/proxy-host.js` returns 0 hits; those fields live on the *certificate* object. Further, the server **merges** meta (`internal/proxy-host.js:94,162` — `_.assign({}, row.meta, thisData.meta)`) and then **overwrites** it after each write (`:217-218`, `configure()` returns `new_meta`). And there is **no `PUT /nginx/certificates/{certID}`** at all, so certificate meta is unreachable from this CLI. The meta-safe builder type and its cross-phase regression mandate are **removed**.
4. **The real irreversible operation is `cert rm`.** `internal/certificate.js:418-421` — deleting a `letsencrypt` cert calls `revokeLetsEncryptSsl()`. Actual ACME revocation. This is now R1.
5. **`PUT` is a partial update but not unconstrained** — `minProperties: 1` and `additionalProperties: false`. A map-based payload builder must never emit an unknown key.
6. **Parity is 68 operations across 44 paths.** A path-list checklist passes while missing methods. `GET /` (`operationId: health`) was unmapped entirely.
7. **`audit-log` has no `--limit`/`--offset`** — `internal/audit-log.js` hardcodes `.limit(100)`; the validator whitelist is `{expand, query}`.
8. **`certificate_id` accepts the string `"new"`** (`common.json`, `anyOf` integer | `^new$`), so a *proxy-host* write can trigger blocking ACME issuance.
9. **`audit-log` leaks credentials.** `omissions()` includes `meta.dns_provider_credentials`, applied by `create` (`certificate.js:225`) but **not by `renew`** (`:916` passes `meta: updatedCertificate` raw).

## Risks

| # | Risk | Sev | Mitigation | Phase |
|---|---|---|---|---|
| R1 | **`cert rm` performs irreversible ACME revocation** | **Critical** | Privileged confirmation, dependent scan, undo journal (cannot un-revoke — warn explicitly) | P1, P2 |
| R2 | Write returns 200 while nginx reload failed (`meta.nginx_err`) | **Critical** | Post-write re-GET, assert `nginx_online`, exit 8 | P1 |
| R3 | Secrets leak via dry-run bodies, `audit-log`, `cert download`, multipart cert upload (`login-as` deferred by V3) | **Critical** | Single output scrubber in the serializer + header redaction | P1 |
| R4 | Auto-retry duplicates non-idempotent writes / burns ACME quota | **Critical** | Retry GET + token endpoints only; test forbids retrying mutating methods | P1 |
| R5 | Agent misuse on production | High | Out-of-argv `NPMCTL_ALLOW_WRITE=1` + `--yes`; narrow skill grant; Privileged tier | P1, P4 |
| R6 | ACME rate limits (5 duplicate certs/week) | High | Attempt journal + cooldown; never auto-retry; poll instead of guess | P2 |
| R7 | `advanced_config` raw-nginx injection → `/data` exposure → `keys.json` → forged tokens | High | Separate `--allow-advanced-config` gate, TTY-only, line diff, prohibited in skill | P1, P2 |
| R8 | Lost update — no ETag/`If-Match`; dry-run→`--yes` widens the window | High | Capture `modified_on`, re-GET immediately before PUT, abort on change | P1 |
| R9 | Delete previews blind to dependents | High | Dependency scan before delete; refuse without `--cascade-ack` | P1, P2 |
| R10 | Credentials not profile-scoped; `-p lab --insecure` leaks prod creds | High | Key credentials by `(profile, url, identity)`; refuse on URL change | P1 |
| R11 | Self-signed NPM certs | High | `--insecure` + per-profile flag, plus `--ca-cert` / `--pin-sha256` | P1 |
| R12 | Fixtures never exercise real NPM | Med | `schema check` + a **throwaway NPM 2.15.1 Docker instance** as the `lab` profile; the opt-in live smoke test (`NPMCTL_E2E_URL`, skipped by default) moves **P3 → P2** so cert/ACL code is exercised before prod | P2 |
| R13 | Concurrent invocations corrupt the credential file | Med | `flock` + temp-file `os.Rename`; keep later `expires` | P1 |
| R14 | ~~2FA-admin endpoints poorly documented~~ | — | **Retired by V3** — the whole `/users` surface is deferred to v2, so no 2FA-admin endpoint ships in v1 | — |
| R16 | Undo journal holds secrets at rest (a cert pre-image carries `meta.dns_provider_credentials`) | Med | Stored **raw** at 0600 so restore stays possible; 30-day retention sweep on each run; README documents `~/.local/state/npmctl/undo/` as sensitive | P1 |
| R15 | Upstream schema drift | Med | Vendored fixture + `schema check` (normalize `info.version`/`servers[0].url` before diffing) | P3 |

## Acceptance Criteria

1. `npmctl auth login` completes a 2FA challenge when present, mints a 30d token via `GET /tokens?expiry=30d`, stores **only the token**, and reports the credential backend used. A call 48h later works with no re-prompt.
2. Every operation in the **v1 surface** reachable — checklist enumerates path × method against the vendored schema, not paths. `GET /` health included. `npmctl <group> --help` self-describes. The deferred set (**all `/users` paths, `PUT /settings/{id}`**) is enumerated explicitly as out-of-scope, and a test asserts those commands are absent from the binary. The exact v1 operation count is derived from the vendored schema at implementation time, not asserted here.
3. `npmctl host list` → table on TTY, JSON when piped; `-o` overrides both.
4. Writes refuse without **both** `NPMCTL_ALLOW_WRITE=1` and `--yes` (exit 3). `--dry-run` issues **zero mutating requests** (reads permitted, asserted by method).
5. **No secret ever reaches stdout/stderr** — test asserts a DNS provider credential, a user password, a bearer header, and a PEM key are absent from every renderer including dry-run and `-v`.
6. **Every mutating call writes a pre-image** to the undo journal before executing (asserted). `npmctl undo <entry>` replays that pre-image back through `Guard` — subject to the same write gate, CAS, and post-write health verification — and a fixture test round-trips a host update → undo → original state.
7. **A write that leaves `nginx_online: false` exits 8** and prints `nginx_err` verbatim (fixture test).
8. **A mutating method is never retried** by the transport (asserted with a counting RoundTripper).
9. `go test ./...` hermetic: no network, no Docker.
10. Single binary, no runtime deps, cross-compiles for 5 targets.
11. Claude Code drives a full host lifecycle **against the P2 lab instance** using only `SKILL.md`, and a write triggers a permission prompt rather than executing pre-approved.
12. `npmctl schema check` reports drift when pointed at a modified schema, and reports **no** drift against an unmodified one despite per-request `info.version`/`servers[0].url` mutation.
13. A disposable **NPM 2.15.1** Docker instance is registered as the `lab` profile, and the full host + cert + ACL lifecycle is verified against it **before** the binary is pointed at production.
14. The deferred surface is absent from the binary — a cobra-tree test asserts no `user`, `login-as`, or `settings set` command exists in v1.

## Dependencies

**External:** `spf13/cobra`, `zalando/go-keyring`, `gopkg.in/yaml.v3`, `gofrs/flock`, stdlib `net/http`. No viper (KISS).

**Tooling:** Docker (P2 lab instance, `jc21/nginx-proxy-manager:2.15.1` — pinned, never `latest`), goreleaser (P4).

**Cross-plan:** none. First plan in this repo.

**Blocking questions — both RESOLVED in validation (2026-08-18):**
1. ~~GitHub account for module path~~ → **`github.com/nnmduc/npmctl`** (account `nnmduc`).
2. ~~Public or private repo~~ → **public, MIT**. Homebrew tap viable; README must carry the honest threat model.

Nothing blocks P1.

## Red Team Review

### Session — 2026-08-18
**Reviewers:** Security Adversary, Assumption Destroyer, Failure Mode Analyst (3 parallel, hostile lens)
**Findings:** 15 after dedupe from 27 raw (15 accepted, 0 rejected)
**Severity breakdown:** 8 Critical, 6 High, 1 Medium
**Evidence standard:** greenfield project — no codebase exists, so evidence was plan `file:line` plus verification against the NPM v2.15.1 OpenAPI schema **and backend source**. All load-bearing claims were independently re-verified by the controller before acceptance.

| # | Finding | Severity | Disposition | Applied To |
|---|---------|----------|-------------|------------|
| 1 | R1 targeted a non-risk — proxy-host `meta` holds no LE state, server merges then overwrites it, no `PUT /certificates/{id}` exists | Critical | Accept | plan.md, phase-01, phase-02 |
| 2 | `cert rm` performs irreversible ACME revocation; was unranked | Critical | Accept | plan.md (new R1), phase-02 |
| 3 | P1 "single retry on 5xx/network" contradicts P2 "never auto-retry cert creation" | Critical | Accept | phase-01, phase-02 |
| 4 | No post-write nginx health verification; NPM returns 200 with `nginx_err` set | Critical | Accept | phase-01 |
| 5 | `Bash(npmctl:*)` + `--yes` pre-authorizes admin takeover (`PUT /users/{id}/auth` `current` optional) | Critical | Accept (user decision: out-of-argv factor + narrow grant) | plan.md D5, phase-01, phase-04 |
| 6 | D7 premise false — `GET /tokens?expiry=1y` works; storing the password unjustified | Critical | Accept (user decision: 30d token) | plan.md D7, phase-01 |
| 7 | Auto-relogin impossible on a 2FA account → original AC1 unachievable | Critical | Accept | plan.md AC1, phase-01 |
| 8 | Secrets unredacted in dry-run, `audit-log` (DNS creds on renew), `login-as`, `cert download` | Critical | Accept | phase-01, phase-02, phase-03 |
| 9 | No pre-write state capture; export deferred left zero recovery path | High | Accept (user decision: undo journal in P1) | plan.md D10, phase-01 |
| 10 | Cert `meta` payload and ACL field names invalid vs schema | High | Accept | phase-02 |
| 11 | Cert timeout mitigation advisory only; no attempt journal/cooldown; 180s too low for DNS-01 | High | Accept | phase-02 |
| 12 | Zero-HTTP dry-run cannot preview deletes; no CAS on `modified_on` | High | Accept | phase-01, phase-02 |
| 13 | `advanced_config` raw-nginx injection absent from all four phases | High | Accept | phase-01, phase-02, phase-04 |
| 14 | Credentials not profile-scoped | High | Accept | phase-01 |
| 15 | Parity accounting, `GET /` unmapped, `audit-log` pagination, `schema check` false drift, P2/P3 dependency | Medium | Accept | plan.md, phase-02, phase-03 |

### Whole-Plan Consistency Sweep

Performed after applying all 15 findings. Decision delta checked across `plan.md` and all four phase files:

- **`meta` builder / R1 machinery** — removed from plan.md risks, phase-01 architecture + steps + success criteria + risk table, and phase-02 (the "extend the meta-absence test to every resource" mandate). Remaining `meta` references are only where meta is legitimately authored (cert create) or read (nginx health). Verified no surviving reference to "GET → merge → PUT" as a general rule.
- **D7 password → token** — reconciled in plan.md decision table, amendments, AC1; phase-01 credential chain, session lifecycle, config example, steps, success criteria, risks. `NPMCTL_PASSWORD` retained only for the explicit `--store-password` opt-in path.
- **Retry contradiction** — phase-01 transport spec now excludes mutating methods; phase-02's "never auto-retry" restated as consistent with it rather than contradicting it.
- **Exit codes** — table extended (8 = applied but nginx unhealthy, 9 = interactive re-auth required) and propagated to phase-01, phase-03, phase-04 skill protocol.
- **Parity wording** — "44 endpoints" replaced with "44 paths / 68 operations" in plan.md overview, D3, AC2, and phase-03 success criteria. *(Superseded by validation V3: D3 amended and AC2 rescoped to the v1 surface — see § Validation Log.)*
- **P2/P3 independence** — corrected in plan.md Phases section.

**Unresolved contradictions: none.**

Two blocking questions remained at red-team time (GitHub module path, repo visibility); both were **resolved in Validation Session 1** — see § Validation Log.

## Validation Log

### Session 1 — 2026-08-18

**Mode:** prompt · **Questions asked:** 7 · **Verification pass:** skipped per Step 2.5 guard — `## Red Team Review` already present with `file:line` evidence against the NPM v2.15.1 OpenAPI schema and backend source; `[UNVERIFIED]` tag scan returned **0 hits** across all five plan files.

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| V1 | Module path (blocking Q1) | `github.com/nnmduc/npmctl` | Unblocks P1 step 1 `go mod init`; supports `go install` and goreleaser |
| V2 | Repo visibility (blocking Q2) | **Public + MIT** | Enables Homebrew tap and public releases; obliges the honest threat model in README (already a P4 criterion) |
| V3 | P3 scope vs AC2 full parity | **Trim P3 to read-only** | Phase argued against itself: low daily value, most of the attack surface. Ships `audit-log`, `report hosts`, `health`, `settings list\|get`, `schema get\|check`. Defers **all `/users` paths** and **`PUT /settings/{id}`** to v2 |
| V4 | Token expiry | **30d, as planned** | Bounded exposure on an unrevocable RS256 token; monthly interactive re-auth accepted |
| V5 | Undo journal restore | **`npmctl undo` ships in P1** | Journal was capture-only — evidence, not recovery. Restore replays the pre-image through `Guard` (write gate + CAS + health verify apply) |
| V6 | Journal secrets at rest | **Raw at 0600, 30d retention sweep** | Scrubbing would write `[redacted]` into a restore. Journal trust level matches the credential file, which already holds the token |
| V7 | First real contact is production | **Throwaway NPM 2.15.1 Docker instance as `lab`; E2E smoke moves P3 → P2** | P2 states its own draft "has been wrong twice" on payloads. Exercises cert/ACL behavioral contracts (partial-update, revoke-on-delete) before prod. D9 hermetic default preserved — smoke test stays opt-in behind `NPMCTL_E2E_URL` |

**Cascade from V3** — the deferral is not a phase-local edit:

- `D3` marked **[AMENDED]**; AC2 rewritten from "all 68 operations" to the v1 surface plus an explicit out-of-scope list and an absence test.
- `R14` (2FA-admin endpoints) **retired** — no `/users` endpoint ships.
- P3 becomes read-only → **no longer modifies `internal/cli/guard.go`**; the P2/P3 conflict narrows to `internal/cli/root.go` only. Sequencing P2 → P3 still stands.
- P1's Privileged tier loses `user *`, `settings set`, and `login-as`; it retains `auth *` and `advanced_config` writes.
- P4's "absent from the skill" criteria become **absent from the binary** — a stronger guarantee, reworded rather than dropped.
- Red Team findings 5 and 15 reference `PUT /users/{id}/auth` and the 68-operation count. Those entries are **historical record and are left intact**; the risk they describe is now removed by scope rather than mitigated by tiering.

**Effort delta:** P3 ~2d → ~1d (V3); P1 +~0.5d (V5 restore command); P2 +~0.5d (V7 lab instance). Net ≈ unchanged.

### Whole-Plan Consistency Sweep — Validation Session 1

Re-read `plan.md` and all four phase files after propagating V1–V7. Terms searched across every file: `68 operations`, `full parity`, `set-password`, `login-as`, `user *`, `settings set`, `/users`, `2fa`, `<user>`, `<module-path>`, `blocking question`, `R14`, `NPMCTL_E2E_URL`, `guard.go`.

Contradictions found and reconciled:

| Stale item | Location | Resolution |
|---|---|---|
| "Two blocking questions remain" | plan.md § Red Team sweep | Reworded to "remained at red-team time; resolved in Validation Session 1" |
| Frontmatter description claimed unqualified 68-op parity | plan.md:3 | Now "less a deferred admin slice" |
| Red-team parity-wording bullet described superseded state | plan.md § Red Team sweep | Left intact as history, annotated *(Superseded by V3)* |
| R3 listed `login-as` as a live leak site | plan.md § Risks | Replaced with multipart cert upload; `login-as` noted as deferred |
| Scrubber rationale cited `user set-password --dry-run` / `login-as` | phase-01 § Output redaction | Reworded to surviving leak sites; kept the serializer-level design and said why |
| E2E smoke test located in P3 | phase-02, phase-03, plan.md R12 | Moved to P2 in all three; P3 now points at P2 for behavioral coverage |
| P3 modifies `guard.go` (Privileged tier) | phase-03, plan.md § Phases | P3 is read-only; marked "Not modified"; P2/P3 conflict narrowed to `root.go` |
| R14 (2FA-admin) active | plan.md § Risks, phase-03 | Retired — no `/users` endpoint ships |
| `go mod init <module-path>` | phase-01 step 1 | `github.com/nnmduc/npmctl` |
| `LICENSE` "needs blocking question 2" | phase-04 | MIT |

Deliberately preserved as historical record, **not** contradictions: Red Team findings 5 and 15 reference `PUT /users/{id}/auth` and the 68-operation count. They describe what the review found at the time. The Validation Log states that V3 removes those risks by scope rather than by the mitigations those rows name.

Duplicate-contract check: the cert `meta` allow-list, the ACL `items`/`clients` body, and the exit-code table each appear in exactly one authoritative place (phase-02, phase-02, phase-01) and are referenced — not restated — elsewhere. No divergent second copies.

**Unresolved contradictions: none.**

## Implementation Log

### Session — 2026-08-18 (`/ck:cook --auto`)

All four phases implemented. `go test ./...` hermetic and green, `go vet` clean, `gofmt`
clean, every non-test file under 200 LOC, static binaries for all five release targets.

**Surface shipped:** 54 of the schema's 68 operations. The 14 deferred are the 13 `/users`
operations plus `PUT /settings/{settingID}`, absent from the binary and asserted absent by
a cobra-tree test. `schema check` against the live lab reports 68/68 operations and **zero**
findings.

#### Deviations from the plan, and why

| Planned | Shipped | Reason |
|---|---|---|
| `testdata/schema-2.15.1.json` | `internal/schemadata/schema-2.15.1.json` | `schema check` needs the document at runtime, and `go:embed` cannot reach outside its own package directory. One embedded copy serves both the command and the tests, instead of duplicating the file. |
| `skill/SKILL.md` + `skill/references/` | `internal/skill/files/…` | Same `go:embed` constraint. Keeping a second copy at the repo root would have been two sources of truth. |
| Generic `resource` helper "after the third implementation" | Extracted after the fourth | Followed the sequencing rule. `proxyhosts.go` was written concretely first; the helper appeared once redirect/stream/dead-host made the shape observable rather than guessed. Request bodies are deliberately **not** shared — they genuinely differ. |

A project-local `.claude/.ckignore` was added: the shared ignore baseline matches the words
`build` and `target` anywhere in a Bash command, which blocks `go build` and any Go
identifier named `target`. The override is scoped to this repo, which has neither directory.

#### Found by running against a real NPM 2.15.1 — invisible to fixtures

The lab instance (V7) paid for itself. Four issues that a fixtures-only strategy would have
shipped:

1. **`expand` was silently dropped on every request.** Every NPM route parses it as
   `typeof req.query.expand === "string" ? …split(",") : null`. npmctl sent repeated
   `expand=a&expand=b`, which Express turns into an array, so the `typeof` check failed and
   expand became `null` — no error, just missing fields. Now one comma-separated value.
   *Caught by:* `TestE2EAccessListNeverReturnsPasswords` returning zero items.
2. **NPM silently coerces four TLS flags off.** `internal/host.js` `cleanSslHstsData`:
   no `certificate_id` clears `ssl_forced` **and** `http2_support`; no `ssl_forced` clears
   `hsts_enabled`; no `hsts_enabled` clears `hsts_subdomains`. The write still returns 200.
   An operator passing `--hsts` and seeing exit 0 would reasonably believe HSTS was on, so
   npmctl now compares request against response and warns per flag with the missing
   prerequisite. This is a **new control not in the plan**, added because a silent no-op on
   a security flag is a correctness problem, not a cosmetic one.
3. **`stream create` produced a stream that forwarded nothing.** `tcp_forwarding` was only
   sent when the flag was explicitly changed, so NPM defaulted it to false despite `--help`
   documenting `--tcp` as true. Both protocol flags are now always sent on create.
4. **NPM 2.15.1 seeds no default admin.** `backend/setup.js` creates the first user only
   when `INITIAL_ADMIN_EMAIL` and `INITIAL_ADMIN_PASSWORD` are both set — the old
   `admin@example.com` / `changeme` pair is gone. The plan's Docker recipe omitted them, so
   anyone following it would have hit a login wall. Corrected in `docs/lab-instance.md`.

#### Verified empirically, not just from source

- **D7's premise.** `GET /tokens?expiry=30d` really does mint a 30-day token: login on
  2026-08-18 returned `expires: 2026-09-17`. The amendment rested on a correct reading.
- **Normalization is necessary, not defensive.** Live `info.version` is `2.15.1` against the
  vendored `2.x.x`, and `servers[0].url` differs by port because NPM derives it from the
  `Origin` header. Without stripping both, every check would report false drift.
- **`meta.nginx_online` exists** on live responses, so the gate's step 7 has something real
  to assert on.
- **`additionalProperties: false` is enforced server-side** (HTTP 400), which is what makes
  the payload allowlist load-bearing rather than decorative.
- **Access lists never return passwords** — every item comes back `password: ""`,
  confirming the read-modify-write refusal is required.
- **go-keyring is fine with `CGO_ENABLED=0`.** The static Linux binary runs on Alpine with
  no D-Bus and no glibc, degrading to the 0600 file rather than crashing.

#### Two bugs fixed in npmctl's own P1 code, caught by its tests

- An env- or flag-supplied token was treated as expired because it carries no `expires`
  metadata, which would have broken every CI and container invocation before it sent a
  request. Unknown expiry is now trusted, and a 401 is left to answer.
- The keyring availability probe *wrote* a throwaway item on every invocation. Under an
  overridden `HOME` macOS could not locate a login keychain and raised a **GUI prompt**,
  hanging the test suite. The probe is now a cached read, and the suite forces the file
  backend outright.

#### Not verified

- **Agent-driven acceptance.** Whether Claude Code can drive a full host lifecycle from
  `SKILL.md` alone, with a permission prompt on each write, needs a human-run agent session.
  The mechanical parts are asserted (no write verb in `allowed-tools`, `undo apply` excluded,
  the generated reference matching the binary), but the end-to-end behaviour is not.
- **Real ACME issuance.** Let's Encrypt cannot validate `.test` names. All three
  issuance states are covered by fixtures and the cooldown was exercised against the live
  binary, but no real order was placed.
- **`goreleaser` execution and `go install` from the public repo.** Both need a pushed
  repository and a tag.
