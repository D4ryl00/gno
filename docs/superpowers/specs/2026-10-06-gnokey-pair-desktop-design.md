# gnokey-pair desktop: a menu bar app that starts at login

Design doc. Status: accepted. Builds on `2026-10-05-gnokey-pair-design.md`
(the CLI, its protocol and its review), which it does not change.

## Context

gnokey-pair is a terminal program. To hand a transaction from Gnokey Mobile to
the computer, the user opens a terminal, runs `gnokey-pair -remote <rpc>`,
scans the code, reviews in the terminal, types `y`, then the gnokey password.
Every request starts with that terminal session, and the code works once.

The request: a desktop app for macOS and Linux that starts at login (a
setting turns that off), so the user never has to run the CLI. Every request
still starts from a fresh code and gets its own review and approval: the
phone never keeps a standing way to reach the computer.

Two ways to use the relay, chosen in Settings:

- **When I ask** (the default): the app holds no connection while idle. The
  user clicks "Receive from phone…" and the app connects and shows a code, as
  the CLI does.
- **When gnokey-pair starts**: the app connects at launch and keeps a code
  ready, so the phone can scan it whenever the user opens it, without a click
  on the computer first.

Unchanged: **`gnokey` is not modified** and stays the only program that signs;
the CLI keeps working. The app needs no protocol change of its own: signing
offline and signing by hand, which it offers, are additions to the base design
(optional fields), made there for the CLI and gnokey-mobile too.

## Decision

```
 phone                               relay                    computer (app, from login)
 ─────                               ─────                    ──────────────────────────
                                                              menu bar icon, idle
                                                              "Receive from phone…"
                                                              (or at launch, automatic)
                                    ◀── allocate ──           code + QR
 scan / type code ── claim ──▶
 ◀──────────────── SPAKE2, check words (as the CLI) ─────────▶
 request ──────────────────────────────────────────────────▶  notification + review window
                                                              [Decline]  [Sign…]
                                                              password / Ledger → gnokey sign
 ◀──────────────────────────────── result ───────────────────  dry-run, broadcast
                                                              idle again (manual)
                                                              or a new code (automatic)
```

- One Go program, `gnokey-pair`'s desktop app, with a [Fyne](https://fyne.io)
  UI: a menu bar icon, and windows for the code, the review, signing and
  settings. It runs the same pipeline as the CLI, in process.
- `gnokey sign` runs in a pseudo-terminal the app owns. The app answers its
  password prompt from a password field, or tells the user to confirm on the
  Ledger.
- Start at login: `SMAppService` on macOS, an XDG autostart entry on Linux.
  On by default; Settings → General turns it off.
- The relay connection is made on demand or at launch, per a setting.

## Scope

In:

- macOS 13 and later (Apple silicon and Intel), Linux desktops with X11 or
  Wayland (GNOME, KDE Plasma, XFCE, Cinnamon).
- Everything the CLI does: `sendtx` and `signtx`, the review, the checks
  before and after signing (base design, Pipeline), signing offline on a
  local network and signing by hand (base design, sections of those names).
- Start at login, the two connection modes, settings, notifications.

Out:

- Windows. Fyne runs there, but autostart, the pseudo-terminal and packaging
  differ; a later design.
- Remembering a phone. Every request starts from a code (Alternatives).
- Storing the gnokey password, or any key. The app never keeps a password
  past the `gnokey sign` it was typed for.
- Bundling `gnokey`. The app runs the gnokey the user installed and shows
  which one (as the CLI prints it).
- Auto-update. Releases ship as packages; updates go through them.
- Protocol or gnokey-mobile changes beyond the base design's.

## The app

### Running

One process per user session, started at login with `--background` (no
window) or from the launcher (opens the menu). A second launch hands over to
the running one through a Unix socket in the user's runtime directory
(`$TMPDIR` on macOS, `$XDG_RUNTIME_DIR` on Linux) and exits.

The menu bar icon shows the state: idle (no connection), a code ready, a
request waiting (badge), and offline (the relay is unreachable; the app
retries with backoff and says so in the menu).

