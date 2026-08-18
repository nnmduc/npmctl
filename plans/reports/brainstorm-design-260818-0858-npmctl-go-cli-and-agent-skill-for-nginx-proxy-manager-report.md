# Brainstorm Report — `npmctl`: Go CLI + Agent Skill for Nginx Proxy Manager

- **Date:** 2026-08-18 08:58 (Asia/Saigon)
- **Status:** Design approved. Plan created → `plans/260818-0858-npmctl-nginx-proxy-manager-cli/`
- **Superseded points:** two claims below were disproved by schema verification during planning. See `plan.md` § "Corrections From Schema Verification".
  1. §3.5 / §5.3 — NPM has **no `expiry` request parameter** on `POST /api/tokens`; token lifetime is server-controlled. The "mint a 1-year token" option was not implementable.
  2. §6 R1 — login payload is `{identity, secret}`, not `{email, password}`. And since `PUT` has **no required fields** (partial update), the R1 mitigation is simpler than GET-merge-PUT: **omit `meta` from update payloads entirely**.
- **Modes requested:** none (`--html`, `--wiki` not requested)
- **Scope:** greenfield project, no existing repo

---

## 1. Problem Statement

Manage Nginx Proxy Manager (NPM) hosts/certs/ACLs from terminal, and let AI agents (Claude Code, Codex, Antigravity) do the same safely. Reference impl `altpersona/nginx-proxy-manager-CLI` (Node/JS) exists but is thin, has admitted SSL-flag bugs, and no safety guardrails for agent-driven writes.

**Underlying problem (problem-first inversion):** the stated ask is "a CLI wrapper", but the real driver is *unattended + agent-driven NPM administration on live infra without breaking production*. That reframes safety from a feature to the core design constraint.

---

## 2. Scout Findings

| Finding | Detail |
|---|---|
| Project state | `/Users/duc` not a git repo. Greenfield. Stray root `package.json` (wrangler/docx) unrelated. |
| Toolchain | Go 1.25.6 darwin/arm64 at `/opt/homebrew/bin/go`. Node 22 + pnpm also present. |
| NPM version | v2.15.1, released 2026-06-03. |
| NPM API schema | **Ships real OpenAPI 3.1**, 44 paths, all groups incl. certificates/2FA/streams/dead-hosts/reports. |
| Schema caveat | In-repo `backend/schema/swagger.json` is 8 KB with **69 external `$ref`s** → needs bundling before codegen. Live instance serves dereferenced version at `GET /api/schema`. |
| Reference repo | Node/JS. Groups: `auth\|hosts\|certificates\|access-lists\|streams\|redirection\|settings`. Config `~/.npm-cli/config.yaml`, env `NPM_URL`/`NPM_TOKEN`. README admits SSL bugs fixed in 0.2.1. |
| Agent Skill spec | Local at `~/.claude/skills/agent_skills_spec.md`. Skill = folder + `SKILL.md` (YAML frontmatter `name`/`description`, optional `allowed-tools`). 226 skills already installed. |
| Production exposure | User runs NPM in prod (dwighthanoi WP server, certbot renewals) → tool touches live certs/hosts. |

---

## 3. Evaluated Approaches

### 3.1 Language / distribution

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **Go, single static binary** | Zero runtime deps — only distribution that works uniformly across Claude Code / Codex / Antigravity. Cross-compile free. Fast. | Slightly more upfront code than Node. | **CHOSEN** |
| Node/TS (fork reference repo) | Fastest v0, npm-native dist. | Requires Node runtime in every agent env; inherits a codebase whose README advertises its own bugs. | Rejected |

Key point: the Go win is **distribution**, not speed.

### 3.2 API client strategy

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **Hand-written typed client** | ~600 LOC. Full control of edge cases. No Node in Go build. | Manual upstream drift tracking. | **CHOSEN** |
| `oapi-codegen` from OpenAPI 3.1 | Free drift tracking, generated types. | OpenAPI **3.1** is oapi-codegen's weak path (3.0 solid). Requires redocly/swagger-cli bundling step → reintroduces Node toolchain into the Go build, defeating the point. ~4000 LOC generated. | Rejected |

Mitigation for drift: vendor `swagger-2.15.1.json` as **contract-test fixture**, plus `npmctl schema check` (see §5.4). ~20% of codegen value at ~5% of cost.

