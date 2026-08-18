# Red Team Review — Failure Mode Analyst (Flow Tracer)

**Target:** `plans/260818-0858-npmctl-nginx-proxy-manager-cli`
**Reviewer role:** hostile operational review; trace each flow end to end
**Evidence base:** plan files (cited by line) + NPM v2.15.1 schema
(`https://raw.githubusercontent.com/NginxProxyManager/nginx-proxy-manager/v2.15.1/backend/schema/...`)
**Date:** 2026-08-18

Verdict up front: the plan's safety story rests on one gate (`--yes` + `--dry-run`) that the
agent itself controls, and on prose in `SKILL.md`. There is no write-verification step, no
undo state, no idempotency, and one transport rule (auto-retry) that directly contradicts the
"never retry cert creation" rule. Two payload shapes in P2 are wrong against the v2.15.1
schema and the hermetic-fixtures decision (D9) guarantees no test will catch them.

---

## Finding 1: Transport-level auto-retry silently duplicates non-idempotent writes and burns Let's Encrypt quota

- **Severity:** Critical
- **Location:** `phase-01-core-foundation.md:191`, section "Implementation Steps"; contradicted by `phase-02-host-resources.md:127`, section "Risk Assessment"
- **Flaw:** The client is specified with "single retry on 5xx/network with backoff" with **no method or idempotency filter**. Every `POST` goes through it, including `POST /nginx/certificates`, `POST /nginx/proxy-hosts`, `POST /nginx/certificates/{id}/renew`, and `POST /users/{id}/login`. NPM commits before responding; a network-level failure after commit is indistinguishable from a failure before commit. Retrying is therefore a duplicate write, not a recovery.
- **Failure scenario:**
  1. Agent runs `npmctl cert create -d wp.dwighthanoi.com --yes`.
  2. NPM starts certbot; ACME HTTP-01 succeeds at ~60s; NPM writes the cert row.
  3. The reverse proxy in front of NPM (or the NPM nginx reload triggered by the write itself) drops the connection before the 201 reaches npmctl.
  4. `client.go` classifies this as a network error and **retries the POST automatically** — the agent never sees a decision point, `Guard` is not re-consulted, `SKILL.md` rule 7 is irrelevant because the retry happens two layers below the CLI.
  5. NPM issues a second identical certificate. Duplicate-certificate limit is 5/week; two more manual attempts and the domain is locked out for a week — on a live WordPress host with an expiring cert.
  Same trace for `POST /nginx/proxy-hosts` yields two hosts claiming the same `domain_names`, which NPM will generate conflicting nginx server blocks for.
- **Evidence:** `phase-01-core-foundation.md:191` — "single retry on 5xx/network with backoff, no retry on 4xx"; `phase-02-host-resources.md:127` — "Let's Encrypt rate limits | Never auto-retry cert creation. Ever." The plan states both rules and never reconciles them. Schema: `POST /nginx/certificates` (`paths/nginx/certificates/post.json`) returns `201` with a freshly created object and takes no client-supplied idempotency key — there is no request field npmctl can use to make the retry safe.
- **Suggested fix:** Retry **only** `GET` and the token endpoints, and only on connect-time errors (`net.OpError` before any bytes written) or 502/503/504. Explicitly forbid retry on all `POST`/`PUT`/`DELETE` in `client.go` and add a unit test that fails if a mutating method is retried. On a lost response for a mutating request, exit 7 with a message naming the read command that establishes ground truth (`npmctl cert list`, `npmctl host list --domain X`) and stating that the write **may have applied**.

---

## Finding 2: No write succeeds/fails verification — NPM returns 200 while nginx is broken; `meta.nginx_err` is never checked

