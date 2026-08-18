# npmctl

[![CI](https://github.com/nnmduc/npmctl/actions/workflows/ci.yml/badge.svg)](https://github.com/nnmduc/npmctl/actions/workflows/ci.yml)
[![Release](https://github.com/nnmduc/npmctl/actions/workflows/release.yml/badge.svg)](https://github.com/nnmduc/npmctl/actions/workflows/release.yml)
[![GitHub Release](https://img.shields.io/github/v/release/nnmduc/npmctl?color=blue)](https://github.com/nnmduc/npmctl/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A command-line tool for [Nginx Proxy Manager](https://nginxproxymanager.com), built for safe manual use and AI agent automation.

Tested with **NPM 2.15.1**. Use `npmctl schema check` to detect API changes in newer NPM versions.

```bash
npmctl host list
npmctl host get app.example.com
npmctl host update app.example.com --forward-port 8081 --dry-run
```

## Why it exists

Nginx Proxy Manager has a great web UI, but scripts and AI agents need safeguards when making automated changes. A mistake can take down a live website or revoke a certificate.

npmctl is built with **safety first**:

- Every write requires two factors (`NPMCTL_ALLOW_WRITE=1` and `--yes`).
- `--dry-run` previews changes without making any API calls.
- Backups are saved automatically before every change for rollback with `npmctl undo`.
- Mutating HTTP requests are never retried automatically.
- Consistent exit codes help scripts and AI agents handle errors reliably.

## Install

### One-line installer (macOS & Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/nnmduc/npmctl/main/install.sh | bash
```

See [INSTALLATION.md](INSTALLATION.md) for custom install directories, specific versions, Go install, and pre-built binaries.

## Quickstart

```bash
# Log in once (stores a 30-day token; password is discarded)
npmctl auth login --url https://npm.example.com

npmctl auth status        # Profile, identity, and token expiry
npmctl health             # Check if NPM instance is up
npmctl host list          # Table in terminal, JSON when piped
```

Making changes requires both the environment variable and `--yes`:

```bash
export NPMCTL_ALLOW_WRITE=1
npmctl host create --domain app.example.com \
  --forward-host 10.0.0.9 --forward-port 8080 --yes
```

### Use with AI agents

Install skills for Claude Code, Antigravity, Codex, Cursor, OpenCode, or Gemini CLI:

```bash
npmctl skill install
```

The installed skill only permits read commands and `--dry-run` by default. Any change still prompts a human for approval.

## Documentation

- [Safety and the Write Gate](docs/safety.md): Two-factor write gate, execution order, exit codes, undo journal, and certificate rate limits.
- [Security and Threat Model](docs/security.md): Threat model, sensitive files on disk, and advanced configuration risks.
- [Configuration and Profiles](docs/configuration.md): Profiles, multi-instance setups, and TLS settings.
- [Development](docs/development.md): Building, testing, linting, and live lab testing.

## TODO

User management (`/users`), `login-as`, password changes, and `settings set` are deliberately omitted from the binary so they cannot be triggered accidentally or by an automated agent. `settings list` and `settings get` are available.

## License

MIT - see [LICENSE](LICENSE).