### 3.3 Agent surface

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **`SKILL.md` over the CLI** | Works everywhere day one. Codex/Antigravity via `AGENTS.md` pointer. Testable without an LLM. Smallest surface. | Agent shells out + parses JSON (acceptable — models are good at this). | **CHOSEN** |
| `SKILL.md` + `npmctl mcp` stdio subcommand | Typed tool args, native fit for MCP-first hosts. | Two surfaces over one client; per-client config; running process. | Deferred — structure client so this is a ~200 LOC addition later |
| MCP-only | Agent-optimal. | Rejects the stated goal of terminal management. | Rejected |

### 3.4 Safety model

| Option | Verdict |
|---|---|
| **Write-gated + dry-run** — reads free; all mutations need `--yes`; deletes print target first; `--dry-run` on all writes | **CHOSEN** |
| Profile-scoped read-only default | Deferred (v2 candidate) |
| Plain `--yes` only | Rejected — agent could delete prod host in one call |

### 3.5 Credential storage

| Option | Verdict |
|---|---|
| **Chain: keyring-first, password stored, auto-relogin** | **CHOSEN** |
| Long-lived (1yr) token, no password | Rejected — NPM has no token-revocation endpoint; leaked token = 1yr full admin |
| Env + file only, no keyring | Rejected — never uses secure store where one exists |

### 3.6 Declarative mode (`apply -f`)

Deferred to v2. Design type layer so it slots in without rework. Reason: reconciliation + drift detection + identity matching roughly doubles v1 effort; premature before real usage patterns known (YAGNI).

### 3.7 Testing

| Option | Verdict |
|---|---|
| **Fixtures only (httptest + vendored schema), hermetic** | **CHOSEN by user** |
| Docker NPM in CI | Rejected by user — heavier |

Risk accepted: never proves behavior against a real NPM. Mitigated by `npmctl schema check` (§5.4).

---

## 4. Locked Decisions

| # | Decision |
|---|---|
| D1 | Language: **Go**, single static binary |
| D2 | Repo: `~/Projects/npmctl`, module `github.com/<user>/npmctl`, binary `npmctl` |
| D3 | Scope: **full API parity — all 44 endpoints** |
| D4 | Client: hand-written, no codegen; vendor swagger as fixture |
| D5 | Agent surface: `SKILL.md` wrapping CLI; MCP deferred |
| D6 | Safety: write-gated (`--yes`) + `--dry-run` on all mutations |
| D7 | Credentials: resolution chain, keyring-first, password stored, auto-relogin |
| D8 | Output: TTY-aware (table on terminal, JSON when piped) + `-o` override |
| D9 | Tests: hermetic fixtures only |
| D10 | `apply`/`diff`/`export`: **out of scope** for v1 |

Full parity (D3) is defensible **because** of D4: the 44 endpoints collapse into ~6 repeated shapes (list/get/create/update/delete/enable/disable), so parity is ~1.5× the mid-scope option, not 3×. Cost lands in testing, not code.

---

## 5. Final Design

### 5.1 Command surface — 44 endpoints → 12 groups

| Group | Commands | Endpoints |
|---|---|---|
| `auth` | `login logout status whoami` | `POST/GET /tokens`, `/tokens/2fa` |
| `host` | `list get create update rm enable disable` | `/nginx/proxy-hosts*` |
| `redirect` | same 7 | `/nginx/redirection-hosts*` |
| `stream` | same 7 | `/nginx/streams*` |
| `dead-host` (alias `404`) | same 7 | `/nginx/dead-hosts*` |
| `cert` | `list get create rm renew download upload validate test-http dns-providers` | `/nginx/certificates*` |
| `acl` | `list get create update rm` | `/nginx/access-lists*` |
| `user` | `list get create update rm set-password permissions 2fa login-as` | `/users*` |
| `settings` | `list get set` | `/settings*` |
| `audit-log` | `list get` | `/audit-log*` |
| `report` | `hosts` | `/reports/hosts` |
| `schema` | `get check` | `/schema` + drift diff |

**Global flags:** `-p/--profile`, `--url`, `--token`, `-o/--output json|table|yaml`, `--yes`, `--dry-run`, `--insecure`, `--timeout`, `-v/--verbose`.

