# Lab Instance

A disposable Nginx Proxy Manager setup for testing `npmctl` before running it in production.

Unit tests (`go test ./...`) do not require Docker. The lab instance helps verify real-world behaviors that cannot be fully captured in static test fixtures.

## Start the Lab

Pin the image to version **2.15.1**:

```bash
docker run -d --name npm-lab \
  -p 18181:81 -p 18080:80 \
  -e INITIAL_ADMIN_EMAIL=lab-admin@npmctl.test \
  -e INITIAL_ADMIN_PASSWORD='<a throwaway password>' \
  -v "$PWD/npm-lab/data:/data" \
  -v "$PWD/npm-lab/letsencrypt:/etc/letsencrypt" \
  jc21/nginx-proxy-manager:2.15.1
```

`INITIAL_ADMIN_EMAIL` and `INITIAL_ADMIN_PASSWORD` are **required**. NPM 2.15.1 creates the first user only when both environment variables are set. Without them, the instance starts in an unconfigured state and rejects logins.

Wait for setup to complete:

```bash
curl -s http://127.0.0.1:18181/api/
# {"status":"OK","setup":true,"version":{"major":2,"minor":15,"revision":1}}
```

`setup: true` confirms the admin account is ready.

## Register as a Profile

Keep the lab under its own profile and identity. Credentials in npmctl are keyed by `(profile, url, identity)` so lab credentials will never be sent to production.

```bash
npmctl -p lab --url http://127.0.0.1:18181 auth login --identity lab-admin@npmctl.test
```

For HTTPS setups with self-signed certificates:

```bash
npmctl -p lab --url https://127.0.0.1:18181 --ca-cert ~/.config/npmctl/lab-ca.pem auth login ...
```

## Run Live Smoke Tests

Smoke tests are skipped by default during normal unit tests:

```bash
NPMCTL_E2E_URL=http://127.0.0.1:18181 \
NPMCTL_E2E_IDENTITY=lab-admin@npmctl.test \
NPMCTL_E2E_SECRET='<throwaway password>' \
  go test ./internal/npmapi/ -run E2E -v
```

Tests only run against loopback addresses unless `NPMCTL_E2E_ALLOW_REMOTE=1` is explicitly set.

## Verified NPM Behaviors

The following NPM behaviors were verified against a live 2.15.1 instance and are tested in `internal/npmapi/e2e_test.go`:

| Item | Behavior |
|---|---|
| `expand` parameter | Must be a single comma-separated parameter. Passing repeated parameters causes NPM to ignore the field. |
| Partial updates | `PUT` requests preserve unmentioned fields. |
| TLS flag dependencies | Flags like `ssl_forced` and `http2_support` require a valid certificate. If missing, NPM resets them automatically. npmctl checks and warns if flags were reset. |
| Access-list passwords | `GET` requests never return user passwords. Updates require submitting complete user lists. |
| Stream fields | `POST /nginx/streams` accepts `domain_names`, while `PUT /nginx/streams/{id}` does not. Unknown fields are rejected with HTTP 400. |

## Certificates in the Lab

Let's Encrypt cannot validate `localhost` domains. For testing certificates, use a public domain or the Let's Encrypt staging environment.

Note that running `cert rm` on a Let's Encrypt certificate performs an ACME certificate revocation.

## Tear Down

```bash
docker rm -f npm-lab
rm -rf npm-lab/
```