- **Severity:** Critical
- **Location:** `phase-01-core-foundation.md:102-126`, section "Write gate"; `phase-01-core-foundation.md:143`; only mention of `nginx_err` anywhere is `phase-04-agent-skill-and-release.md:106` (a hand-written troubleshooting doc)
- **Flaw:** NPM's write path is: validate → persist → regenerate nginx config → reload. The HTTP 200 is returned for the *persist*, and per-host reload status is reported out-of-band in `meta.nginx_online` / `meta.nginx_err`. `Guard`'s contract ends at `do() error` — a 2xx is treated as success. Nothing in P1–P3 re-reads the resource or inspects `meta` after a write, so npmctl will report success on a change that took the site off TLS or produced an invalid config.
- **Failure scenario:**
  1. Agent applies a requested tweak: `npmctl host update 3 --advanced-config "$(cat snippet.conf)" --yes`. The snippet contains a directive invalid in the `server` context.
  2. NPM validates the JSON (the schema types `advanced_config` as a bare `"type": "string"` — no nginx syntax validation), persists it, writes `/data/nginx/proxy_host/3.conf`, reload fails.
  3. NPM marks the host `nginx_online: false`, `nginx_err: "<nginx test output>"`. HTTP response is **200 with the updated object**.
  4. npmctl prints the updated row, exits 0. Agent tells the user "done".
  5. `wp.dwighthanoi.com` is now down. There is no alert, no non-zero exit, and no captured previous `advanced_config` to restore (see Finding 4). Diagnosis at 3am requires knowing to inspect `meta.nginx_err`, which is documented only in a prose reference file the agent may not read.
- **Evidence:** Schema `components/proxy-host-object.json` — `meta` is a required property whose example is exactly `{"nginx_online": true, "nginx_err": null}`; `paths/nginx/proxy-hosts/hostID/put.json` `200` example likewise returns `"meta": {"nginx_online": true, "nginx_err": null}`. Plan: `phase-01-core-foundation.md:143` acknowledges `meta` carries "nginx status (`nginx_online`, `nginx_err`)" and then only uses that fact to justify *not sending* `meta` — never to *read* it. No success criterion in any phase mentions post-write verification (`phase-01:205-221`, `phase-02:108-121`).
- **Suggested fix:** Make `Guard` responsible for the whole write transaction: after any mutation on a config-generating resource (proxy/redirect/dead hosts, streams, access lists, certificates, `settings/default-site`), re-`GET` the resource and assert `meta.nginx_online != false` and `meta.nginx_err == null`. On failure: print `nginx_err` verbatim to stderr, exit non-zero with a dedicated code (e.g. 8 = `applied but nginx unhealthy`), and print the exact revert command using the pre-write state captured per Finding 4. Add this as a P1 success criterion and a fixture test with an `nginx_err` response.

---

## Finding 3: The write gate is agent-self-service; the claim that "the gate holds regardless of whether the agent obeys" is false

- **Severity:** Critical
- **Location:** `phase-04-agent-skill-and-release.md:130`, section "Risk Assessment"; `phase-04-agent-skill-and-release.md:53`, section "SKILL.md frontmatter"; `phase-01-core-foundation.md:119-126`
- **Flaw:** The only thing separating "read-only" from "irreversible production mutation" is a flag on the same command line the agent composes. `allowed-tools: Bash(npmctl:*)` pre-authorizes every `npmctl` invocation, including `npmctl host rm 12 --yes`, so the tool-permission prompt — the actual human-in-the-loop — never fires. The plan's stated mitigation for R4 is therefore self-refuting: exit 3 is not a boundary, it is a hint the agent can erase by appending four characters.
- **Failure scenario:**
  1. User: "the staging host is broken, clean it up."
  2. Agent resolves the wrong ID (two hosts share a `nice_name`-like domain), runs `npmctl host rm 12 --dry-run` → exit 0, prints `DELETE /api/nginx/proxy-hosts/12` and nothing about what host 12 is (dry-run sends zero requests, so no name is resolved — see Finding 9).
  3. Agent, per protocol steps 3–4 (`phase-04:63-64`), re-runs `npmctl host rm 12 --yes`. `Bash(npmctl:*)` is pre-approved → no prompt reaches the user.
  4. Production host deleted, nginx regenerated without it, site 404s. No export exists (D10), no undo state (Finding 4).
  A misbehaving or merely sloppy agent reaches the same place faster: a generic "retry non-zero exit with more flags" loop turns exit 3 into `--yes` on attempt two. `SKILL.md` rule 6 (`phase-04:66`) is a request, not a control.