### 5.2 Package layout (every file target < 200 LOC)

```
~/Projects/npmctl/
  cmd/npmctl/main.go
  internal/
    npmapi/    client.go errors.go types.go tokens.go
               proxyhosts.go redirectionhosts.go deadhosts.go streams.go
               certificates.go accesslists.go users.go settings.go
               auditlog.go reports.go schema.go
    auth/      store.go keyring.go file.go env.go session.go
    config/    profiles.go        # ~/.config/npmctl/config.yaml
    cli/       root.go guard.go host.go cert.go ...   # 1 file per group
    output/    table.go json.go yaml.go tty.go
  testdata/    *.json + swagger-2.15.1.json
  skill/       SKILL.md + references/
```

Go naming: snake_case not applicable — Go convention is lowercase single-word package files. Kebab-case rule applies to JS/TS/shell only.

### 5.3 Credential resolution chain

```
1. --token flag                          explicit, one-shot
2. NPMCTL_TOKEN / NPMCTL_PASSWORD env    CI, containers, headless
3. OS keyring (zalando/go-keyring)       auto-detected
4. ~/.config/npmctl/credentials.json     0600, plaintext, explicit fallback
```

**`NPM_TOKEN` must NOT be used** — canonical env var for the npm **registry**. Collision would break `npm install` in the same shell (bug present in reference repo). All vars prefixed `NPMCTL_`.

Keyring platform coverage:

| Platform | Backend | Works |
|---|---|---|
| macOS | Keychain | yes |
| Windows | Credential Manager | yes |
| Linux desktop | Secret Service (gnome-keyring/KWallet) | yes |
| **Linux headless / container / SSH** | none (no D-Bus session) | **no → falls to env or 0600 file** |

Plaintext 0600 fallback is deliberate, not a compromise to hide. Machine-derived-key encryption is theater (key sits beside ciphertext); passphrase encryption kills unattended agent use. Same posture as `gh`, `docker login`, `kubectl`. Requirement: `npmctl auth status` always prints active backend; one-time WARN on first plaintext write.

Token lifecycle: NPM JWT default expiry ~1 day. Refresh via `GET /api/tokens` while valid; on 401 → silent full re-login from stored password. Result: unattended agent runs never hit an auth wall.

### 5.4 Write gate — single chokepoint

All mutations route through `cli.Guard`. No command calls a write method directly.

```
--dry-run          → print METHOD /path + body, exit 0, nothing sent
delete && !--yes   → GET resource, print what would be deleted, exit 3
mutate && !--yes:
   TTY             → interactive confirm
   non-TTY (agent) → exit 3, never guess
```

**Stable exit codes** (critical for agent reasoning):

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | generic error |
| 2 | usage error |
| 3 | refused / needs confirmation |
| 4 | auth failure |
| 5 | not found |
| 6 | API error |
| 7 | network error |

Error model: NPM returns `{"error":{"code":..,"message":..}}` → mapped to typed Go errors. Human mode = one line to stderr. JSON mode = `{"error":{...}}` to stderr + exit code.

### 5.5 Schema drift mitigation

`npmctl schema check` — fetch live `GET /api/schema`, diff paths + required fields vs vendored `testdata/swagger-2.15.1.json`, report added/removed/changed. Manual, on-demand, ~120 LOC. Recovers most of what integration tests would have caught, without a Docker dependency.

### 5.6 Agent skill

```
~/.claude/skills/nginx-proxy-manager/
  SKILL.md                      # frontmatter + safety protocol + recipes
  references/
    command-reference.md        # GENERATED from cobra via `npmctl docs`
    common-tasks.md             # add host+SSL, renew cert, attach ACL, debug 502
```

- Frontmatter: `name`, `description`, `allowed-tools: Bash(npmctl:*)`.
- Body encodes **mandatory protocol**: read state → `--dry-run` the write → show user exact request → only then `--yes`.
- `npmctl skill install` writes the folder and appends a pointer line to `AGENTS.md` → Codex + Antigravity pick it up from same source.
- Command reference is **generated**, so skill docs cannot drift from the binary.

---

## 6. Risks & Mitigations

