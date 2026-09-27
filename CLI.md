# LINE CLI Guide

LINE CLI lets you use a personal LINE account from a terminal. It supports
interactive use, JSON output for scripts, Letter Sealing, file transfers,
replies, reactions, unsend, and live events.

> [!IMPORTANT]
> LINE allows one Chrome-style session. Signing in with `line` may replace an
> existing LINE Chrome extension or another Chrome-style client session.

## Install

### macOS and Linux

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/kongesque/line-cli/main/scripts/install-release.sh | sh
```

The installer selects the correct release for your computer, verifies its
checksum, installs `line` into `~/.local/bin`, and configures `PATH` for common
shells. Reopen the terminal if prompted.

On Linux, install credential storage before logging in. For Debian or Ubuntu:

```sh
sudo apt install libsecret-tools gnome-keyring
```

The Secret Service keyring must be running and unlocked in the same D-Bus
session. This is especially important on SSH and headless systems.

Homebrew is also available on macOS:

```sh
brew install kongesque/tap/line-cli
```

### Windows

Run in PowerShell:

```powershell
irm https://raw.githubusercontent.com/kongesque/line-cli/main/scripts/install-release.ps1 | iex
```

The installer places `line.exe` in `%LOCALAPPDATA%\line-cli\bin` and adds that
directory to your user `PATH`. Reopen PowerShell if `line` is not immediately
available.

Release binaries are currently unsigned. You can also download and inspect them
from [GitHub Releases](https://github.com/kongesque/line-cli/releases/latest).

Verify the installation:

```sh
line version
line help
```

## Quick start

Bare `line login` selects QR login. Scan the terminal QR with LINE on your phone,
approve the login, and enter the displayed PIN if prompted. QR login is
experimental. Email/password login remains available with
`line login --email ADDRESS`.

```sh
line login
line whoami
line chats
line messages "Family group"
line send "Alice" --text "Hello!"
```

Email login requires an email address and password configured on your LINE
account. Your password is used only during login and is never saved. If your
session expires, sign in again with QR or the explicit email fallback.

## Login

```sh
line login                         # interactive QR login (experimental)
line login --qr                    # explicitly select QR
line login --email you@example.com # email/password fallback
line login --qr-url                # sensitive URL instead of terminal QR
line login --force                 # skip saved-session replacement question
line login --help
```

| Option | Behavior |
| --- | --- |
| `--qr` | Select QR login explicitly; this is also the default. |
| `--email ADDRESS` | Use hidden password input and the existing phone-verification flow. |
| `--qr-url` | Print the one-time QR value to stderr for a trusted local QR tool. |
| `--force` | Skip only the saved-session replacement question. |
| `--headless` | Linux only: select headless storage, independently of the login method. |

`--email` cannot be combined with `--qr` or `--qr-url`. There is no password flag
or password environment variable. QR login, including `--qr-url`, requires an
interactive input terminal; it is not an unattended login API. Redirecting
stdout is allowed. Progress, the QR, PINs, and warnings use stderr; the final
success message uses stdout.

### QR status and current limits

The CLI renders a compact QR with a quiet border. Scan it using the QR scanner
in LINE on your phone. When PIN verification is available, enter the displayed
code in LINE exactly as shown, including any leading zeros. Wait for
`Session saved securely` before relying on the new session: phone approval
alone does not mean profile validation, key export, or local saving succeeded.

**QR login is experimental.** Use `--email ADDRESS` as the email/password
fallback if needed. A saved QR certificate is reused only when known to
come from QR login; legacy and email certificates are not reused. PIN fallback
for rejected saved certificates is not yet supported, and unknown certificate
errors still stop login.
QR accounts with Letter Sealing disabled are unsupported; missing encryption
data never silently disables encryption.

A certificate-verification error reports that stage separately. When a structured
server error is available, its `Diagnostic: verifyCertificate` suffix contains
only numeric HTTP, gateway, status, and optional service codes. That diagnostic
can help identify the missing protocol evidence; it does not expose the QR value,
PIN, certificate, tokens, or server response text.

The displayed expiry time is approximate and appears only when stderr supports
in-place terminal updates. Reaching zero does not replace the QR. Confirmed
expiry creates a fresh code, up to three codes per login. Network failures and
PIN timeouts do not start another attempt. Plain terminals and redirected stderr
receive state changes without a per-second countdown. Expired codes are cleared
where possible or explicitly marked as expired before their replacement.

If the terminal is too narrow, login stops with the required width and a
`--qr-url` suggestion; it never prints a clipped QR or automatically exposes its
value. `NO_COLOR` disables explicit colors. If the code is unreadable with your
terminal theme or assistive setup, use a trusted local tool with `--qr-url`.
Do not send that value to an online QR generator, share it, or save it: it is
sensitive and remains in terminal scrollback. QR images are not saved. Unset
`QRCODE_DEBUG` before normal QR login because encoder debug mode can write data.

### Saved sessions and cancellation

Before contacting LINE, login checks storage and asks before replacing a usable
saved session. This prompt describes local state; it does not claim that the
remote session is valid. Answering no keeps the session and exits successfully.
`--force` skips only this question. It does not bypass storage checks, concurrent
session-change detection, headless protection consent, or protocol failures.
The Chrome-style session warning is still shown.

Ctrl-C cancels QR polling and local prompts. SIGINT exits with 130; SIGTERM exits
with 143, including when output is redirected. Password echo is restored before
exit. The legacy email authentication HTTP requests retain their existing
timeouts; local email prompts and shared completion work are cancellable.

Cancellation before local saving preserves the old saved session. If the final
login request may have reached LINE, the previous Chrome-style session may
already have been replaced even when the new session could not be saved. The
final login request is never automatically retried. Read the reported outcome
before retrying; cancellation cannot undo remote approval. A successful local
save is still reported if a signal arrives afterward. An uncertain-save error
means storage may already have changed: use `line auth status --check` for local
diagnostics and do not overwrite it with an older copy or blindly repeat login.

### SSH login

Use an interactive SSH terminal; no remote browser is needed for terminal QR
display. For example:

```sh
ssh -t user@host
line login --qr
# Email/password fallback:
line login --email you@example.com
```

Choose one login method. SSH does not remove the storage requirement. On a
supported Linux host without an unlocked keyring, use
`line login --headless --email you@example.com` for the current fallback, or
`line login --headless` for experimental QR login. See [headless Linux](#headless-linux).

## Interactive use

Most everyday commands guide you when information is missing:

```sh
line messages
line send
line download
line react
line unsend
```

Chat and message choosers accept a displayed number or a name search. Use `n`
and `p` to change pages, `q` to cancel, and Ctrl-C to stop at any time.

Prompts require an interactive terminal. They are disabled with `--json` and
`--stdin`, so scripts must provide every required argument.

## Finding chats and contacts

```sh
line contacts
line contacts --search "Alice"
line chats
line chats --search "Family"
line chats --all --limit 50
line chats --show-ids
```

`contacts` and `chats` show 20 results by default. Use `--limit 0` for all
results. `chats --all` includes inactive conversations.

Commands that accept `CHAT` support either:

- A unique exact name, matched without case sensitivity.
- A full contact, room, or group ID.

Quote names containing spaces. Partial and duplicate names are rejected; use
`line chats --search NAME --show-ids` to find the correct full ID. IDs are the
safer choice for long-lived scripts because display names can change.

## Reading messages

```sh
line messages "Alice"
line messages "Alice" --limit 50
line messages "Alice" --show-ids
line messages CHAT_ID --json
```

`messages` fetches 1–100 recent messages. Human output is displayed oldest to
newest. `--show-ids` reveals message IDs needed for replies and message actions.

Reading history does not mark messages as read or save message text locally.
Older encrypted messages may be unavailable if LINE no longer provides their
original device keys.

## Sending messages and files

Send text directly:

```sh
line send "Alice" --text "Hello!"
```

Read multiline text from standard input:

```sh
line send "Alice" --stdin < message.txt
```

Reply to a message or send a file:

```sh
line send "Alice" --text "Sounds good" --reply-to MESSAGE_ID
line send "Family group" --file ./report.pdf
line send "Family group" --file ./report.pdf --reply-to MESSAGE_ID
```

Choose only one of `--text`, `--stdin`, or `--file`. Text must be nonempty and
within LINE's 10,000 UTF-16-unit limit. Generic file attachments are limited to
20 MiB. Images, video, and audio sent with `--file` appear as ordinary files.

LINE CLI uses Letter Sealing when the conversation supports it. Missing keys,
incomplete group membership, or transport failures stop an encrypted send; they
do not silently downgrade it to plaintext. An explicit group send may register
a new group key when every current member is known.

Use JSON output to inspect the result:

```sh
line send "Alice" --text "Hello" --json
```

The result includes `encrypted`, `group_key_registered`, the server message ID,
and the request sequence.

> [!CAUTION]
> A send or other remote mutation is attempted once. If the response is lost,
> inspect LINE before retrying because the action may already have succeeded.

## Downloads, reactions, and unsend

Use a message ID from `line messages CHAT --show-ids` or `--json`:

```sh
line download "Alice" --message MESSAGE_ID --output ./received.pdf
line react "Alice" --message MESSAGE_ID --reaction love
line react "Alice" --message MESSAGE_ID --remove
line unsend "Alice" --message MY_MESSAGE_ID
```

Available reactions are `like`, `love`, `laugh`, `surprise`, `sad`, and `angry`.
`unsend` works only for your own messages and remains subject to LINE's server
rules.

These commands search the latest 100 messages in the selected chat. Downloads
never overwrite an existing destination and verify encrypted data before saving.
Remote metadata never controls the output path.

## Live events

`watch` writes newline-delimited JSON to stdout:

```sh
line watch
line watch --timeout 30s
line watch --limit 10
line watch --from-now
```

The first run starts at LINE's current revision. Later runs resume from the saved
revision. `--from-now` discards that checkpoint. Status and reconnect messages go
to stderr.

Event types are:

- `message`: a sent or received message.
- `operation`: another LINE notification.
- `resync_required`: LINE reported a history gap; refresh chats and messages.

Consumers should deduplicate events by `revision`. A crash between writing an
event and saving its checkpoint can repeat the last event. Only one watcher can
run at a time.

## JSON and scripting

Add `--json` to commands that support structured output:

```sh
line whoami --json
line contacts --json
line chats --search "Family" --json
line messages CHAT_ID --json
line send CHAT_ID --stdin --json < message.txt
```

JSON goes to stdout and diagnostics go to stderr. Successful commands and help
return exit status 0; ordinary errors return 1. Storage errors use the
[dedicated codes below](#storage-exit-codes); SIGINT/SIGTERM use 130/143.
Empty result lists are `[]`.

Important JSON fields include:

| Command | Result |
| --- | --- |
| `whoami` | Account profile and ID |
| `contacts` | Contact names and IDs |
| `chats` | Chat ID, type, unread count, and optional activity time |
| `messages` | Message ID, sender, timestamp, content, encryption, and status |
| `send` | Message ID, chat ID, encryption, group-key registration, and sequence |
| `download` | Output path, byte count, and message ID |
| `react`, `unsend` | Action, chat ID, message ID, and sequence |
| `watch` | Newline-delimited event stream |

If some history entries cannot be decrypted, `messages --json` still writes the
complete array with per-message failure statuses, then exits with status 1. It
never prints encrypted chunks or raw server response bodies.

## Sessions and credential storage

LINE CLI stores one account per operating-system user:

| Platform | Storage |
| --- | --- |
| macOS | Keychain |
| Linux | AES-GCM encrypted session file with its key in Secret Service |
| Windows | Current-user DPAPI encrypted session file |

Your password is used only during login and is never saved. Session updates use
a process lock, so another command may briefly report that the session is busy.
Retry it after the current update finishes.

### Headless Linux

On a supported Linux host without an unlocked Secret Service keyring, create a
new session with:

```sh
line login --headless --email you@example.com
line auth status
line auth status --check --json
```

This example uses the email fallback. `line login --headless` selects experimental
QR login with the same storage protection. Both require an interactive terminal
for enrollment; `--headless` does not make login unattended.

The CLI asks you to accept **Host key; no TPM** protection before contacting LINE.
`--force` does not skip this consent. This mode protects the session from other
unprivileged users, but not from root, malware running as your Unix account, or
someone with a complete copy of the disk. It does not claim TPM protection.
Cancelling enrollment before saving leaves no new saved LINE session.

After enrollment, ordinary commands need no storage flag. Signing in again keeps
the selected backend. `login --headless` never converts or overwrites an existing
native session.

The trusted `/usr/bin/systemd-creds` helper and its user credential broker must
be available. Use the same Unix account and config directory for interactive
commands, SSH, cron, and systemd jobs. Versions 256–259 are accepted; Debian
13/systemd 257 and Ubuntu 26.04/systemd 259 have disposable-VM validation. Other
accepted versions still need a successful local preflight. Older and unreviewed
newer versions are rejected. UniPi and physical TPM behavior have not been
verified.

### Check or migrate storage

Status checks are local and never contact LINE:

```sh
line auth status
line auth status --check --json
```

`auth status` does not refresh tokens or prove that the LINE session is still
valid. It reports inaccessible storage as unreadable, not logged out. `--check`
also tests write readiness where that can be done without an unlock prompt.
Native Linux Secret Service and macOS Keychain therefore report
`interactive_check_required`; their full write checks run during interactive
login. Headless reboot access is reported as `expected_not_verified` until you
test it on your own host.

To move an accessible native Linux session to headless storage:

```sh
line auth migrate --storage=headless
```

Migration requires a terminal and the same explicit host-only acceptance. Stop
watchers, automation, and older CLI versions first. Migration preserves tokens,
E2EE keys, request sequence, and the watch checkpoint without contacting LINE or
requesting your password.

If migration is interrupted, run the same command again. Do not delete the
`migration.pending` file or replace `session.enc` manually. `auth status` reports
`migration_pending` until cleanup succeeds. See the
[headless storage internals](internal/session/HEADLESS.md) for the transaction,
envelope, and recovery design.

### Run unattended

For unattended use, enroll interactively as a dedicated unprivileged account,
then verify `auth status --check` under that same UID and environment after a
reboot. Keep `HOME` and `XDG_CONFIG_HOME` fixed. The operator owns service and
cron configuration; the CLI does not install either.

#### Headless user service

Save this as `~/.config/systemd/user/line-watch.service` for the enrolled account.
Install the binary at the path shown, or adjust the executable path. `%h` is
the account's home directory; use the same config directory used during login.
Do not add `User=` to a user-manager service.

```ini
[Unit]
Description=LINE event watcher
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Environment=HOME=%h
Environment=XDG_CONFIG_HOME=%h/.config
UMask=0077
NoNewPrivileges=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
LockPersonality=true
MemoryDenyWriteExecute=true
ExecStart=/usr/local/bin/line watch
Restart=on-failure
RestartSec=15s
RestartPreventExitStatus=65 69 74 78