- **Evidence:** `phase-04-agent-skill-and-release.md:130` — "the gate holds regardless of whether the agent obeys"; `phase-04-agent-skill-and-release.md:53` — `allowed-tools: Bash(npmctl:*)`; `phase-01-core-foundation.md:126` — "mutating && !`--yes` && !TTY | exit 3", i.e. the only enforcement is the absence of a flag the caller supplies. `plan.md:21` — "Every mutation passes one gate requiring `--yes`".
- **Suggested fix:** Move at least one factor of authorization outside the agent's argv. Concretely: (a) require `NPMCTL_ALLOW_WRITE=1` in the environment *and* `--yes` for destructive verbs, so enabling writes is a user/profile act, not a per-command act; (b) narrow the skill grant to read verbs — `allowed-tools: Bash(npmctl list:*), Bash(npmctl get:*), Bash(npmctl:* --dry-run)` — so every real write triggers a tool-approval prompt; (c) bind `--yes` to a nonce printed by the immediately preceding `--dry-run` (`--confirm <nonce>`), which also closes Finding 8. Then rewrite the R4 row to state the true property: the gate prevents *accidental* writes, not adversarial or confused ones.

---

## Finding 4: Zero recovery path — no pre-write state capture, and export deferred; the one place prior state is fetched is the path the skill never takes