```
 When I ask (idle)                     When gnokey-pair starts
 ┌───────────────────────────────┐     ┌──────────────────────────────────┐
 │ gnokey-pair                   │     │ gnokey-pair                      │
 │ Not connected                 │     │ ● Ready: 7-guitarist-revenge     │
 │ ───────────────────────────── │     │ ──────────────────────────────── │
 │ Receive from phone…           │     │ Show the code…                   │
 │ Recent ▸                      │     │ Disconnect                       │
 │ ───────────────────────────── │     │ Recent ▸                         │
 │ Settings…                     │     │ ──────────────────────────────── │
 │ Quit gnokey-pair              │     │ Settings…                        │
 └───────────────────────────────┘     │ Quit gnokey-pair                 │
                                       └──────────────────────────────────┘
```

"Recent" lists the last ten results: what, when, block. Closing a window
never quits. Quit closes any channel; a phone waiting on it sees the channel
lost and falls back to watching the chain, as with the CLI.

**Linux without a tray.** GNOME shows tray icons only with the AppIndicator
extension (Ubuntu ships it, Fedora does not). The app checks for a
`StatusNotifierWatcher` on the session bus. Without one it still runs; the
launcher entry opens a small main window with the menu's items, and a request
raises a notification that opens the review. Settings says which mode is in
use.

### Connecting to the relay

Settings → General → **Connect to the relay**:

- **When I ask** (default). Idle means no connection. "Receive from phone…"
  connects, allocates a code and opens the code window. After the request
  (or Cancel, or the timeout) the app disconnects.
- **When gnokey-pair starts**. At launch the app connects and allocates a
  code, without opening a window. The menu shows the code; "Show the code…"
  opens the window with the QR. When a request is done, the app allocates the
  next code. "Disconnect" in the menu goes idle until "Connect" or the next
  launch; switching the setting applies at once.

A code is the CLI's: one-shot, two words, one guess for anyone who does not
have it. Keeping one ready changes two things, and the app handles both:

- **A wrong guess burns the code.** The CLI exits then. In automatic mode the
  app does **not** allocate a new code on its own: that would give an
  attacker unlimited guesses, one per new code. It shows a notification
  ("Someone used a wrong code; the code is no longer valid") and the menu
  shows "Get a new code". A new code comes only from the user.
- **Nobody may be watching the computer** when a request arrives. The review
  window comes with a notification, shows the check words first and large,
  and [Sign…] waits as below. A request the user did not send is declined,
  and the user knows someone scanned the code.

How long a code lasts:

- In manual mode, as in the CLI: the code window counts down `-timeout`
  (10 minutes by default, in Settings), then the app disconnects.
- In automatic mode, as long as the app is connected. The relay keeps a
  nameplate while its client stays subscribed, so a code can wait for hours.
- After sleep or a network change, the `wormhole` package reconnects. The
  relay keeps a disconnected client's claim for up to 11 minutes; after a
  longer sleep the code is gone, and the app allocates a new one (this is not
  a wrong guess, so it is safe to do on its own) and updates the menu.

### First launch

A welcome window: what the app does, where it found `gnokey` (path and
version), **Start gnokey-pair when you log in** (checked) and **Connect to the
relay**: when I ask / when gnokey-pair starts. Continue applies them.

If `gnokey` is not found it says so and asks for its location. A GUI app does
not get the shell's `PATH` (launchd gives `/usr/bin:/bin:/usr/sbin:/sbin`,
autostart on Linux the session's), so the app looks in `PATH`,
`$(go env GOPATH)/bin`, `~/go/bin`, `/opt/homebrew/bin`, `/usr/local/bin` and
`/usr/bin`, then asks. The path is saved; Settings shows it with the version.

### The code window

The QR and the code, as the CLI prints them:

```
 ┌──────────────────────────────────────────────┐
 │ Receive from phone                           │
 │                                              │
 │   ▄▄▄▄▄▄▄ ▄ ▄▄ ▄▄▄▄▄▄▄                      │
 │   █ ▄▄▄ █ ▀█▄▀ █ ▄▄▄ █    7-guitarist-revenge│
 │   █ ███ █ ▄▀▄█ █ ███ █                      │
 │   ▀▀▀▀▀▀▀ ▀ ▀ ▀▀▀▀▀▀▀    In Gnokey Mobile:  │
 │                           Send to my computer│
 │ Waiting for the phone… (9:41 left)  [Cancel] │
 └──────────────────────────────────────────────┘
```

In automatic mode there is no countdown, and [Cancel] is [Close] (the code
stays ready). After the handshake the window shows the check words, large:
"Your phone shows the same two words. If they differ, decline." Then the
request opens the review.

### Review

