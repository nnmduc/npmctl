# Security and Threat Model

## Threat Model

Read this before using npmctl in production environments.

### What the safety gates protect against
- Accidental commands and typos
- AI agents that misinterpret instructions
- Confusing dry-run previews with actual executed changes

### What the safety gates cannot protect against
- Compromised user credentials
- Local processes running as your user

Any process running under your local user account can read stored tokens and call the NPM API directly. Client-side tools cannot prevent this.

### Server-Side Isolation (Recommended)

The most effective boundary is on the server:

1. In the NPM web UI, create a dedicated user for automation.
2. Grant only the permissions needed (for example, manage proxy hosts, but not certificates or settings).
3. Log in with that profile:
   ```bash
   npmctl -p automation auth login --url https://npm.example.com --identity bot@example.com
   ```

If the credentials are leaked or misused, potential damage is limited by the server-side account permissions.

## Sensitive Files on Disk

| Path | Contents | File Mode |
|---|---|---|
| OS keyring, or `~/.config/npmctl/credentials.json` | Bearer token | 0600 (read/write by user only) |
| `~/.local/state/npmctl/undo/` | Pre-change backups (unredacted) | 0600, cleaned after 30 days |
| `~/.local/share/npmctl/certs/` | Certificate archives (includes private keys) | 0600 |

### Undo Backups and Redaction

Undo records store raw state data so they can be restored accurately. This means DNS provider credentials in certificate backups are stored in plain text with 0600 file permissions.

In interactive output, logs, and `--dry-run` previews, secrets and tokens are always redacted.

## Advanced Configuration Risks

The `advanced_config` field allows custom nginx configuration directives. An invalid or harmful block could expose internal files or cause routing issues.

To protect against accidents:
- Changes to `advanced_config` require the `--allow-advanced-config` flag.
- Changes require an interactive terminal and display a diff before applying.
- Pre-built agent skills forbid modifying `advanced_config` by default.
