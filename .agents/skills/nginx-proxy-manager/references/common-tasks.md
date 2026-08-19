# Common tasks

Every write below assumes the human supplies `NPMCTL_ALLOW_WRITE=1` (or the AI agent runs in automation mode via `--ignore-safe-mode`). Dry-run first.

## Add a proxy host

```bash
# 1. Check nothing already serves the domain
npmctl host list

# 2. Preview
npmctl host create \
  --domain app.example.com \
  --forward-host 10.0.0.9 --forward-port 8080 \
  --block-exploits --websocket-upgrade \
  --dry-run

# 3. The human runs the write
NPMCTL_ALLOW_WRITE=1 npmctl host create \
  --domain app.example.com \
  --forward-host 10.0.0.9 --forward-port 8080 \
  --block-exploits --websocket-upgrade --yes
```

## Add a host with SSL

TLS flags depend on a certificate being attached. NPM silently forces `ssl_forced`,
`http2_support`, `hsts_enabled` and `hsts_subdomains` off when their prerequisites are
missing — npmctl warns when that happens, so read stderr.

Order the certificate first, then attach it:

```bash
# 1. Will the HTTP-01 challenge even reach the domain?
npmctl cert test-http --domain app.example.com

# 2. Order it. --wait is the default and reports one definite outcome.
NPMCTL_ALLOW_WRITE=1 npmctl cert create --domain app.example.com --yes

# 3. Find the new certificate's ID
npmctl cert list

# 4. Attach it and enable TLS in one update
NPMCTL_ALLOW_WRITE=1 npmctl host update app.example.com \
  --certificate-id 5 --ssl-forced --http2 --hsts --yes
```

`--certificate-id new` on a host write asks NPM to order a certificate inside the host
write. It works, and npmctl widens the timeout for it, but it couples two failures into
one command. Prefer the two-step form above.

## Renew a certificate

```bash
npmctl cert list                                    # check expires_on first
NPMCTL_ALLOW_WRITE=1 npmctl cert renew example.com --yes
```

If it fails, **stop**. Report the error. Do not retry — see `troubleshooting.md`.

## Move a host to a new upstream

```bash
npmctl host get app.example.com                     # note the current upstream
npmctl host update app.example.com --forward-host 10.0.0.20 --dry-run
NPMCTL_ALLOW_WRITE=1 npmctl host update app.example.com --forward-host 10.0.0.20 --yes
npmctl undo list                                    # the pre-image, in case it breaks
```

Only the flags you pass are sent, so everything else keeps its current value.

## Attach an access list (basic auth)

Creating a list needs every user's password up front, because NPM never returns
passwords afterwards:

```bash
NPMCTL_ALLOW_WRITE=1 npmctl acl create --name staff \
  --item alice:alicepassword \
  --item bob:bobpassword \
  --client allow:10.0.0.0/8 --yes

npmctl acl list                                     # get the ID
NPMCTL_ALLOW_WRITE=1 npmctl host update app.example.com --access-list-id 2 --yes
```

**Updating a list replaces its users wholesale.** To add carol while keeping alice and
bob, you must pass all three with their real passwords:

```bash
npmctl acl update staff \
  --item alice:alicepassword --item bob:bobpassword --item carol:carolpassword \
  --dry-run     # shows ADDED / PASSWORD SET / REMOVED per user
```

Omitting a user removes them. npmctl refuses an existing username with an empty
password, because NPM would accept it and blank that user's credentials.

## Redirect one domain to another

```bash
NPMCTL_ALLOW_WRITE=1 npmctl redirect create \
  --domain old.example.com --forward-domain new.example.com \
  --http-code 301 --yes
```

## Park a domain (404 host)

```bash
NPMCTL_ALLOW_WRITE=1 npmctl 404 create --domain parked.example.com --yes
```

## Forward a raw TCP port

```bash
NPMCTL_ALLOW_WRITE=1 npmctl stream create \
  --incoming-port 2222 --forwarding-host 10.0.0.4 --forwarding-port 22 --tcp --yes
```

Streams are addressed by ID or incoming port. `--domain` works on create only; the
update endpoint rejects that field.

## Find what changed

```bash
npmctl audit-log list                    # newest first, capped at 100 by the server
npmctl audit-log list --expand user      # include who did it
npmctl audit-log list --query certificate
```

There is no pagination — the API offers none.

## Temporarily disable a host

```bash
NPMCTL_ALLOW_WRITE=1 npmctl host disable app.example.com --yes
NPMCTL_ALLOW_WRITE=1 npmctl host enable app.example.com --yes
```

Prefer this over deleting: it is reversible and keeps the configuration.

## Check a whole instance

```bash
npmctl health          # up, set up, version
npmctl report hosts    # counts by type
npmctl settings list   # how it is configured
npmctl schema check    # has the API drifted from what npmctl expects
```
