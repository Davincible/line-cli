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

The paging request shape comes from the LINE Chrome extension as reproduced by OkLine
(`okline/services/messaging.py`) and linejs (`client/features/chat/fetcher.ts`). LINE returns
the cursor message again at the top of each page; the client deduplicates by ID and treats a
page with nothing new as the start of the chat.

## Verified

- Unit tests: `go test ./internal/... ./cmd/line ./pkg/line/... ./pkg/e2ee`.
- Live, 4 October 2026: `LINE_CLI_LIVE_READ=1 LINE_CLI_LIVE_CHAT=<id> go test ./internal/messaging -run TestLiveHistoryPaging -v`
  paged a group chat in 5-message pages and matched one plain read exactly.

## Keeping up with upstream

```sh
git fetch upstream --tags
git rebase <new-tag>        # on branch davincible
go test ./internal/... ./cmd/line ./pkg/line/... ./pkg/e2ee
git push --force-with-lease origin davincible
cd ~/dotfiles && nix flake update line-cli-src && just rebuild
```
