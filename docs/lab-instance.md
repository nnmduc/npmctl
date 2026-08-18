# Lab instance

A disposable Nginx Proxy Manager for exercising `npmctl` before it is pointed at
anything you care about.

Hermetic tests (`go test ./...`) need none of this. The lab exists because fixtures only
prove the code agrees with itself: a fixture built from a wrong payload matches the wrong
payload. Several behavioural contracts are invisible to the OpenAPI schema, so
`npmctl schema check` cannot detect them changing — only a live instance can.

## Start it

Pin the image to **2.15.1**. Never `latest`: every payload and behaviour npmctl relies on
was verified against that specific version.

```bash
docker run -d --name npm-lab \
  -p 18181:81 -p 18080:80 \
  -e INITIAL_ADMIN_EMAIL=lab-admin@npmctl.test \
  -e INITIAL_ADMIN_PASSWORD='<a throwaway password>' \
  -v "$PWD/npm-lab/data:/data" \
  -v "$PWD/npm-lab/letsencrypt:/etc/letsencrypt" \
  jc21/nginx-proxy-manager:2.15.1
```

`INITIAL_ADMIN_EMAIL` and `INITIAL_ADMIN_PASSWORD` are **required**. NPM 2.15.1 does not
seed the old `admin@example.com` / `changeme` account — `backend/setup.js` creates the
first user only when both variables are set. Without them the instance starts, answers
`GET /api/` with `"setup": false`, and rejects every login.

Wait for it to finish setting up:

```bash
curl -s http://127.0.0.1:18181/api/
# {"status":"OK","setup":true,"version":{"major":2,"minor":15,"revision":1}}
```

`setup: true` means the admin account exists. Ports 18181/18080 avoid colliding with
whatever already holds 8080 on a typical dev machine.

## Register it as a profile

Keep the lab under its own profile and its own identity. A credential minted for the lab
must never be usable against production, which is why npmctl keys credentials by
`(profile, url, identity)`.

```bash
npmctl -p lab --url http://127.0.0.1:18181 auth login --identity lab-admin@npmctl.test
```

For an HTTPS lab with a self-signed certificate, prefer keeping verification on:

```bash
npmctl -p lab --url https://127.0.0.1:18181 --ca-cert ~/.config/npmctl/lab-ca.pem auth login ...
```

`--insecure` also works but is refused for `auth login`, because sending a password over a
connection you declined to verify is the exact case an interceptor wants.

## Run the live smoke tests

Skipped by default, so the hermetic guarantee is unaffected:

```bash
NPMCTL_E2E_URL=http://127.0.0.1:18181 \
NPMCTL_E2E_IDENTITY=lab-admin@npmctl.test \
NPMCTL_E2E_SECRET='<throwaway password>' \
  go test ./internal/npmapi/ -run E2E -v
```

They refuse to run against a non-loopback URL unless `NPMCTL_E2E_ALLOW_REMOTE=1` is set —
they create and delete real objects.

## What the lab catches that fixtures cannot

Each of these was found or confirmed by running against a live 2.15.1 instance, and each is
now pinned by a test in `internal/npmapi/e2e_test.go`:

| Contract | Behaviour |
|---|---|
| `expand` encoding | Must be **one** comma-separated parameter. Every route parses `typeof req.query.expand === "string" ? …split(",") : null`, so repeated `expand=a&expand=b` arrives as an array, fails the check, and is silently dropped — no error, just missing fields. |
| Partial update | `PUT` genuinely leaves unmentioned fields alone. `minProperties: 1` says a one-field body is legal, not that the rest survives. |
| TLS flag coercion | `internal/host.js` `cleanSslHstsData` forces flags off when prerequisites are missing: no `certificate_id` clears `ssl_forced` **and** `http2_support`; no `ssl_forced` clears `hsts_enabled`; no `hsts_enabled` clears `hsts_subdomains`. The write still returns 200, so npmctl compares request against response and warns. |
| Access-list passwords | `GET` never returns them — every item comes back with `password: ""`. This is why `acl update` demands complete arrays and refuses an existing user with an empty password. |
| Stream update fields | `POST /nginx/streams` accepts `domain_names`; `PUT /nginx/streams/{id}` does not, and both forbid unknown properties. |
| Unknown properties | Rejected with HTTP 400, which is what makes the payload allowlist load-bearing rather than decorative. |

## Certificates in the lab

Let's Encrypt cannot validate `localhost`, so real issuance needs either a public domain
pointed at the lab or the LE **staging** directory. Staging has far looser rate limits,
which also makes it the right place to exercise the attempt journal — `npmctl` refuses a
4th issuance for the same domain set within 7 days, and you want to see that refusal
somewhere harmless.

`cert rm` on a `letsencrypt` certificate performs a real ACME **revocation**. Confirm you
are on the lab profile before running it.

## Tear it down

```bash
docker rm -f npm-lab
rm -rf npm-lab/
```

The data directory holds the lab's SQLite database and any certificates it issued. It is
not sensitive in the way production is, but it is not nothing either — remove it rather
than leaving it around.