[Install]
WantedBy=default.target
```

The hardening settings above are the working combination reported on Debian 13,
systemd 257.13, ARM64 in [issue #3](https://github.com/kongesque/line-cli/issues/3).
The complete example adds restart limits; validate it on the deployment host
before enabling it. It has not been certified across
all kernels or systemd releases. `AF_UNIX` is needed for the credential broker;
`AF_INET` and `AF_INET6` permit LINE connections and DNS.

After saving the unit, run as the enrolled account:

```sh
systemctl --user daemon-reload
systemctl --user start line-watch.service
systemctl --user status line-watch.service
# Enable after checking that storage and the watcher both start successfully:
systemctl --user enable line-watch.service
```

For startup without an interactive login, an administrator may need to enable
lingering for this account. For a system-manager service instead, add
`User=linebot`, use explicit `/home/linebot` paths for both environment variables,
and change `WantedBy=` to `multi-user.target`.

#### Sandbox compatibility and diagnosis

The following settings failed in the issue #3 user-service environment:

| Setting | Reported result |
| --- | --- |
| `PrivateTmp=true` | Headless helper unavailable, exit 69 |
| `ProtectSystem=strict` | Headless helper unavailable, exit 69 |
| `ProtectHome=read-only` | Nonzero exit, including filesystem write failure |

Leave these settings and `PrivateUsers=` out of the baseline unit. Adding
`ReadWritePaths=%h/.config/line-cli` alone did not resolve the reported failures.
This is a compatibility limit for the reported user-service configuration, not a
claim that these directives fail for every system-manager service.

Systemd filesystem sandboxing in user services can involve a user namespace.
With a per-user manager, `PrivateUsers=true` omits the host root UID mapping
([systemd 257 documentation](https://github.com/systemd/systemd/blob/v257/man/systemd.exec.xml)).
This can make the root-owned helper appear owned by an unmapped UID. The CLI
must reject a helper whose root ownership and safe permissions it cannot verify.
It does not relax trust checks or change storage providers to work around this.
Read-only mounts can also block locks, path registration, refreshed tokens,
request sequences, and watcher checkpoints. Both `$XDG_CONFIG_HOME/line-cli` and
`$HOME/.config/line-cli` need write access, even when they are different paths.

`auth status --check --json` reports `headless_helper_untrusted` (exit 69) when
the helper or its parent directories fail ownership/permission checks. The human
error names the sandbox settings to inspect. This reason can also indicate a
real installation permission problem; it does not prove sandboxing caused it.
`headless_unavailable` remains the reason for discovery, unsupported-version,
and root-execution failures, with a stage-specific human diagnostic.
`storage_unavailable` can indicate a credential broker or unsealing failure;
helper stderr is never exposed.

Compare `line auth status --check --json` interactively and inside a transient
unit using the same account, environment, and hardening settings:

```sh
systemd-run --user --wait --collect \
  --property=Environment=HOME="$HOME" \
  --property=Environment=XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}" \
  --property=UMask=0077 \
  --property=NoNewPrivileges=true \
  --property='RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6' \
  --property=LockPersonality=true \
  --property=MemoryDenyWriteExecute=true \
  /usr/local/bin/line auth status --check --json
