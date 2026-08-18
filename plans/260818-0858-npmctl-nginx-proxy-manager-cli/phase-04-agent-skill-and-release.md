---
phase: 4
title: "Agent Skill and Release"
status: completed
priority: P1
effort: "1-2 days"
dependencies: [1, 2, 3]
---

# Phase 4: Agent Skill and Release

# [RED TEAM] Findings 5,8,13 applied 2026-08-18. Skill grant narrowed to read + --dry-run; false safety claim removed.
<!-- Updated: Validation Session 1 — V2 public+MIT; V3 deferred commands absent from binary, not just the grant; V5 undo in skill protocol -->

## Overview

Ship the Agent Skill that lets Claude Code, Codex, and Antigravity drive `npmctl`, plus cross-platform release automation.

**The safety claim in the original draft was false** and is corrected here. It read: "the gate holds regardless of whether the agent obeys." It did not. `allowed-tools: Bash(npmctl:*)` pre-approved every command, which suppressed the tool-permission prompt — the only checkpoint an actual human sees — and `--yes` was four characters the agent supplied itself. The revised design puts one factor outside argv (`NPMCTL_ALLOW_WRITE=1`) and narrows the grant so writes re-enter the approval flow.

Honest statement of what this protects against: **accident and casual agent error, not a determined attacker who already holds the credential.** An agent running as the user can read the token file and call the API directly; no CLI-side gate can prevent that. Server-side scoping (a restricted NPM user) is the only real boundary, and it is offered as a profile option rather than mandated.

## Requirements

**Functional**
- `~/.claude/skills/nginx-proxy-manager/` with `SKILL.md` + `references/`
- `npmctl docs` generates `references/command-reference.md` from cobra
- `npmctl skill install` writes the skill folder + appends an `AGENTS.md` pointer
- goreleaser building 5 targets

**Non-functional**
- Command reference **generated** — cannot drift from the binary
- `SKILL.md` frontmatter valid per `~/.claude/skills/agent_skills_spec.md`
- Skill grant covers **read verbs and `--dry-run` only**

## Architecture

### Skill layout

```
~/.claude/skills/nginx-proxy-manager/
  SKILL.md
  references/
    command-reference.md    # GENERATED via `npmctl docs`
    common-tasks.md
    troubleshooting.md
```

### Frontmatter — narrowed grant (D5 amended)

```yaml
---
name: nginx-proxy-manager
description: >
  Manage Nginx Proxy Manager (proxy hosts, SSL certificates, redirects,
  streams, access lists) from the terminal via the npmctl CLI. Use when the
  user asks to list, create, update, enable, disable, or delete NPM hosts or
  certificates, renew SSL, or troubleshoot a proxied site.
allowed-tools:
  - Bash(npmctl auth status:*)
  - Bash(npmctl host list:*)
  - Bash(npmctl host get:*)
  - Bash(npmctl cert list:*)
  - Bash(npmctl cert get:*)
  - Bash(npmctl redirect list:*)
  - Bash(npmctl stream list:*)
  - Bash(npmctl dead-host list:*)
  - Bash(npmctl acl list:*)
  - Bash(npmctl report hosts:*)
  - Bash(npmctl audit-log:*)
  - Bash(npmctl health:*)
  - Bash(npmctl settings list:*)
  - Bash(npmctl settings get:*)
  - Bash(npmctl undo list:*)
  - Bash(npmctl undo show:*)
  - Bash(npmctl * --dry-run:*)
---
```

`name` must match the directory name (spec requirement). **No write command is pre-approved.** `undo list`/`show` are reads; `undo apply` is a write and is deliberately excluded.

**V3 strengthens this.** `user *`, `settings set`, and `login-as` are not merely absent from the grant — they are **absent from the binary** in v1 (deferred to v2, see phase-03). Scope beats tiering: a command that does not exist cannot be invoked by an agent that ignores its instructions, nor reached by prompt injection through `advanced_config` or a hostname field.

### The protocol `SKILL.md` encodes

