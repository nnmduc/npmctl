# Safety and the Write Gate

npmctl puts safety first. Destructive actions and unintended changes are guarded by confirmation gates, preview modes, and automatic backups.

## The Write Gate

Every write operation requires two factors:
1. `NPMCTL_ALLOW_WRITE=1` in the environment
2. `--yes` on the command line

If either is missing, the command exits with code **3** and makes no changes.

`--yes` can easily be typed by an AI agent. The environment variable cannot be added to `argv` by an agent composing a command, which keeps human permission prompts in the loop.

### Execution Steps

Every change runs through these checks in order:

1. **Resolve and preview**: Reads current state by domain or ID.
2. **Dependency scan**: Checks for references before deleting any resource (requires `--cascade-ack` if dependents exist).
3. **Authorize**: Verifies write authorization factors.
4. **Compare-and-swap**: Re-checks `modified_on` before writing to catch concurrent changes.
5. **Save backup state**: Saves the previous state to the undo journal before sending the request.
6. **Execute**: Sends the API request. Mutating HTTP requests are never retried automatically.
7. **Verify**: Checks `meta.nginx_online`. If the nginx reload failed, exits with code **8** and displays the error message.

### Mutation Tiers

| Tier | Applies To | Additional Requirement |
|---|---|---|
| Normal | Create, update, enable, disable | Standard two-factor write gate |
| Destructive | Any delete, including certificate removal | Dependency check, `--cascade-ack` when dependents exist |
| Privileged | `advanced_config` writes, `undo apply` | Interactive terminal and typed confirmation |

## Exit Codes

npmctl uses consistent exit codes so scripts and agents can understand results accurately:

| Code | Meaning | Details |
|---|---|---|
| 0 | Success | Operation completed successfully |
| 1 | Generic error | General error occurred |
| 2 | Usage error | Invalid arguments or flags |
| 3 | Refused | Safety gate rejected the action; do not retry automatically |
| 4 | Auth failure | Authentication failed or token expired |
| 5 | Not found | Requested resource does not exist |
| 6 | API error | NPM returned an error response |
| 7 | Network error | Connection failed; verify state with a read before retrying |
| 8 | Reload failed | Write succeeded, but nginx reload failed; site may be down |
| 9 | Re-auth required | Interactive re-authentication is needed |

## Undo Journal

Every change saves the previous state before running.

```bash
# List recent backup states (newest first)
npmctl undo list

# View details for a backup entry
npmctl undo show <entry>

# Replay and restore a previous state
npmctl undo apply <entry>
```

Running `undo apply` is treated as a write operation: it requires the write gate, passes compare-and-swap checks, creates its own backup record, and checks nginx status afterwards.

> **Important Note on Certificates**: Deleting a Let's Encrypt certificate sends a revocation request to the Certificate Authority. The undo journal can restore the database record in NPM, but the revoked certificate itself cannot be un-revoked.

## Certificates and Rate Limits

Let's Encrypt enforces a limit of 5 duplicate certificates per week. To protect against accidental rate limit bans:

- npmctl never retries certificate creation automatically.
- Attempts are tracked per domain list. A 4th attempt within 7 days is blocked (override with `--force`).
- Status is clearly reported as `ISSUED`, `NOT PRESENT`, or `INDETERMINATE`.