```

This check is local and does not contact LINE. Test added sandbox directives one
at a time. Restore the working service environment instead of deleting the
session or re-enrolling to fix a sandbox-only failure.

Treat watcher output as private message data and restrict its journal or output
files. If the service stops, run the local check as the service user before
restarting it. Exit 69 from the watcher suppresses automatic restarts. After
repairing storage, use `systemctl --user reset-failed line-watch.service` and start it
again. This restart policy applies only to the watcher; it does not authorize
retrying sends or other remote mutations. Cron jobs should use the same account
and paths, with `umask 077`.

### Linux files and upgrades

Login checks native storage before local login prompts and rechecks session and
storage identity before contacting LINE. The check saves, reads, replaces, and
removes a separate temporary credential item or encrypted file. It preserves the active session
and reports cleanup failures. Existing unreadable or corrupt storage blocks
login; restore access first, or explicitly log out to remove the local session.
A successful check cannot guarantee a later save if storage becomes unavailable.
If another login or logout changes the session during local input, login
stops and asks you to start again.

On Linux, the session file and both process locks share the directory
`$XDG_CONFIG_HOME/line-cli` (normally `~/.config/line-cli`). Changing
`XDG_CACHE_HOME` does not create a separate lock. Keep the application directory
private (`0700`) and its files private (`0600`); unsafe ownership, file types,
symlinks at the application directory or files, and hard-linked files are
rejected. Existing parent directories are never automatically chmodded.

Linux also records observed session paths in `native-paths.json` under
`$HOME/.config/line-cli`. This prevents cleanup in one config directory from
deleting a key still needed by another known directory. If older releases used
custom `XDG_CONFIG_HOME` locations, run `line auth status` once with each old
location before migration or logout so the CLI can register it. Never delete the
path record or lock files as a cleanup shortcut.

Before upgrading to this lock layout, stop old CLI commands and watchers.
Concurrent old and new binaries are unsupported. Keep lock files in place,
including after logout. Multiple config directories are not a supported
multi-account setup: native Linux storage uses one wrapping-key identity per
Secret Service keyring.

An uncertain-durability error means the file may already have changed. Do not
restore an older copy over it; repeat the same local operation. Logout uses a
private recovery receipt and can be repeated to finish interrupted cleanup.

### Storage exit codes

Storage failures have dedicated executable exit codes:

| Code | Meaning |
| --- | --- |
| 65 | Invalid format, missing key, failed authentication, or protection mismatch |
| 69 | Storage/helper unavailable or timed out |
| 74 | Uncertain durability or failed probe cleanup |
| 75 | Local contention, changed storage during login, or cancelled helper |
| 78 | Configuration, consent, migration, repair, or interactive-check requirement |

Signals take precedence over storage error codes: SIGINT exits 130 and SIGTERM
exits 143. Other CLI and network errors use status 1. `auth status --json` still
writes its status object when storage is unavailable, then returns the matching
nonzero status.

### Session expiration

LINE controls the lifetime of Chrome-style sessions, which can expire after
about 168 hours (7 days). The CLI does not impose a seven-day local expiration:
it continues using accepted credentials and uses LINE's token refresh schedule
when provided. A refresh boundary is not itself a terminal session expiration.

When LINE returns a terminal authentication signal such as `REQUEST_NEED_LOGIN`
or `V3_TOKEN_CLIENT_LOGGED_OUT`, the CLI stops using the session and asks you to
run `line login`. These signals do not uniquely distinguish expiration from
replacement by another Chrome client, so the diagnostic says the session
"expired or was invalidated". Failed authentication during token refresh also
provides expiration context and the re-login command, without exposing the
server response. Network failures remain connection errors.

Read-only requests can recover authentication with one refresh and retry.
Remote mutations are never replayed automatically. Successful login replaces the
invalidated session and restores authenticated commands. `auth status --check`
only checks local storage access; it does not verify LINE session validity.

### Log out

```sh
line logout
```

`logout` removes the local saved session. It does not revoke the session on
LINE's servers.

## Command help

Use the built-in help for the authoritative option list:

```sh
line help
line login --help
line chats --help
line messages --help
line send --help
line watch --help
```

## Build from source

Source builds require Git and Go 1.26 or newer. macOS also requires Xcode Command
Line Tools and CGO for Keychain support.

On macOS or Linux:

```sh
git clone https://github.com/kongesque/line-cli.git
cd line-cli
./install.sh
```

On Windows PowerShell:

```powershell
git clone https://github.com/kongesque/line-cli.git
Set-Location line-cli
$env:CGO_ENABLED = "0"
go build -trimpath -o .\bin\line.exe ./cmd/line
.\bin\line.exe help
```

Contributor checks:

```sh
go test -race ./internal/... ./cmd/line ./pkg/line/... ./pkg/e2ee
go vet ./internal/... ./cmd/line ./pkg/line ./pkg/e2ee ./pkg
go build -trimpath -o bin/line ./cmd/line
```

Ordinary tests use fake APIs and credentials. Live tests require explicit
authorization and are disabled by default.

Cancellation tests use synthetic subprocesses. On macOS/Linux, the password
terminal test uses Python 3's standard-library PTY support and skips if Python
is unavailable. It checks Ctrl-C/SIGINT/SIGTERM, hidden input, and terminal
restoration without accessing LINE or the credential store.

Storage regressions cover failed writes and cleanup, legacy ciphertext,
preflight before authentication, and Linux process-lock contention. Native
Secret Service integration runs only in an explicitly enabled disposable D-Bus
session; see the CI workflow for its isolated keyring setup. Windows CI runs
DPAPI roundtrip and preflight tests against temporary files.

## Current limitations

- One saved LINE account per OS user.
- QR login is experimental. QR login with Letter Sealing disabled is unsupported.
- Recent history only, with at most 100 messages per read.
- Generic files only; no stickers or specialized media sending.
- Reading messages does not mark them as read.
- Release binaries are not signed or notarized.
- LINE protocol changes may affect compatibility.

LINE CLI is an independent project derived from `beeper/line`; it is not an
official LINE product and does not require Beeper or a Matrix homeserver.
