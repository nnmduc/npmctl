# Troubleshooting

## Exit codes

| Code | Meaning | What to do |
|---|---|---|
| 0 | Success | — |
| 1 | Generic error | Read the message |
| 2 | Usage error | Fix the command; check `--help` |
| 3 | **Refused** | A safety gate said no. Surface it — do not work around it |
| 4 | Auth failure | The credential was rejected; the human re-runs `auth login` |
| 5 | Not found | Resolve the name with `list` first |
| 6 | API error | NPM rejected the request; the message says why |
| 7 | Network error | **The write may have applied.** Verify with a read |
| 8 | **Applied, but nginx unhealthy** | The site may be down. Act now — see below |
| 9 | Re-authentication required | A human must run `npmctl auth login` |

Exit 3 is not a failure to route around. It means a control fired: a missing factor, a
dependency that would break, a concurrent change, or an ACME cooldown.

## Exit 8: the write applied but nginx did not reload

This is the urgent one. NPM answers HTTP 200 for a write whose nginx reload failed, so
npmctl re-reads the object and checks `meta.nginx_online`.

```bash
# 1. Read the reason npmctl printed — nginx_err is verbatim from nginx
# 2. Find the pre-image captured before the write
npmctl undo list
npmctl undo show <entry>

# 3. Restoring is a WRITE and needs a terminal. Hand this to the human:
NPMCTL_ALLOW_WRITE=1 npmctl undo apply <entry> --yes
```

The usual causes are an `advanced_config` block that nginx rejects, or a certificate
reference that no longer resolves. NPM rolls back only when `nginx -t` fails outright; a
config that is valid but wrong is applied.

## Exit 7: the write may have applied

A transport failure on a mutating call is genuinely ambiguous — NPM commits before it
responds, so a lost response is indistinguishable from a write that never happened.
npmctl never retries a mutation for this reason.

Establish ground truth with a read before doing anything else:

```bash
npmctl host get app.example.com
```

## Certificate operations

`cert create --wait` reports exactly one of three states. Report it verbatim:

| State | Meaning |
|---|---|
| `ISSUED` | The certificate exists and has an expiry date. Done. |
| `NOT PRESENT` | Issuance failed. NPM creates the row, runs certbot, and deletes the row on failure. |
| `INDETERMINATE` | The row exists without an expiry and the deadline passed. The order may still be running. **Do not retry** before the time given. |

**Never retry a certificate operation.** Let's Encrypt allows 5 duplicate certificates
per week. npmctl tracks attempts per domain set and refuses a 4th within 7 days:

```
refusing to request this certificate again: 3 attempts in the last 7 days...
The window frees up after 2026-08-25T06:13:21Z. Pass --force to override.
```

That refusal is correct. `--force` exists for a human who understands the quota cost.

Common causes of failure:

- The domain does not resolve to this NPM instance → `npmctl cert test-http --domain X`
- Port 80 is not reachable from the internet (HTTP-01 needs it)
- DNS-01 without enough propagation time → raise `--propagation-seconds`
- A `.test` / `.local` / `localhost` domain — Let's Encrypt cannot validate these at all

## A site returns 502 Bad Gateway

NPM reached the host but the upstream did not answer.

```bash
npmctl host get app.example.com     # check forward_host, forward_port, forward_scheme
```

Check in order:

1. Is the upstream actually listening on that host and port?
2. Is `forward_scheme` right? Pointing `http` at an HTTPS-only upstream gives a 502.
3. Can NPM reach the upstream? A container that resolves nowhere from NPM's network
   produces the same symptom.
4. Is the host enabled, and is `meta.nginx_online` true?

## A site returns 404 from NPM itself

Either no host matches the requested domain, or it matches a dead (404) host:

```bash
npmctl host list
npmctl 404 list
```

## Basic auth stopped working for some users

Almost certainly an access-list update that replaced the items array. NPM never returns
existing passwords, so a partial update blanks the ones it was not given. npmctl refuses
this, but a change made through the web UI or an older tool can cause it.

```bash
npmctl acl get staff        # passwords always show empty; that is normal
```

Recovery is to set the passwords again with the complete list.

## Authentication problems

```bash
npmctl auth status          # profile, identity, backend, expiry
```

- `authenticated: false` → the human runs `npmctl auth login`
- Exit 9 → the token expired or was rejected. There is no automatic re-login; on a
  2FA account it is impossible by construction, since NPM answers with a challenge
  rather than a token.
- A changed profile URL invalidates stored credentials deliberately: a credential
  minted for one instance is never replayed against another.

## Self-signed NPM certificate

Keep verification on rather than disabling it:

```bash
npmctl --ca-cert ~/.config/npmctl/lab-ca.pem -p lab host list
npmctl --pin-sha256 <base64-sha256-of-public-key> -p lab host list
```

`--insecure` exists but is refused for `auth login` — sending a password over a
connection you declined to verify is exactly the case an interceptor wants.

## Where things live

| What | Where |
|---|---|
| Config and profiles | `~/.config/npmctl/config.yaml` |
| Credentials | OS keyring, or `~/.config/npmctl/credentials.json` (0600) |
| Undo journal | `~/.local/state/npmctl/undo/` (0600, **unredacted**, swept after 30 days) |
| Certificate attempts | `~/.local/state/npmctl/cert-attempts.json` |
| Downloaded certificates | `~/.local/share/npmctl/certs/` (0600) |

The undo journal holds pre-images verbatim, including secrets such as DNS provider
credentials. Scrubbing them would make a restore impossible. Treat that directory with
the same care as the credential file.