The request opens the review window and, if the app is in the background, a
notification ("Request from your phone: create a session for example.com").
Clicking it brings the window forward. The window never takes focus on its
own while the user types elsewhere: it appears behind, with the notification,
unless the code window was in front.

```
 ┌──────────────────────────────────────────────────────────┐
 │ Check words   guitarist revenge                          │
 │ Your phone shows the same two words. If not, decline.    │
 │                                                          │
 │ Requester  "Gnokey Mobile" for "example.com"             │
 │            claimed, not verified                         │
 │ Network    dev via http://127.0.0.1:26657                │
 │ Signer     g1jg8m…sqf5  "test1"  (gnokey key, local)     │
 │ Action     sign and broadcast                            │
 │                                                          │
 │ 1  Create session  g1g3m5…4rq0                           │
 │      Allowed   vm/exec:gno.land/r/demo/counter           │
 │      Budget    5 GNOT per hour, at most 3,600 GNOT…      │
 │      Expires   2026-11-05 18:38 UTC (in about 30 days)   │
 │                                                          │
 │ Fee        0.0021 GNOT (gas 2,000,000), balance 10 GNOT  │
 │ Simulation ✓ ok, 1,356,267 gas                           │
 │                                                          │
 │ gnokey 1.2.0 at /Users/remi/go/bin/gnokey                │
 │                               [Decline]   [Sign…]        │
 └──────────────────────────────────────────────────────────┘
```

The content is the CLI's review, field for field: the same checks, warnings
(in colour, e.g. a wildcard allow path) and wording. To share it, the review
becomes a model (Architecture) that the CLI renders as text and the app as
widgets.

The approve button follows Settings → Signing (Signing by hand, below):
[Sign…] by default, [Copy gnokey command] when the user signs by hand. A small
link under it takes the other path for this request only ("Sign it myself" /
"Let gnokey-pair sign"), so the choice never costs an extra question.

Against approving by accident:

- The approve button stays disabled for one second after the window appears
  or changes.
- Neither button is the default: Return does nothing, Escape declines.
- One request at a time: a channel carries one request, and in automatic mode
  the next code is allocated only after it is done.

Decline answers `cancelled`, as `n` does in the CLI.

### Signing

[Sign…] runs `gnokey sign` in a pseudo-terminal (`creack/pty`, already a test
dependency) instead of inheriting a terminal, and reads what gnokey prints:

- The password prompt (`Enter password to decrypt key`): the window shows
  "Password for `test1` in gnokey" and a password field. The app writes it to
  the pseudo-terminal followed by a newline, then overwrites its buffer.
  gnokey sees a terminal, so it reads the password as it does when typed;
  `-insecure-password-stdin` is not used.
- A Ledger key: "Confirm on your Ledger" with a spinner, and gnokey's lines
  as it prints them.
- A wrong password: gnokey's message, and the field again (gnokey asks
  once; the app runs it again, at most three times).

Then the CLI's steps, unchanged: verify the signed transaction, dry-run,
broadcast, wait for inclusion. The window shows each step, then the result
("Included in block 240", the hash to copy) and [Done]. The phone gets the
result as today.

**What differs from the CLI.** In the CLI the password goes from the terminal
to gnokey and never crosses gnokey-pair. Here it crosses the app's memory for
the length of one write. The app never logs, stores or sends it, and keeps
it only in a byte slice it zeroes. A user who wants the password never to
reach gnokey-pair signs by hand.

### Signing by hand

Settings → Signing → **When I approve**: (•) gnokey-pair signs with gnokey,
( ) I run the gnokey command myself. Off by default. The command is the base
design's (Signing by hand): sign, dry-run and broadcast for `sendtx` with a
node; `gnokey sign` alone when the signed transaction goes back to the phone
(`signtx`, or signing offline).

[Copy gnokey command] approves the request, puts the command on the
clipboard, and shows it in the window, selectable:

- **Full command**: "Copied. Run it in a terminal: it signs and broadcasts.
  Your phone watches the chain." The phone has its answer (`manual`); [Done]
  removes the transaction file.
- **`gnokey sign` alone**: "Copied. Run it in a terminal; gnokey-pair sends the
  signed transaction to your phone." The window waits for the signed file,
  then checks it and answers the phone, as when gnokey-pair signs. [Cancel]
  answers `cancelled`.

The command carries no secret, so the clipboard holds nothing sensitive.

### Settings