1. **Read before write.** `npmctl host list` / `get` to establish current state.
2. **Never invent IDs.** Resolve a domain to an ID via `list` first.
3. **Dry-run every mutation.** `--dry-run` performs reads to build a truthful preview and issues no mutating request.
4. **Show the user** the dry-run output and what will change.
5. **Then ask the user to run the write**, or run it and let the permission prompt reach them. Writes additionally require `NPMCTL_ALLOW_WRITE=1`, which the agent must not set on its own.
6. **Exit 3 means refused, not failed.** Do not retry with different flags, do not set `NPMCTL_ALLOW_WRITE`, do not work around it. Surface the refusal.
7. **Exit 9 means a human must re-authenticate.** Stop and say so.
8. **Exit 8 means the write applied but nginx is unhealthy.** Report `nginx_err` and the undo-journal path immediately — the site may be down.
9. **Never retry a failed certificate operation.** Let's Encrypt allows 5 duplicate certs per week. Use `cert create --wait` and report its three-state result verbatim.
10. **Never use `advanced_config`.** It accepts raw nginx directives and can expose NPM's data volume. Refer the user to the web UI.
11. **`cert rm` revokes the certificate irreversibly.** The undo journal restores the database row, not the certificate.
12. **After any write, name the undo entry.** `npmctl undo list` shows recent pre-images and `npmctl undo show <entry>` renders one. Reading them is safe. **`npmctl undo apply` is a write** — it re-enters the gate like any other mutation, so surface it to the user rather than running it.

Rules 6 and 10 matter most. An agent that treats a refusal as transient, or that pipes user text into `advanced_config`, defeats the design. Rule 12 matters after exit 8 — the site may be down and the recovery path should be one command away, not a JSON archaeology exercise.

### Generated command reference

`npmctl docs --format markdown --out <path>` walks the cobra tree and emits every command, flag, **exit code**, and tier. Wired into the release process so skill docs cannot drift from the binary.

### `npmctl skill install`

```
npmctl skill install [--dir ~/.claude/skills] [--agents-md ./AGENTS.md]
```

1. Write `SKILL.md` + `references/` (embedded via `go:embed`).
2. Generate `references/command-reference.md`.
3. Optionally append a pointer to `AGENTS.md` for Codex/Antigravity.
4. **Idempotent with a manifest.** Store a checksum of each written file. On re-run: unchanged files are overwritten silently; **locally modified files are left alone and reported**, never clobbered. Resolves the original contradiction between "updates in place" and "warn before overwriting."
5. `AGENTS.md` dedupe keys on a stable marker comment, not a substring match, and the file is in the caller's cwd — likely git-tracked, so print a diff and require confirmation.

### Release

goreleaser targets: `darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`, `windows/amd64`. `CGO_ENABLED=0`. Version via `-ldflags -X main.version=`.

**Public repo, MIT (V2)** — module `github.com/nnmduc/npmctl`. Ship `LICENSE` (MIT), enable GitHub Releases, and a Homebrew tap is viable. Because strangers will point this at their own production NPM, the README threat-model section below is a release requirement, not a nicety.

Verify — do not assume — that `go-keyring` builds and runs with `CGO_ENABLED=0` on each target.

## Related Code Files

