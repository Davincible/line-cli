# Davincible fork of line-cli

Branch `davincible`, based on upstream tag `v0.4.0`. Built into `line` on David's machines by
`~/dotfiles/packages/line-cli` through the `line-cli-src` flake input.

## What the fork changes

| Change | Why |
| --- | --- |
| `messages --limit` goes up to 10,000. Past 100 it pages back with `getPreviousMessagesV2WithRequest` | Upstream only calls `getRecentMessagesV2`, so anything older than the latest 100 messages was unreachable |
| `messages --since` (`YYYY-MM-DD`, RFC 3339, `72h`, `30d`) | Bound a read by time instead of guessing a count |
| `messages --json` adds `from_name` | Upstream JSON gave only the sender's MID, so every script needed a second contacts call |
| `chats --json` is the table as data: `name`, `updated_at`, newest first, active chats unless `--all` | Upstream JSON was an unordered list of IDs, including inactive chats, with no names |
| `download`, `react` and `unsend` find a message within the latest 2,000, not 100 | Same paging |
| Reads retry twice (1 s, then 4 s) on a timeout, 408, 429, 5xx or dropped connection | Upstream reported any blip as `LINE request failed`. Mutations are still never replayed |
| Commands queue for the session lock (up to 60 s, `LINE_CLI_LOCK_WAIT`) and say so, instead of failing with "session is busy" | Two agents reading at once, or a read during the watcher's short lock, used to error |
| `line watch --log DIR` is the **hub**: events go to daily files (dir 0700, files 0600, `--retain`, default 14 days) plus a `hub.json` heartbeat | LINE allows one watcher and one resume position per account, so only one listener could ever run |
| `line events [--follow] [--chat ID] [--from-revision N]` reads the hub's files; `line events --status` checks the heartbeat | Any number of readers, no session and no lock; deduplicates the one event a hub crash can replay; warns once when the hub stops |
| `line login` refuses on a machine other than `LINE_CLI_SESSION_HOST` unless `--take-over` | LINE has one Chrome-style session per account; a login anywhere else silently signs the owner out and kills its hub |
| `LINE_CLI_DEBUG=1` adds the failure class (HTTP status and LINE code, or the Go error) to `LINE request failed` | Response bodies stay out, as upstream intended |

The paging request shape comes from the LINE Chrome extension as reproduced by OkLine
(`okline/services/messaging.py`) and linejs (`client/features/chat/fetcher.ts`). LINE returns
the cursor message again at the top of each page; the client deduplicates by ID and treats a
short page as the start of the chat. A full page that adds nothing means LINE ignored the
cursor, and the read fails rather than returning a silently short result. `--since` without
`--limit` reads up to 10,000 messages instead of the default 20.

## Verified

- Unit tests: `go test ./internal/... ./cmd/line ./pkg/line/... ./pkg/e2ee`.
- Live, 4 October 2026: `LINE_CLI_LIVE_READ=1 LINE_CLI_LIVE_CHAT=<id> go test ./internal/messaging -run TestLiveHistoryPaging -v`
  paged a group chat in 5-message pages and matched one plain read exactly; a read past the start
  of the chat ended cleanly, and a cursor at the first message returned only that message.
- Reviewed independently on 4 October for loops, wrong results and API hammering. Its three
  defects (`--since` capped at 20, a stuck cursor read as the start of the chat, a panic on an
  all-null first page) are fixed and each has a test.

## Concurrency model

- **One session, one machine.** The session lives on `LINE_CLI_SESSION_HOST` (sekura). Other
  machines and cloud sessions never log in; they ask a session on sekura.
- **One watcher, the hub.** On sekura the `line-hub` launchd agent runs
  `line watch --log default`. Nothing else runs `line watch`.
- **Many readers.** Listeners use `line events --follow`; they cannot collide.
- **Commands queue.** `messages`, `chats`, `send` and the rest wait their turn for the session
  lock, so parallel callers succeed one after another rather than failing.

Verified live 4 October 2026 with the hub running: six simultaneous `line messages` calls all
succeeded within 6 s (upstream fails five of them), and three `line events --follow` readers ran
against one hub.

## How the one watcher is protected

LINE gives an account one event stream and one resume position, so a second consumer does not
just duplicate events, it steals them from the hub's log. Every path to that is closed:

| Risk | Guard |
| --- | --- |
| Two watchers at once | `watch.lock` (flock). The second fails with "another line watch is already running" |
| The lock file deleted under a running hub (it lived in `~/Library/Caches`, which macOS may purge) | Locks now live in `~/Library/Application Support/line-cli`. The hub re-checks every 15 s that the file at the lock path is still the one it holds, and stops if not; launchd restarts it on a fresh lock. Verified live 4 Oct by deleting the file: detected, restarted in seconds |
| A plain `line watch`, or a second hub logging elsewhere, run while the hub is down | Refused once a hub has run on the machine (`hub.json` exists); `--direct` overrides. Verified live |
| `line logout` under a running hub | Refused unless `--force` |
| `line login` on another machine or a cloud session | Refused off `LINE_CLI_SESSION_HOST` unless `--take-over` |
| The session syncing to another Mac | Not possible: the item is in the file-based `login.keychain-db`, which iCloud Keychain never syncs (checked 4 Oct) |
| The hub dying unnoticed | `hub.json` heartbeat; `line events` warns once when it goes stale; a hub that stops on an error raises a macOS notification naming the reason, never message content (`LINE_CLI_NO_NOTIFY=1` disables) |
| A crash between writing an event and checkpointing | The event is replayed once on restart; readers deduplicate by revision |

## Keeping up with upstream

```sh
git fetch upstream --tags
git rebase <new-tag>        # on branch davincible
go test ./internal/... ./cmd/line ./pkg/line/... ./pkg/e2ee
git push --force-with-lease origin davincible
cd ~/dotfiles && nix flake update line-cli-src && just rebuild
```