```
 General   Start gnokey-pair when you log in      [x]
           Connect to the relay   (•) When I ask   ( ) When gnokey-pair starts
           Wait for the phone     10 minutes (when I ask)
           Notifications                          [x]
 Signing   When I approve   (•) gnokey-pair signs with gnokey
                            ( ) I run the gnokey command myself
 Networks  dev        http://127.0.0.1:26657                       [Edit] [Remove]
           topaz-1    https://rpc.topaz.testnets.gno.land          [Edit] [Remove]
           gnoland-1  offline: no node, the phone broadcasts       [Edit] [Remove]
           Unknown chain: ( ) ask each time  (•) ask, then remember
 gnokey    /Users/remi/go/bin/gnokey  (1.2.0)                      [Change…]
           Home: gnokey's default                                  [Change…]
 Relay     wss://gnokey-pair.berty.io/v1                           [Change…]
```

**Networks** replace the CLI's `-remote`. A request names a chain id and
suggests an RPC. For a chain id in the list, the app uses the listed node and
ignores the suggestion, as `-remote` does. For an unknown chain the review
shows the suggested RPC and asks to use it, and by default remembers the
answer. The base design's rules apply: the node's chain id must match.

A chain can be set to **offline** instead of a node: the CLI's `-offline` for
that chain. The review then lists what was not checked (base design, Signing
offline), and the signed transaction goes back to the phone, which
broadcasts. A node that does not answer is never turned into offline on its
own; the review says the node failed and offers to edit the network. For a
computer without internet, the relay is set to one on the local network
(Settings → Relay; README, "Running your own relay").

Settings live in `~/Library/Application Support/gnokey-pair/settings.json` on
macOS and `$XDG_CONFIG_HOME/gnokey-pair/settings.json` on Linux.

### Start at login

- **macOS**: `SMAppService.mainApp` `register()` and `unregister()`, from a
  few lines of Objective-C through cgo. The app appears in System Settings →
  General → Login Items. The checkbox reads `status` on every opening, so it
  reflects a change the user made there. This needs the app bundle, which is
  how the app ships (Packaging).
- **Linux**: an XDG autostart entry,
  `$XDG_CONFIG_HOME/autostart/land.gno.gnokey-pair.desktop`, with
  `Exec=<path> --background`. The checkbox writes or deletes it. GNOME, KDE,
  XFCE and Cinnamon all read that directory. A systemd user service is not
  used: it can start before the graphical session, with no tray.

"At boot" means at login: the app needs the user's session for the tray and
the notifications. Starting at login and connecting at launch are separate:
in the default mode the app starts idle and connects only when asked.

## Architecture

```
contribs/gnokey-pair/
  wormhole/, protocol/        unchanged
  pipeline/                   new: run.go, request.go, review.go, verify.go,
                              chain.go, keys.go moved out of package main
    Review                    the review as data; CLI text renderer here
    UI interface              Review(ctx, *Review) (approve bool, err)
                              Password(ctx, prompt) / Ledger(ctx) / Step(...)
  main.go                     the CLI: a terminal UI over pipeline
  desktop/                    new module: the app
    go.mod                    requires fyne; replace ../ and ../../..
    main.go, tray.go, windows (code, review, sign, settings),
    relay.go (manual and automatic modes, reconnection), autostart_*.go,
    pty.go
```

- **`pipeline`** holds what `run.go` does today, behind a UI interface. The
  CLI implements it with the terminal (its output unchanged: the golden files
  stay); the app with windows. The review becomes a struct of sections,
  lines and warnings; the CLI's text comes from it.
- **`desktop` is its own module** so that Fyne and its cgo dependencies stay
  out of the gnokey-pair module, which gnokey-mobile imports.
- The app runs the pipeline in process. There is no daemon with a separate
  UI process: one program per session, so no IPC to secure.

### Why Fyne

The app is small, Go is where the pipeline is, and it needs a tray on both
systems. Fyne gives windows and a tray (`desktop.App.SetSystemTrayMenu`: an
`NSStatusItem` on macOS, StatusNotifierItem over D-Bus on Linux) from Go,
with cgo for OpenGL. It does not look native; for four small windows that is
an acceptable trade (Alternatives).

## Packaging

- **macOS**: a universal `gnokey-pair.app` (bundle id `land.gno.gnokey-pair`,
  `LSUIElement` so it has no Dock icon), signed with the Developer ID
  Application certificate of team `WMBQ84HN4T`, hardened runtime, notarized.
  Distributed as a DMG and a Homebrew cask. Notification permission is asked
  on first use.