- Create: `skill/SKILL.md`, `skill/references/{common-tasks,troubleshooting}.md`
- Create: `internal/cli/{docs,skill}.go`
- Create: `internal/skill/{embed,manifest}.go`
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`
- Create: `README.md`, `LICENSE` (**MIT** — blocking question 2 resolved in validation)
- Modify: `internal/cli/root.go` — register `docs`, `skill`

## Implementation Steps

1. Write `SKILL.md` — narrowed frontmatter + the 11-rule protocol + quickstart.
2. Write `references/common-tasks.md`: add host with SSL; renew a cert; attach an ACL; move a host to a new upstream; find what changed (`audit-log`).
3. Write `references/troubleshooting.md`: exit-code table incl. 8/9, 502 diagnosis, `nginx_err` interpretation, undo-journal recovery, cert three-state results, self-signed upstreams.
4. `internal/cli/docs.go` — cobra tree walker → markdown incl. exit codes and tiers.
5. `internal/skill/{embed,manifest}.go` + `cli/skill.go` — checksum-based idempotent install.
6. `.goreleaser.yaml` + release workflow.
7. `README.md` — install, quickstart, **honest safety model**, agent usage, server-side scoping recipe, and a note that `~/.local/state/npmctl/undo/` holds **unredacted** pre-images at 0600 with a 30-day sweep (V6/R16).
7b. `LICENSE` — MIT. Optional Homebrew tap in the goreleaser config.
8. Verify static builds on all 5 targets.
9. **Manual acceptance** (below).

## Success Criteria

- [x] `SKILL.md` frontmatter validates against `~/.claude/skills/agent_skills_spec.md`; `name` matches directory
- [x] **No write command appears in `allowed-tools`** — test greps the shipped frontmatter
- [x] `user *`, `settings set`, `login-as` absent from the **binary** entirely (V3) — cross-checked against phase-03's cobra-tree absence test, a stronger guarantee than absence from the grant
- [x] `undo apply` absent from `allowed-tools`; `undo list`/`show` present
- [x] `npmctl docs` output covers every command, flag, exit code, and tier
- [x] Generated `command-reference.md` matches the binary's command set (test compares)
- [x] `skill install` is idempotent; a locally modified `SKILL.md` is **reported, not overwritten**
- [x] `AGENTS.md` pointer dedupes on a marker comment across rewording; shows a diff before writing
- [x] goreleaser produces working binaries for all 5 targets; `CGO_ENABLED=0` verified per target — *cross-compiled and confirmed static for all 5; the Linux amd64 binary was additionally run on Alpine (musl, no D-Bus) to prove go-keyring degrades gracefully. goreleaser itself has not been executed (needs a git tag and a release token).*
- [x] `README.md` states the honest threat model — no claim that the gate stops a determined agent
- [x] `README.md` documents the undo journal as sensitive at rest (unredacted, 0600, 30d sweep)
- [ ] `LICENSE` is MIT (done) and `go install github.com/nnmduc/npmctl/cmd/npmctl@latest` works from the public repo — *the install path cannot be verified until the repo is pushed*
- [ ] **Manual — NOT YET VERIFIED (requires a human-run agent session):** Claude Code completes create → attach cert → enable → disable → delete **against the P2 lab instance** using only `SKILL.md`; each write triggers a **permission prompt** rather than executing pre-approved; an exit-3 refusal is surfaced, not worked around; the agent reports the undo entry after each write
- [x] `go test ./...` hermetic

## Risk Assessment

| Risk | Mitigation |
|---|---|
| **R5 agent bypasses safety** | Narrow grant + out-of-argv `NPMCTL_ALLOW_WRITE=1`. Claim only accident/error protection — an agent holding the token can call the API directly, and README says so. Offer server-side scoped-user recipe as the real boundary. |
| **R7 `advanced_config` misuse** | Skill rule 10 prohibits it; binary requires `--allow-advanced-config` + TTY |
| Skill docs drift from binary | Command reference generated, regenerated at release |
| **Public repo widens the blast radius (V2)** | MIT + honest threat model in README; server-side scoped-user recipe presented as the real boundary |
| Agent runs `undo apply` unprompted | Excluded from `allowed-tools`; it is a `Guard` write and re-enters the gate |
| Agent invents host IDs | Skill rule 2 |
| Agent retries cert failures into a rate limit | Skill rule 9 + P2 attempt journal (enforced in the binary, not just prose) |
| `skill install` clobbers user edits | Checksum manifest; modified files reported, never overwritten |
| go-keyring breaking static builds | Verify per target; do not assume |
| Codex/Antigravity ignore `AGENTS.md` | Acceptable — `SKILL.md` readable directly; manual setup documented |