- **Severity:** Critical
- **Location:** `phase-01-core-foundation.md:124`, section "Write gate"; `plan.md:51` (D10)
- **Flaw:** `Guard` fetches and prints the target resource **only** in the `destructive && !--yes` refusal branch. On the path the skill actually prescribes (`--dry-run`, then `--yes`), no GET of prior state ever happens: dry-run is required to send zero requests, and the `--yes` run skips the preview branch. So at the moment a destructive or lossy write executes, the previous state exists nowhere — not on disk, not in scrollback, not in an export (D10 defers `export`). Recovery from a wrong `host rm`, a `meta` overwrite, a bad `advanced_config`, or an ACL replacement is "reconstruct from memory."
- **Failure scenario:**
  1. Agent deletes proxy host 12 (Finding 3's trace, or simply an off-by-one after a `list`).
  2. User: "put it back." Nothing recorded `forward_host`, `forward_port`, `advanced_config`, `locations[]`, `access_list_id`, `certificate_id`, `hsts_*`, `ssl_forced`.
  3. `GET /audit-log` is the only trace. NPM's audit log records the object type/id and action meta — it is not a full pre-image, and P3 exposes it read-only with no restore path (`phase-03:76-78`).
  4. Reconstruction is guesswork. If `certificate_id` pointed at a cert the agent also deleted, re-issuance now competes with Let's Encrypt rate limits already consumed.
- **Evidence:** `phase-01-core-foundation.md:124` — "destructive && !`--yes` | GET the resource first, print what would be deleted, exit 3" (only branch that reads prior state); `phase-01-core-foundation.md:123` — dry-run "return nil without calling `do`"; `plan.md:79` — "`--dry-run` sends **zero** HTTP requests (asserted)"; `plan.md:51` — "D10 | `apply`/`diff`/`export` out of scope (v2)". Deferring export is defensible only if the gate is a real boundary; per Finding 3 it is not, so D10 removes the last line of defense.
- **Suggested fix:** Cheap, in-scope, no new command surface: `Guard` always GETs the target before a mutating call and appends the pre-image JSON to `~/.local/state/npmctl/undo/<profile>/<ts>-<resource>-<id>.json` (0600), plus one stderr line naming the file. That is ~30 LOC and turns every mistake into a recoverable one. Reconsider D10 to the extent of a read-only `npmctl export --all > file.json` built from existing list endpoints (~40 LOC) and recommend running it once before the first agent-driven write.

---

## Finding 5: `cert create` payload as specified is invalid against the v2.15.1 schema, and D9 guarantees no test catches it

- **Severity:** High
- **Location:** `phase-02-host-resources.md:53-63`, section "Certificates — verified request shape"; success criterion `phase-02-host-resources.md:113`
- **Flaw:** The plan's `meta` for cert creation contains `letsencrypt_email` and `letsencrypt_agree`. The v2.15.1 certificate `meta` schema is `additionalProperties: false` and permits only `certificate`, `certificate_key`, `dns_challenge`, `dns_provider_credentials`, `dns_provider`, `letsencrypt_certificate`, `propagation_seconds`, `key_type`. Neither planned key is allowed. NPM validates request bodies against this schema before touching certbot, so the documented payload is a 400 on every invocation — the flagship P2 command as designed cannot work. Because D9 mandates fixtures-only tests, the test suite validates npmctl against a fixture npmctl's own author wrote; the drift detector (`schema check`) compares *paths, methods and required fields*, not permitted properties, so it will not fire either.
- **Failure scenario:**
  1. Implementer follows `phase-02:53-63` literally, writes the `meta` builder, writes a fixture asserting that exact JSON. `go test ./...` green. Success criterion `phase-02:113` ("builds correct `meta`") passes.
  2. First real run against the production instance: `400 {"error":{"code":400,"message":"should NOT have additional properties"}}`.
  3. Worse variant if NPM tolerates it in some path: `letsencrypt_agree` is silently dropped, certbot runs without the ToS flag, and the failure surfaces 60s later as an opaque ACME error while the user waits.
  A related unhandled path: `common.json#/properties/certificate_id` accepts the literal string `"new"`, meaning `POST/PUT /nginx/proxy-hosts` can itself trigger a blocking ACME issuance. The plan gives cert commands a 180s timeout and everything else the 30s default (`phase-02:27`), so a host write that requests a new cert times out at 30s while issuance continues server-side — straight into Finding 1's retry and Finding 7's rate limit.
- **Evidence:** Schema `components/certificate-object.json` — `meta` is `"additionalProperties": false` with the eight properties listed above; `paths/nginx/certificates/post.json` example body is `{"provider":"letsencrypt","domain_names":[...],"meta":{"dns_challenge":false}}` — no email, no agree flag. `common.json#/properties/certificate_id` — `anyOf: [integer>=0, string pattern "^new$"]`. Plan: `phase-02-host-resources.md:57-62` shows the invalid `meta`; `plan.md:50` — "D9 | Hermetic fixture tests only"; `phase-03-admin-resources.md:69` — the differ covers only "paths added/removed, methods added/removed, required-field changes".
- **Suggested fix:** Re-derive every request body from the v2.15.1 schema files, not from the brainstorm, before P2 starts; the LE email comes from NPM settings/user, not the request. Extend `schema check` to diff `additionalProperties`, `enum`, and property sets — otherwise it does not mitigate R5/R7 for the failure mode that actually bites. Add a single opt-in, clearly-marked live smoke test (`NPMCTL_E2E_URL`, skipped by default) covering `cert validate`/`test-http` and one host round-trip; D9 as written means the first execution of every payload happens against production.

---

## Finding 6: Access-list update uses wrong field names and, via the suggested GET-modify-PUT helper, wipes every basic-auth password

- **Severity:** High
- **Location:** `phase-02-host-resources.md:85-87`, section "Access lists"
- **Flaw:** Two defects in three lines. (a) The plan names the nested arrays `access_items` and `access_clients`; the PUT request body properties are `items` and `clients`, and the body is `additionalProperties: false` — the planned payload 400s. (b) The suggested `--add-item`/`--remove-item` GET-modify-PUT convenience is a data-loss machine: the GET response returns items as `{id, created_on, modified_on, access_list_id, username, password: "", meta, hint: "a****"}`, while the PUT item schema permits only `{username, password}`. A round-trip therefore both fails validation on the extra keys and, once those are stripped, resends `password: ""` for every pre-existing user.
- **Failure scenario:**
  1. Access list "wp-admin" protects the WordPress admin path; it has 3 users with real passwords.
  2. Agent: "add user `deploy` to the ACL" → `npmctl acl update 1 --add-item deploy:secret --yes`.
  3. Helper GETs list 1, appends the new item, strips unknown keys, PUTs `items: [{username:"admin",password:""},{username:"ops",password:""},{username:"asdad",password:""},{username:"deploy",password:"secret"}]`.
  4. NPM replaces all items. `password` is a permitted string with no `minLength`, so the three existing users are rewritten with empty passwords. nginx reloads; basic auth for those users is now broken (or, depending on htpasswd generation, degenerate). npmctl exits 0 having "added a user".
  5. Nobody notices until an operator cannot log in. Original passwords are unrecoverable — the API only ever returns `hint`.
- **Evidence:** Schema `paths/nginx/access-lists/listID/put.json` — request body `"additionalProperties": false`, properties are `name`, `satisfy_any`, `pass_auth`, `items`, `clients`; `common.json#/properties/access_items` items are `additionalProperties: false` with only `username` (`minLength: 1`) and `password` (no `minLength`); the same file's `200` example shows the GET/response form with `id`, `access_list_id`, `password: ""`, `hint: "a****"`. Plan: `phase-02-host-resources.md:85` — "`access_items` (basic auth users) and `access_clients` (IP allow/deny) are nested arrays"; `phase-02-host-resources.md:87` — "Consider `--add-item` / `--remove-item` convenience flags that GET-modify-PUT."
- **Suggested fix:** Fix the field names from the schema. Drop `--add-item`/`--remove-item` for v1, or make them refuse when any existing item's password is unavailable (always) unless the caller re-supplies every password explicitly. `acl update` must reject a payload containing an item whose password is empty while `username` matches an existing item, and dry-run output for ACLs must render an explicit item-level diff with `REMOVED`/`PASSWORD RESET` lines rather than the raw body.

---

## Finding 7: The cert-timeout mitigation is advisory text with no state; it neither prevents the retry nor tells the truth about what to check

- **Severity:** High
- **Location:** `phase-02-host-resources.md:71-77`, section "R2 — cert creation blocks"; `phase-02-host-resources.md:126-127`
- **Flaw:** The entire mitigation is a printed sentence plus an instruction in `SKILL.md`. There is no persisted attempt record, no cooldown, no dedupe, and no polling. "Run `npmctl cert list` to check" is also not reliable: NPM creates the certificate row *before* running certbot and deletes it if issuance fails, so the list is ambiguous during the window right after a client-side timeout. And the 180s default is too low for the DNS-01 case the plan itself calls out: `propagation_seconds` alone is commonly 60–120s on top of ACME round-trips.
- **Failure scenario:**
  1. `npmctl cert create -d wp.dwighthanoi.com --provider letsencrypt --yes`. HTTP-01 issuance takes 200s (DNS/rate-limit backoff at the ACME server).
  2. Client hits 180s, prints "may have succeeded, check `cert list`", exits non-zero.
  3. Agent immediately runs `npmctl cert list`. Issuance is still in flight — either no row, or a row with no `expires_on`. The plan defines no interpretation for "row exists but not yet valid," so the agent reasonably concludes it failed.
  4. Agent surfaces this to the user; user says "try again." Attempt 2. Then a third. Three duplicate certificates for the same FQDN set out of a weekly allowance of 5 — and, per Finding 1, each attempt may itself have been retried once at the transport layer, so the real count can be 6 and the domain is now rate-limited with the old cert expiring.
  5. There is no local record that three attempts happened, so nothing in npmctl can warn on attempt 4.
- **Evidence:** `phase-02-host-resources.md:74-76` — "cert create/renew default **180s**" / "On timeout, do **not** report plain failure. Print: the operation may have succeeded, run `npmctl cert list` to check."; `phase-02-host-resources.md:127` — "Never auto-retry cert creation. Ever. Surface the error and stop." (the plan forbids *automatic* retry but leaves human/agent-driven retry entirely unguarded, and Finding 1 breaks even the automatic case). Schema: `paths/nginx/certificates/post.json` has no async/job response — the only documented outcome is a synchronous `201` or `400`, so a client-side timeout genuinely has no defined resolution path.
- **Suggested fix:** Persist an attempt journal per profile+domain-set (`~/.local/state/npmctl/cert-attempts.json`): timestamp, outcome, and a derived duplicate count. `cert create`/`renew` refuses with exit 3 and a plain explanation when ≥3 attempts for the same domain set occurred in the last 7 days, `--force` to override. Raise the DNS-01 default to `propagation_seconds + 240s` computed from the payload. Replace the ambiguous message with a deterministic resolution routine: poll `GET /nginx/certificates` every 10s for up to N minutes and report `ISSUED`, `NOT PRESENT`, or `INDETERMINATE — do not retry before <time>`. Make `--wait` the default for cert create.

---

## Finding 8: Read-modify-write with no concurrency control, and the dry-run→`--yes` protocol widens the TOCTOU window on purpose

- **Severity:** High
- **Location:** `phase-04-agent-skill-and-release.md:59-67`, section "The protocol SKILL.md encodes"; `phase-01-core-foundation.md:145`; `phase-02-host-resources.md:87`
- **Flaw:** NPM offers no `ETag`/`If-Match` and npmctl plans no alternative, yet the plan mandates a protocol whose steps 1→4 are separated by an unbounded human/agent interval, and two flows (cert-config `GET → merge → PUT`, ACL add/remove) are explicit read-modify-writes. The response objects carry `modified_on`, which the plan never uses. So the "show the dry-run, then execute" ritual produces confidence proportional to nothing: what is shown was computed from state read before the user's deliberation.
- **Failure scenario:**
  1. Agent runs `npmctl host get 12` and `npmctl host update 12 --forward-port 8081 --dry-run`; prints the intended payload.
  2. User reads it, thinks, asks a question, approves — 4 minutes elapse.
  3. Meanwhile the user's colleague, in the NPM web UI, attaches a new certificate to host 12 and enables `ssl_forced` (the UI PUTs the *full* object including `meta` with `letsencrypt_agree`).
  4. Agent runs the `--yes` command. Because P1's rule omits unset fields (`phase-01:145`), the port change alone lands and does not clobber the colleague's edit — the partial-PUT design accidentally saves this case.
  5. Now the cert variant, where the plan mandates GET-merge-PUT: agent GETs `meta` at step 1, colleague changes `dns_provider_credentials` at step 3, agent PUTs the *stale merged* `meta` at step 4. The colleague's credentials are silently reverted; renewal fails 60 days later, long after anyone connects it to this command. Identical shape for `acl update`.
  6. Nothing anywhere logs that a concurrent modification occurred.
- **Evidence:** Schema `paths/nginx/proxy-hosts/hostID/put.json` — the only parameter is `hostID`; no conditional-request header parameter exists, and `components/proxy-host-object.json` requires `modified_on` (an unused optimistic-concurrency token). Plan: `phase-04-agent-skill-and-release.md:61-64` — the four-step read/dry-run/show/execute protocol with no freshness requirement; `phase-01-core-foundation.md:145` — "Only when the caller is deliberately changing certificate config do we GET → merge → PUT"; `phase-02-host-resources.md:87` — the GET-modify-PUT ACL helper. No phase file contains the words concurrent, ETag, `If-Match`, or `modified_on` in a write context.
- **Suggested fix:** Every GET-merge-PUT flow records the `modified_on` it read and, immediately before the PUT, re-GETs and aborts with exit 3 if `modified_on` changed ("host 12 was modified at <t> by someone else; re-run to see current state"). Have `--dry-run` emit `plan-id: <sha256(resource, modified_on, payload)>` and require `--yes --confirm <plan-id>`; the confirm path re-reads state, recomputes the hash, and refuses on mismatch. That converts a two-step ritual into an actual compare-and-swap, and simultaneously fixes Finding 3's self-service problem.

---

## Finding 9: Deletes are previewed without dependents, and `--dry-run` is structurally blind to what it is about to destroy

- **Severity:** High
- **Location:** `phase-01-core-foundation.md:123-124`, section "Write gate"; `plan.md:79`
- **Flaw:** Two rows of the same table conflict in effect. `--dry-run` must send zero HTTP requests, so it cannot resolve an ID to a domain, cannot show the object being deleted, and cannot show a before/after diff — it can only echo `DELETE /api/nginx/certificates/4`. The informative preview lives in the *other* branch (no `--yes`, no `--dry-run`), which the skill protocol never instructs the agent to use. And even that preview GETs only the target, so it cannot report dependents — and for certificates NPM provides no dependent count to report.
- **Failure scenario:**
  1. Cleanup task: "remove the unused certificate." Agent: `npmctl cert list` → picks cert 4 by name similarity.
  2. `npmctl cert rm 4 --dry-run` → prints `DELETE /api/nginx/certificates/4`, exit 0. Zero information about the two proxy hosts with `certificate_id: 4` and `ssl_forced: true`.
  3. `npmctl cert rm 4 --yes` → `200 true`.
  4. NPM regenerates nginx for the affected hosts. TLS material is gone; the hosts either drop to plain HTTP or their config fails to load. `wp.dwighthanoi.com` serves a browser interstitial or nothing. Exit code was 0 both times.
  5. Recovery = re-issue via ACME, competing with any rate-limit budget already spent (Findings 1, 7).
  The access-list mirror: `acl rm` on a list with `proxy_host_count: 3` removes authentication from three hosts, and the object *does* carry the count the preview never reads.
- **Evidence:** Schema `components/certificate-object.json` — no reference/usage field at all (`additionalProperties: false`, required set is `id, created_on, modified_on, owner_user_id, provider, nice_name, domain_names, expires_on, meta`), so dependents must be discovered by scanning hosts; `components/access-list-object.json` requires `proxy_host_count`; `paths/nginx/certificates/certID/delete.json` returns bare `true` with no warning channel. Plan: `phase-01-core-foundation.md:123` — dry-run "return nil without calling `do`"; `:124` — destructive preview does a single "GET the resource"; `plan.md:79` — dry-run "sends **zero** HTTP requests (asserted)".
- **Suggested fix:** Redefine the invariant as "`--dry-run` performs no *mutating* request" and assert that instead — reads are what make a preview truthful. Before any delete, run a dependency scan (`GET` proxy/redirect/dead hosts + streams, filter on `certificate_id`/`access_list_id`) and print every dependent by domain; refuse with exit 3 when dependents exist unless `--cascade-ack` is given. Print the resolved human identity of the target (domains, nice name) in dry-run output, and use `proxy_host_count` for ACLs.

---

## Finding 10: Concurrent invocations race on token refresh and can corrupt the credential file

- **Severity:** Medium
- **Location:** `phase-01-core-foundation.md:93-100`, section "Session lifecycle"; `phase-01-core-foundation.md:84`
- **Flaw:** The session design assumes one process. Agents fan out (`npmctl host list & npmctl cert list & npmctl acl list &`, or a skill recipe running several reads in one turn). Each process independently decides the token is near expiry, calls `GET /api/tokens`, and writes the result to the same `~/.config/npmctl/credentials.json` (or keyring entry). No locking, no atomic temp-file-plus-rename, and no write-only-if-newer rule is specified.
- **Failure scenario:**
  1. Token expires in 90 seconds. Agent launches four npmctl processes concurrently.
  2. All four call `GET /api/tokens` and receive four distinct JWTs.
  3. Two processes write `credentials.json` simultaneously. With a naive `os.WriteFile`, one truncates while the other writes → a half-written JSON file on disk.
  4. Fifth invocation: unmarshal fails. Best case the code treats it as "no credentials" and re-logins from the stored password — which is *in the file it just failed to parse*. So: exit 4, "run `npmctl auth login`", in the middle of an unattended agent run. Worst case the parse error is reported as a generic error (exit 1) and the agent retries in a loop, each iteration failing identically.
  5. Variant: a stale/rotated password in the file plus "re-login on 401" (`:98`) turns every command in the fan-out into a failed login burst against the production NPM.
- **Evidence:** `phase-01-core-foundation.md:96-99` — the four-line session state machine, single-process by construction, with "refresh fails / 401 → re-login from stored password"; `:84` — "`~/.config/npmctl/credentials.json` 0600" with no atomicity or locking requirement; `:194-195` implementation steps mention "cache, refresh-before-expiry, re-login on 401" and no lock. Schema `paths/tokens/get.json` — refresh requires a still-valid bearer token, so the race window is exactly the pre-expiry window the design chooses to act in.
- **Suggested fix:** Write credentials via temp file + `os.Rename` in the same directory, guarded by an advisory lock (`flock` on a sibling `.lock`). Under the lock, re-read and keep whichever token has the later `expires`. Serialize refresh so a losing process reuses the winner's token instead of minting its own. On unmarshal failure, treat the file as absent but say so explicitly (distinct message, exit 4) rather than emitting a generic error, and never rewrite it from a partially parsed value.

---

## Secondary defects (worth fixing, not developed above)

- **Private key material written with default permissions.** `phase-02-host-resources.md:79-81` — `cert download` writes to `--output-dir` (default **cwd**) with no mode specified. `GET /nginx/certificates/{id}/download` returns the cert bundle including the private key. Default-cwd + 0644 means an agent run inside a git repo can leave a private key in the working tree and a later `git add -A` commits it. Fix: 0600, default to a dedicated directory, refuse to write into a directory containing `.git` without `--force`.
- **`skill install` contradicts itself on user edits.** `phase-04-agent-skill-and-release.md:85` says "Idempotent — re-running updates in place"; `:134` says "warn before overwriting a modified `SKILL.md`". No manifest or checksum is specified to detect modification, and there is no interactive prompt path when run by an agent. Fix: store a hash manifest at install time; on mismatch, refuse unless `--force`, and write `SKILL.md.new` beside it. Also note `--agents-md ./AGENTS.md` mutates a file in the caller's cwd — likely tracked by git — and the "no duplicate line" check is a substring match that fails the moment a user rewords the line.
- **Parity accounting is off, and the health endpoint is unmapped.** `plan.md:44` and `phase-03-admin-resources.md:103` count "44 endpoints"; the v2.15.1 schema has **44 paths but 68 operations**. A checklist "verified against the vendored schema's path list" can pass while individual methods are missing. Separately, `GET /` (`operationId: health`, tags `public`, returns `{status, setup, version}`) appears in no endpoint map — that is precisely the pre-flight reachability/`setup` check a write-gated tool should run before a mutation against a possibly half-initialized instance.
- **Exit 0 on dry-run is indistinguishable from a completed write.** `phase-01-core-foundation.md:123` returns nil for dry-run. An agent that keys on exit status will report "deleted" after a dry-run. Fix: dry-run exits with a dedicated non-zero code (or prints a machine-readable `"dry_run": true` marker in `-o json`), so no consumer can confuse simulation with execution.

---

## Unresolved questions

1. Is `Guard` allowed to issue reads (`GET`) during `--dry-run`? The current "zero HTTP requests" invariant makes dry-run output nearly useless for deletes (Finding 9); the invariant needs restating as "zero mutating requests" or the preview design needs to move.
2. What is the intended exit code and message when a write returns 2xx but `meta.nginx_online == false`? Nothing in the exit-code table (`phase-01:128-139`) covers "applied but the resulting nginx config is broken."
3. Does the user accept a persisted pre-write undo journal in P1 (Finding 4)? It is the smallest change that makes agent-driven writes against production recoverable, and it partially reverses D10.
4. Was the `cert create` payload in `phase-02:53-63` ever executed against a live v2.15.1 instance, or transcribed from the brainstorm? The schema says it cannot validate (Finding 5); confirming this determines whether P2 needs a payload re-derivation pass before implementation.
5. Which `allowed-tools` grant will actually ship in `SKILL.md`? `Bash(npmctl:*)` removes the human approval step that the entire safety model implicitly relies on (Finding 3).

Status: DONE_WITH_CONCERNS
Summary: Traced every write flow; found four Critical defects (auto-retry duplicating non-idempotent writes and LE issuance, no post-write `nginx_err` verification, an agent-self-service write gate whose stated guarantee is false, and zero pre-write state capture with `export` deferred) plus five High findings including two request payloads that contradict the v2.15.1 schema.
Concerns/Blockers: The plan's safety claim (`plan.md:21`, `phase-04:130`) is not supported by its own mechanism; Findings 1–4 should be resolved in the plan before P1 implementation starts, and Findings 5–6 require re-deriving P2 payloads from the schema rather than from the brainstorm report.