- **Linux**: a tarball and a `.deb` with the binary, the `.desktop` entry and
  icons; `make install-desktop` for a local build. Fyne needs the GL and X11
  or Wayland libraries, listed as package dependencies. Not Flatpak or Snap:
  their sandboxes would have to be opened up to run the user's `gnokey` and
  read its home, which removes their point.
- The app and the CLI are both called gnokey-pair: the CLI is the
  `gnokey-pair` command, the app the `gnokey-pair.app` bundle and the
  `gnokey-pair` launcher entry. The CLI ships as today, unchanged.

## Alternatives considered

- **Remembering a phone** (pair once; a secret derived from the first
  handshake names a rendezvous the app keeps claimed, and the phone sends
  without a code). Fewer codes, but the phone would keep a standing way to
  reach the computer. Rejected: every request starts from a code the user
  pairs with.
- **Allocating a new code by itself after a wrong guess**, in automatic mode.
  Unlimited guesses for an attacker; the app waits for the user instead.
- **Native UIs** (SwiftUI `MenuBarExtra` on macOS, GTK 4 on Linux, the Go
  pipeline as a library or daemon). Best look and platform behaviour, but two
  UI codebases in two languages and an IPC boundary around the review. Worth
  revisiting if the app grows.
- **Wails v3** (Go with a web view). Nicer styling than Fyne, but its tray
  support is in the v3 alpha, and a web view adds a browser engine to the
  program that shows what is signed.
- **A daemon (systemd user service, launchd agent) with a separate UI.**
  Nothing can be reviewed or signed without a session anyway, and the split
  needs authenticated IPC.
- **Opening a terminal for `gnokey sign`** keeps the password out of the app,
  but pops a terminal for every signature, differently on every Linux
  desktop.
- **`-insecure-password-stdin`.** Simpler than a pseudo-terminal, but the
  password then goes through a pipe, and the flag's name tells users it is the
  wrong path.

## Risks

- **GNOME without AppIndicator.** No tray; the fallback mode keeps it usable,
  but users may think the app is not running. The welcome window says which
  mode is in use.
- **A code kept ready can be guessed once.** Two words give an attacker one
  chance in 65,536 per code, and the app does not renew a burned code on its
  own. A lucky guess gets a request in front of the user, who still has the
  check words, the review and the password.
- **The review is now graphical.** A rendering bug could show something other
  than what is signed. The review model is shared with the CLI and tested
  there; the app's widgets render only its fields, and golden screenshots
  cover each message type.
- **gnokey's prompts are text meant for humans.** The pseudo-terminal bridge
  matches `Enter password` and the Ledger lines; a gnokey release that changes
  them breaks signing. The app shows gnokey's raw output when it does not
  recognise a prompt, and tests run against the gnokey version the module
  requires.
- **Relay load.** In automatic mode each running app holds one WebSocket
  while idle. Small per user, but the default relay must be sized for it.

## Testing

- **Pipeline extraction.** The CLI's existing tests, golden files and
  end-to-end test pass unchanged.
- **Pseudo-terminal bridge.** A fake `gnokey` script that prompts for a
  password, rejects a wrong one, or prints Ledger lines; the bridge must
  answer, retry and surface each case.
- **Relay modes**, against the reference relay (`relaytest`): manual connects
  only on demand and disconnects after the request, the timeout or Cancel;
  automatic connects at launch, allocates the next code after a request, does
  not renew after a wrong guess, renews after the nameplate was pruned (a
  long sleep, with `MAILBOX_EXPIRE` shortening the 11 minutes), and goes idle
  on Disconnect; switching modes while a code is ready.
- **Autostart.** The XDG entry written and removed in a temporary
  `XDG_CONFIG_HOME`; on macOS, `SMAppService` checked by hand.
- **Windows.** Fyne's `test` package drives the review and sign windows:
  the approve button disabled at first, Return not approving, Escape
  declining; the button and link following the Signing setting; the command
  copied and shown, [Done] removing the file, the wait for a signed file.
- **Offline chain.** A chain set to offline signs with the phone's account
  and returns `signedtx`; an unreachable node on a chain with a node stays an
  error.
- **By hand**: macOS 14 and 15; Ubuntu 24.04 (GNOME with AppIndicator),
  Fedora (GNOME without it), KDE Plasma; with a local key and a Ledger; in
  both relay modes.
