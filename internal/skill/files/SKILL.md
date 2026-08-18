---
name: nginx-proxy-manager
description: >
  Manage Nginx Proxy Manager (proxy hosts, SSL certificates, redirects, streams,
  access lists) from the terminal via the npmctl CLI. Use when the user asks to
  list, create, update, enable, disable, or delete NPM hosts or certificates,
  renew SSL, inspect the audit log, or troubleshoot a proxied site.
license: MIT
allowed-tools:
  - Bash(npmctl auth status:*)
  - Bash(npmctl health:*)
  - Bash(npmctl version:*)
  - Bash(npmctl host list:*)
  - Bash(npmctl host get:*)
  - Bash(npmctl cert list:*)
  - Bash(npmctl cert get:*)
  - Bash(npmctl cert dns-providers:*)
  - Bash(npmctl redirect list:*)
  - Bash(npmctl redirect get:*)
  - Bash(npmctl stream list:*)
  - Bash(npmctl stream get:*)
  - Bash(npmctl dead-host list:*)
  - Bash(npmctl dead-host get:*)
  - Bash(npmctl acl list:*)
  - Bash(npmctl acl get:*)
  - Bash(npmctl report hosts:*)
  - Bash(npmctl audit-log:*)
  - Bash(npmctl settings list:*)
  - Bash(npmctl settings get:*)
  - Bash(npmctl schema check:*)
  - Bash(npmctl undo list:*)
  - Bash(npmctl undo show:*)
  - Bash(npmctl * --dry-run:*)
metadata:
  npmctl-npm-version: "2.15.1"
---

# Nginx Proxy Manager (npmctl)

`npmctl` wraps the Nginx Proxy Manager REST API. It manages live infrastructure: a
proxy host is somebody's website, and a certificate deletion is a real revocation.

**Only read commands and `--dry-run` are pre-approved above.** Every write is
deliberately excluded so it re-enters the normal tool-permission flow, where a human
sees it before it runs.

## What this protects against, honestly

Accident and casual error — not a determined attacker. An agent running as this user
can read the token file and call the API directly; no CLI-side gate can prevent that.
Server-side scoping (a restricted NPM user) is the only real boundary. Do not describe
these gates to the user as stronger than they are.

## The write gate

Every mutation requires **both**:

1. `NPMCTL_ALLOW_WRITE=1` in the environment, and
2. `--yes` on the command line.

The environment variable lives outside argv on purpose. **Do not set it yourself.** It
exists so a human is involved in the decision. Setting it defeats the entire design.

## Protocol

1. **Read before write.** Establish current state with `list` / `get` first.
2. **Never invent IDs.** Resolve a domain to an ID with `list` or `get <domain>`.
   Every command accepts a domain name in place of an ID.
3. **Dry-run every mutation.** `--dry-run` performs reads to build a truthful preview
   and issues no mutating request. It exits 0 with `"dry_run": true`.
4. **Show the user the dry-run output** and state plainly what will change.
5. **Then let the user run the write**, or run it and let the permission prompt reach
   them. Do not set `NPMCTL_ALLOW_WRITE` on their behalf.
6. **Exit 3 means refused, not failed.** Do not retry with different flags, do not set
   `NPMCTL_ALLOW_WRITE`, do not look for a workaround. Surface the refusal and its
   reason to the user.
7. **Exit 9 means a human must re-authenticate.** Stop and say so. There is no
   automatic re-login, and repeatedly trying achieves nothing.
8. **Exit 8 means the write applied but nginx did not reload.** The site may be down.
   Report `nginx_err` verbatim and name the undo entry immediately.
9. **Never retry a failed certificate operation.** Let's Encrypt allows 5 duplicate
   certificates per week. Use `cert create --wait` (the default) and report its
   three-state result — `ISSUED`, `NOT PRESENT`, or `INDETERMINATE` — verbatim. npmctl
   refuses a 4th attempt for the same domain set within 7 days; that refusal is
   correct, do not pass `--force` to get around it.
10. **Never use `advanced_config`.** It accepts raw nginx directives and can expose
    NPM's data volume over HTTP, which leads to its token-signing key. Refer the user
    to the web UI instead. The binary also requires a terminal for it, so attempting it
    will fail.
11. **`cert rm` revokes a Let's Encrypt certificate irreversibly.** The undo journal
    restores the database row, not the certificate material. Always dry-run first and
    show the user which hosts depend on it.
12. **After any write, name the undo entry.** `npmctl undo list` and
    `npmctl undo show <entry>` are reads and safe to run. **`npmctl undo apply` is a
    write** — it re-enters the gate and requires a terminal, so surface it to the user
    rather than running it.

Rules 6 and 10 matter most. An agent that treats a refusal as transient, or that pipes
user-supplied text into `advanced_config`, defeats the design.

## Output

Table on a terminal, JSON when piped. Pass `-o json` to force JSON for parsing.
Progress and warnings go to stderr, so stdout stays valid JSON.

Secrets are redacted in every renderer. If you see `[redacted]`, that is correct
behaviour — do not try to obtain the real value.

## Not available in v1

User management (`/users`), `login-as`, password changes, and `settings set` are not
implemented. These commands do not exist in the binary, so do not attempt them or
suggest workarounds.

## Quickstart

```bash
npmctl auth status                     # which instance, which identity, token expiry
npmctl health                          # is the instance up
npmctl host list                       # what is proxied
npmctl host get app.example.com        # one host, by domain
npmctl audit-log list                  # what changed recently
```

Previewing a change:

```bash
npmctl host update app.example.com --forward-port 8081 --dry-run
```

## References

- `references/command-reference.md` — every command, flag, exit code and tier
  (generated by `npmctl docs`, so it cannot drift from the binary)
- `references/common-tasks.md` — recipes for the usual jobs
- `references/troubleshooting.md` — exit codes, 502 diagnosis, recovery
