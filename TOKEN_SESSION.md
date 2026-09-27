# V3 token/session audit

Audited 2026-09-27. This change retains the Go CLI and Chrome gateway protocol.
No live login, messages, uploads, or captures were performed. The local capture
profile was absent; retry-policy behavior below is a bounded CLI implementation
of the supplied policy fields, not a claim of a newly observed Chrome trace.

## Upstream evidence

Public source snapshots inspected:

- LINEJS [`ef6c3d9`](https://github.com/evex-dev/linejs/tree/ef6c3d9f70dd41fa51053615d47f071f58cf8db3):
  [auth refresh](https://github.com/evex-dev/linejs/blob/ef6c3d9f70dd41fa51053615d47f071f58cf8db3/packages/linejs/base/service/auth/mod.ts)
  persists a rotated refresh token, retaining the old one when absent, and uses
  issue time plus duration. [Login](https://github.com/evex-dev/linejs/blob/ef6c3d9f70dd41fa51053615d47f071f58cf8db3/packages/linejs/base/login/mod.ts)
  also saves the refresh token and server-derived deadline.
  [Request recovery](https://github.com/evex-dev/linejs/blob/ef6c3d9f70dd41fa51053615d47f071f58cf8db3/packages/linejs/base/request/mod.ts)
  recognizes MUST_REFRESH_V3_TOKEN and bounds replay. It uses a different Thrift
  endpoint, `/EXT/auth/tokenrefresh/v1`; that endpoint is not substituted for Chrome.
- Beeper [`c40908e`](https://github.com/beeper/line/tree/c40908ee46242e45e696fd116a52b97f2553f9c7):
  [Chrome refresh](https://github.com/beeper/line/blob/c40908ee46242e45e696fd116a52b97f2553f9c7/pkg/line/client.go)
  POSTs JSON `{refreshToken, retryCount}` to `/api/auth/tokenRefresh`, with Chrome
  headers/HMAC, and expects a direct TokenV3IssueResult.
  [Recovery](https://github.com/beeper/line/blob/c40908ee46242e45e696fd116a52b97f2553f9c7/pkg/connector/auth_recovery.go)
  serializes recovery and compares the failing client's token to current tokens
  before interpreting logout. [Client lifecycle](https://github.com/beeper/line/blob/c40908ee46242e45e696fd116a52b97f2553f9c7/pkg/connector/client.go)
  retains rotated tokens and distinguishes forced logout from recovery.
- Beeper's V3 struct contains `refreshApiRetryPolicy`, but its recovery does not
  consume that policy or persist issue time/duration. LINEJS also does not consume
  the policy. Neither is evidence of Chrome's exact retry schedule. The CLI's
  policy implementation is an improvement required by this task, not copied
  behavior. Beeper's password-based relogin fallback is deliberately not adopted:
  the CLI never stores passwords.

## What the CLI already did correctly

- QR and email/PIN login preserve TokenV3IssueResult in the API layer and choose
  its access token over the legacy auth token. `pkg/line/structs.go` already has
  all requested V3 fields and the Chrome decimal-string timing representation.
- Both login methods use `finishLogin`, preserving account identity, certificate,
  exported Letter Sealing keys, access/refresh tokens, and a refresh deadline.
- `/api/auth/tokenRefresh` already uses the current refresh token and Chrome
  headers/signature. Empty access-token results are rejected. A response omitting
  the refresh token retains the previous one.
- Manager proactively refreshes when its saved deadline is due, persists before
  retrying a read, recreates the client, and limits authentication recovery.
- Sends, reactions, unsends, group key registration, and uploads are single-shot
  mutations. Request sequences are persisted before applicable mutations. Only
  OBS readiness/download reads have their own encoding-pending retry loop.
- Cross-process command locks prevent concurrent refresh/save races. The watcher
  uses short command locks and a separate singleton watch lock.
- Watch checkpoints reload current session state after decoding. Existing stale
  SSE logout handling already compared access tokens before invalidating.
- No local session-age cutoff exists. Explicit server logout invalidates saved
  credentials; ordinary refresh-required errors and network failures do not.
- Storage already commits the complete serialized state: Keychain SecItemUpdate
  on macOS; encrypted file sync/rename/directory sync on Linux; DPAPI temporary
  file sync and write-through replacement on Windows. Storage errors stop work.

## Gaps found and fixed

| Previous behavior | Updated behavior |
| --- | --- |
| Deadline was receipt time + duration minus margin; ignored server issue time | Issue time + duration minus 30 seconds, or half the lifetime for tokens shorter than 60 seconds; legacy receipt-time deadline remains a fallback |
| Session discarded duration, issue time, and retry policy | Both login paths and refresh persist all requested V3 fields in the existing single commit |
| Policy existed only in the wire struct; refresh was attempted once | Transient network/408/429/5xx refresh failures get up to four attempts; provided initial/max delay, multiplier, jitter are used with validation and a 30-second delay cap; retryCount increments |
| Refresh request was not cancellable and published its new client token before persistence | Context-aware refresh returns token state without changing the original client; Manager saves before creating a client with the new token |
| Code matching could miss whitespace or confuse 119 with 1190 | JSON normalization and numeric boundaries; namespace-aware unauthorized-device and TokenAuthException handling |
| HTTP-200 auth envelopes could lose codes inside method-specific parsing | RPC boundary preserves a typed, body-redacted auth error before method parsing; non-auth capability envelopes keep existing handling |
| Runtime mutation auth failure left credentials stale | Recover credentials once, but return the mutation failure without replaying it |
| SSE auth recovery depended on a Talk probe also failing | Explicit refresh-required SSE errors refresh directly; generic 401/403 probes for logout, then refreshes even when the probe succeeds |
| SSE lifetime was only the fixed probe interval | Stream timeout is the earlier of the next probe or refresh deadline; reconnect uses the persisted access token; consecutive auth rejection after recovery stops without another rotation or invalidation |
| Stale-response protection was confined to the watcher | Manager compares login generation/current tokens before recovery or invalidation; shared-manager calls are serialized too |
| Diagnostics/tests/docs suggested Chrome sessions expire at 168 hours | Treat 168 hours as the normal access-token refresh boundary; describe refresh rejection separately from forced logout |

Gateway code 10051 alone means RESPONSE_ERROR, not necessarily authentication.
TalkException 119 / refresh-required text trigger recovery. TalkException 8 and
TokenAuthException 3 identify unauthorized-device failures; existing explicit
V3_TOKEN_CLIENT_LOGGED_OUT, REQUEST_NEED_LOGIN/10004, and invalid-sender-key
logout classifications are retained. TokenAuthException 1/2 indicate an auth
rejection without proving a forced logout. Generic 401/403 can trigger recovery
but never alone persist invalidation. A logout code cannot reliably prove which
other client, if any, displaced the session; diagnostics do not invent that cause.

## Call-path audit

| Path | Session handling |
| --- | --- |
| whoami, contacts, chat listing/selection, sender display lookup | Manager.Do |
| history, message lookup for replies/actions, blocked contacts | Manager.Do |
| peer negotiation, peer/group key reads, membership reads | Manager.Do |
| send/reply, react/cancel reaction, unsend | Manager.Mutate; persisted sequence; no replay |
| encrypted/plain file uploads, group-key registration | Manager.Mutate; no replay |
| file/media downloads, including channel/encrypted OBS token acquisition | Entire safe read flow runs inside Manager.Do |
| watch revision/profile probes | Manager.DoContext |
| SSE transport | Direct API client only for the stream; Manager.RecoverStream under command lock for auth, generation/token comparison before invalidation |
| QR/email authentication and immediate setup | Direct clients inside Manager login lifecycle; complete new state saved by finishLogin |
| auth status/check/migrate and local logout | Local storage only; do not contact LINE |

No ordinary CLI command constructs a pkg/line.Client to bypass Manager. External
users of pkg/line.Client remain responsible for their own session persistence;
that package does not automatically retry mutations or own credential storage.
Different CLI processes still fail fast with ErrBusy when a command holds the
session lock; they cannot independently refresh the same stored token. Concurrent
calls sharing one Manager wait for its mutex and then load the committed state.

## Regression coverage and limits

Synthetic tests cover complete email and QR metadata, proactive margin, stale
receipt-time deadlines overridden by issue metadata, restart before/at/after
168 hours, access/refresh-token rotation, omitted refresh tokens, code 119 read
recovery, HTTP-200 errors, refresh rejection/malformed results, transient retries,
jitter bounds, cancellation, forced logout, stale clients/streams, concurrent
refresh, persistence failure, SSE-only auth errors, and scheduled SSE rotation.
Existing messaging tests retain no-replay and encryption capability guards.

Normal token rotation should survive the seven-day access-token boundary without
manual login when refresh credentials remain valid and storage is writable.
There is no seven-day live soak test or promise against server revocation.
A rejected/missing refresh credential or genuine logout may require manual login.
If a server rotates credentials but the response is lost or local persistence
fails, the client cannot guarantee recovering that unknown/uncommitted token.

## Validation completed

On macOS with Go 1.27.1:

- `go test -race ./internal/... ./cmd/line ./pkg/line/... ./pkg/e2ee` passed.
- The final SSE recovery bound also passed `go test -race ./internal/events ./internal/cli`.
- `go vet ./internal/... ./cmd/line ./pkg/line ./pkg/e2ee ./pkg` passed.
- `staticcheck ./internal/... ./cmd/line ./pkg/line ./pkg/e2ee ./pkg` passed.
- `goimports -local github.com/kongesque/line-cli` reports no formatting changes.
- `./build.sh`, `./bin/line help`, and `git diff --check` passed.

Go/staticcheck caches and cached-module builds of the lint tools used `/tmp`
locations because the sandbox does not allow writing the normal user caches.
Linux native keyring and Windows DPAPI integration tests were not executed on
this macOS host; their storage implementation is unchanged.