| # | Risk | Severity | Mitigation |
|---|---|---|---|
| R1 | **`meta` field wipe.** NPM hosts carry free-form `meta` blob with Let's Encrypt state (`letsencrypt_agree`, `dns_challenge`, creds). Partial `PUT` silently wipes it → breaks renewal. | **Critical** — most likely prod-breaking bug in this tool | Every update = **GET → merge → PUT**, never bare PUT. Regression test with LE fixture. |
| R2 | **Cert creation slow/synchronous.** `POST /nginx/certificates` blocks on ACME challenge 30–90s+ (longer on DNS-01). Default timeouts lie — client gives up, cert actually issued. | High | Per-operation timeout override, default 180s on cert ops. Explicit progress output. |
| R3 | **Self-signed NPM certs** common in homelab/internal installs. | High — unusable day one without it | `--insecure` flag + per-profile `insecure_skip_verify`. |
| R4 | Agent misuse on production (dwighthanoi). | High | Write gate (§5.4) + skill protocol (§5.6). |
| R5 | Fixtures-only tests never exercise a real NPM. | Medium (accepted by user) | `npmctl schema check` (§5.5). |
| R6 | Full parity includes user/2FA admin endpoints — poorly documented, low daily value. | Low | Ship as thin passthrough, minimal typing. |
| R7 | Upstream NPM schema drift on version bump. | Medium | Vendored contract fixture + `schema check`. |

---

## 7. Phasing

| Phase | Contents | Exit criterion |
|---|---|---|
| P1 | Skeleton, config/profiles, auth chain, client core, output, guard → `auth`, `host`, `version` | **Tool is usable** — full proxy-host lifecycle works |
| P2 | `cert`, `redirect`, `stream`, `dead-host`, `acl` | Daily-driver parity |
| P3 | `user`, `settings`, `audit-log`, `report`, `schema check` | Full 44-endpoint parity |
| P4 | Skill + `skill install`, docs, goreleaser (darwin/linux/windows × arm64/amd64) | Distributable + agent-drivable |
| v2 | `apply` / `diff` / `export` | Declarative mode |

---

## 8. Success Metrics / Acceptance Criteria

1. `npmctl auth login` succeeds, reports credential backend used; call 48h later works with **no re-prompt**.
2. All 44 endpoints reachable; `npmctl <group> --help` self-describes.
3. `npmctl host list` → table on TTY, JSON when piped; `-o` overrides both.
4. Every write refuses without `--yes`; `--dry-run` sends **zero** HTTP requests (asserted in tests).
5. `npmctl host update` round-trips `meta` verbatim — regression test with Let's Encrypt fixture (guards R1).
6. `go test ./...` hermetic — no network, no Docker.
7. Single binary, no runtime deps, cross-compiles clean for 5 targets.
8. Claude Code drives a full host lifecycle (create → attach cert → enable → disable → delete) using only `SKILL.md`.
9. `npmctl schema check` correctly reports drift when pointed at a schema with an added/removed path.

---

## 9. Next Steps

1. Run `/ck:plan` with this report as input → phase files P1–P4.
2. Create `~/Projects/npmctl`, `git init`, `go mod init`.
3. P1 first — get `auth` + `host` working before touching anything else.
4. Decide GitHub org/user for module path (needed at `go mod init`).

**Dependencies:** `spf13/cobra`, `zalando/go-keyring`, `spf13/viper` (or plain YAML — evaluate at plan time; viper may be overkill per KISS), stdlib `net/http`.

---

## 10. Unresolved Questions

1. **GitHub module path** — `github.com/<user>/npmctl` needs the actual account. Blocks `go mod init`.
2. **Public or private repo?** Affects license choice, goreleaser config, whether a Homebrew tap is worth it.
3. **`--insecure` default** — off (safe) vs on-for-localhost. Recommend off; implementation-level call.
4. **Cert-op timeout value** — 180s proposed, unvalidated against a real DNS-01 issuance.
5. **2FA-admin depth** — thin passthrough vs typed helpers. Recommend thin (R6).
6. **viper vs plain YAML** for config — recommend plain `gopkg.in/yaml.v3` per KISS unless profile merging gets complex.

Items 3–6 are implementation-level; the plan can resolve them without further user input. Items 1–2 need a user answer before P1 starts.
