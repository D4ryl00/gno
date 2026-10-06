# gnokey-pair desktop: a menu bar app that starts at login

Design doc. Status: proposed. Builds on
`2026-10-05-gnokey-pair-design.md` (the CLI, its protocol and its review),
which it does not change for the one-shot flow.

## Context

gnokey-pair is a terminal program. To hand a transaction from Gnokey Mobile to
the computer, the user opens a terminal, runs `gnokey-pair -remote <rpc>`,
scans the code, reviews in the terminal, types `y`, then the gnokey password.
Every request starts with that terminal session, and the code works once.

The request: a desktop app for macOS and Linux that starts at login (a
setting turns that off), so the user never has to run the CLI.

Starting at login is only worth it if the phone can reach the computer without
a new code each time. With one-shot codes, a resident app saves the terminal
but not the trip to the computer to read a code. So this design has two parts:

1. **The app**: a menu bar (tray) app that does what the CLI does, with
   windows instead of a terminal, and starts at login.
2. **Remembered phones**: pair once with a code, then the phone sends to that
   computer directly, and the app pops up the review. This answers the base
   design's open question 1 ("pairing once").

Unchanged: **`gnokey` is not modified** and stays the only program that signs;
the relay is the default one; the CLI keeps working.

## Decision

```
 phone                               relay                    computer (app, from login)
 ─────                               ─────                    ──────────────────────────
                                                              menu bar icon
 first time: "Send to my computer"                            "Pair a phone…": code + QR
   scan / type code ───────────── one-shot channel ─────────▶ (as the CLI)
   check words, "Remember this computer"                      check words, name the phone
   K = link secret, derived on both ends from the PAKE key; never sent

 later: "Send to MacBook"                                     listening on nameplate(K)
   claim nameplate(K), PAKE with code(K) ───────────────────▶ handshake
   request ──────────────────────────────────────────────────▶ notification + review window
                                                              [Decline]  [Sign…]
                                                              password / Ledger → gnokey sign
 ◀──────────────────────────────── result ───────────────────  dry-run, broadcast
```

- One Go program with a [Fyne](https://fyne.io) UI: a menu bar icon, and
  windows for pairing, review, signing and settings. It runs the same pipeline
  as the CLI, in process.
- `gnokey sign` runs in a pseudo-terminal the app owns. The app answers its
  password prompt from a password field, or tells the user to confirm on the
  Ledger.
- Start at login: `SMAppService` on macOS, an XDG autostart entry on Linux.
  On by default; Settings → General turns it off.
- Remembered phones: a long-term secret per phone, derived from the first
  pairing's key. It names a nameplate the app keeps claimed while it runs, and
  gives the password for the handshake on it.

## Scope

In:

- macOS 13 and later (Apple silicon and Intel), Linux desktops with X11 or
  Wayland (GNOME, KDE Plasma, XFCE, Cinnamon).
- Everything the CLI does: `sendtx` and `signtx`, the review, the checks
  before and after signing (base design, Pipeline).
- Start at login, settings, notifications, remembered phones.
- The gnokey-mobile changes for remembered computers.

Out:

- Windows. Fyne runs there, but autostart, the pseudo-terminal and packaging
  differ; a later design.
- Storing the gnokey password, or any key. The app never keeps a password
  past the `gnokey sign` it was typed for.
- Bundling `gnokey`. The app runs the gnokey the user installed and shows
  which one (as the CLI prints it).
- Auto-update. Releases ship as packages; updates go through them.

## The app

### Running

One process per user session, started at login with `--background` (no
window) or from the launcher (opens the menu). A second launch hands over to
the running one through a Unix socket in the user's runtime directory
(`$TMPDIR` on macOS, `$XDG_RUNTIME_DIR` on Linux) and exits.

The menu bar icon has three states: idle, a request waiting (badge), and
offline (the relay is unreachable; the app retries with backoff and says so in
the menu).

```
 ┌───────────────────────────────┐
 │ gnokey-pair                   │
 │ ● Listening for 2 phones      │   or "No phone remembered"
 │ ───────────────────────────── │   or "Offline: retrying the relay…"
 │ Pair a phone…                 │
 │ Recent ▸                      │   last 10 results: what, when, block
 │ ───────────────────────────── │
 │ Settings…                     │
 │ Quit gnokey-pair              │
 └───────────────────────────────┘
```

Quit stops listening: remembered phones then wait and fall back (see the
phone side). Closing a window never quits.

**Linux without a tray.** GNOME shows tray icons only with the AppIndicator
extension (Ubuntu ships it, Fedora does not). The app checks for a
`StatusNotifierWatcher` on the session bus. Without one it still runs and
listens; a request raises a notification that opens the review; the
launcher entry opens a small main window with the menu's items. Settings says
which mode is in use.

### First launch

A welcome window: what the app does, where it found `gnokey` (path and
version), and **Start gnokey-pair when you log in**, checked. Continue
applies it and offers "Pair a phone…".

If `gnokey` is not found it says so and asks for its location. A GUI app does
not get the shell's `PATH` (launchd gives `/usr/bin:/bin:/usr/sbin:/sbin`,
autostart on Linux the session's), so the app looks in `PATH`,
`$(go env GOPATH)/bin`, `~/go/bin`, `/opt/homebrew/bin`, `/usr/local/bin` and
`/usr/bin`, then asks. The path is saved; Settings shows it with the version.

### Pairing

"Pair a phone…" opens a window with the QR and the code, as the CLI prints
them, over the same one-shot channel:

```
 ┌──────────────────────────────────────────────┐
 │ Pair a phone                                 │
 │                                              │
 │   ▄▄▄▄▄▄▄ ▄ ▄▄ ▄▄▄▄▄▄▄                      │
 │   █ ▄▄▄ █ ▀█▄▀ █ ▄▄▄ █    7-guitarist-revenge│
 │   █ ███ █ ▄▀▄█ █ ███ █                      │
 │   ▀▀▀▀▀▀▀ ▀ ▀ ▀▀▀▀▀▀▀    In Gnokey Mobile:  │
 │                           Send to my computer│
 │ Waiting for the phone… (9:41 left)  [Cancel] │
 └──────────────────────────────────────────────┘
```

After the handshake the window shows the check words, large, and "Your phone
shows the same two words. If they differ, cancel." What follows depends on
what the phone sent:

- **A request** (today's protocol): the review, below.
- **A link** (a phone that asks to remember this computer): "Remember this
  phone?" with a name field filled from the phone's (`Rémi's iPhone`) and
  [Don't remember] [Remember]. A phone can link and send a request on the same
  channel.

### Review

A request opens the review window and, if the app is in the background, a
notification ("Rémi's iPhone: create a session for example.com"). Clicking it
brings the window forward. The window never takes focus on its own while the
user types elsewhere: it appears behind with the notification, unless the
user just paired.

```
 ┌──────────────────────────────────────────────────────────┐
 │ Request from Rémi's iPhone            (remembered phone) │
 │ Gnokey Mobile for example.com         claimed, not verified
 │                                                          │
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
becomes a model (below) that the CLI renders as text and the app as widgets.

Against approving by accident:

- [Sign…] stays disabled for one second after the window appears or changes.
- Neither button is the default: Return does nothing, Escape declines.
- One review at a time. A second request, from any phone, gets `busy` (an
  error code the phone shows as "the computer is reviewing another request")
  and is not queued.

Decline answers `cancelled`, as `n` does in the CLI.

### Signing

[Sign…] runs `gnokey sign` in a pseudo-terminal (`creack/pty`, already a test
dependency) instead of inheriting a terminal, and reads what gnokey prints:

- A password prompt (`Enter password`): the window shows "Password for
  `test1` in gnokey" and a password field. The app writes it to the
  pseudo-terminal followed by a newline, then overwrites its buffer. gnokey
  sees a terminal, so it reads the password as it does when typed;
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
it only in a byte slice it zeroes. Running gnokey in a terminal window instead
would keep the CLI's property, at the cost of a terminal popping up for every
signature (see Alternatives).

### Settings

```
 General   Start gnokey-pair when you log in      [x]
           Notifications                          [x]
 Phones    Rémi's iPhone   paired 2026-10-06, last request today   [Rename] [Forget]
 Networks  dev        http://127.0.0.1:26657                       [Edit] [Remove]
           topaz-1    https://rpc.topaz.testnets.gno.land          [Edit] [Remove]
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

Settings live in `~/Library/Application Support/gnokey-pair/settings.json` on
macOS and `$XDG_CONFIG_HOME/gnokey-pair/settings.json` on Linux. Link secrets
do not (below).

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
  used: it can start before the graphical session, with no tray and a locked
  keyring.

"At boot" means at login: the app needs the user's session for the tray, the
notifications and the keychain.

## Remembered phones

### The link

On a one-shot channel, after the handshake, a phone that wants to be
remembered sends a `link` message before (or instead of) a request:

```json
{ "type": "link", "name": "Rémi's iPhone" }
```

The app answers after the user chose:

```json
{ "type": "linked", "name": "MacBook de Rémi" }
{ "type": "link_declined" }
```

Neither message carries a secret. Both ends derive the link secret from the
channel's key, which SPAKE2 made and the check words confirmed:

```
K = HKDF-SHA256(channel key, salt = "", info = "gnokey-pair/v1 link")
```

Both ends store K with the peer's name. A `link` capability in `versions`
says an end supports this; the phone offers "Remember this computer" only
then.

### Meeting again

Each link has a rendezvous both ends compute from K:

```
nameplate = 30 decimal digits of HMAC-SHA256(K, "nameplate")
code      = nameplate + "-" + base32(HMAC-SHA256(K, "password"))[:26]
```

The relay accepts a nameplate the client chooses: digits only, 40 at most
(`check_valid_nameplate` in magic-wormhole-mailbox-server). The code is what
SPAKE2 runs on, as with a typed code, but with 128 bits of secret instead of
two words, so the relay cannot guess it even with unlimited tries.

- **The app** claims the nameplate of every remembered phone and waits: one
  WebSocket per phone (one nameplate per connection in the mailbox protocol).
  The mailbox server keeps a mailbox while a client is subscribed to it, so
  waiting does not expire. The server allows one claim per connection, so
  after each request the app releases and claims again on a new connection.
  It reconnects after sleep or a network change, with the `wormhole`
  package's reconnection.
- **Leftovers on the relay.** The relay keeps a disconnected client's claim
  for up to 11 minutes, admits two sides per nameplate (counting released
  ones until the nameplate is deleted, which happens once no side claims it),
  and refuses a side that reclaims a nameplate it released. An end that died
  between claim and release (a crash, a laptop shut, the phone app killed)
  would block the next meeting with `crowded`. So:
  - Each end keeps one side id per link, stored with K.
  - An end that did not see its release acknowledged (it knows from a flag
    it stores before claiming) first claims as the same side, then releases
    and closes; with no other side, the relay deletes the nameplate.
  - A claim refused (`crowded`, or a reclaim of a released side while the
    other end has not released yet) is retried with backoff. The relay's
    pruning bounds the wait to its 11 minutes.

  A nameplate rotated per request (HMAC over a counter, or a fresh one sent
  in each result) would not remove this, since any nameplate can be left
  claimed, and it adds counters that drift. The tests cover a crash between
  claim and release on each end.
- **The phone**, on "Send to MacBook", joins with the derived code. The
  handshake succeeds only against the app holding K. No check words are
  shown; the review names the phone instead, and the phone shows "Sent to
  MacBook de Rémi".
- If the app is not running (laptop closed, app quit), the phone waits up to
  a minute ("Waiting for MacBook… Is gnokey-pair running?") with [Stop] and
  [Use a code instead].

The relay sees the same nameplate each time a phone sends, so it can tell
that one link is used again, and when. It cannot read the requests, and
cannot use the link without K. Rotating the nameplate (HMAC over a counter)
would hide that, at the cost of counters that can drift between the two ends;
not in v1.

### Storing and forgetting

K lives in the OS keychain: macOS Keychain, Linux Secret Service
(GNOME Keyring, KWallet), through `zalando/go-keyring`. If there is no Secret
Service, the app says that phones cannot be remembered and keeps the one-shot
flow. It does not fall back to a file.

Forget, from either end, deletes K. The other end finds out at its next
attempt: the handshake fails, and it says "this computer (phone) no longer
knows you; pair again with a code". The app also offers [Forget this phone]
in the review window.

### What a remembered phone can do

Holding K lets a phone ask; it never lets it sign. Every request goes through
the review and gnokey's password or Ledger. A lost phone, or K taken from it,
can at worst put review windows in front of the user, one at a time. The
review shows which phone asks, Forget is one click away, and the user should
forget a lost phone, as they revoke its session.

## Architecture

```
contribs/gnokey-pair/
  wormhole/, protocol/        unchanged; protocol gains link messages, busy
  pipeline/                   new: run.go, request.go, review.go, verify.go,
                              chain.go, keys.go moved out of package main
    Review                    the review as data; CLI text renderer here
    UI interface              Review(ctx, *Review) (approve bool, err)
                              Password(ctx, prompt) / Ledger(ctx) / Step(...)
  main.go                     the CLI: a terminal UI over pipeline
  desktop/                    new module: the app
    go.mod                    requires fyne; replace ../ and ../../..
    main.go, tray.go, windows (pair, review, sign, settings), autostart_*.go,
    link.go (listeners per remembered phone), keyring.go, pty.go
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

## gnokey-mobile changes

- The "Send to my computer" card lists remembered computers first, one
  button each ("Send to MacBook de Rémi"), then "Scan or type the code".
- After the check words on a coded channel, "Remember this computer" (on by
  default when the app's `versions` has `link`), with the phone's name
  editable.
- Settings → Send to my computer: the remembered computers, with Rename and
  Forget.
- K in the iOS Keychain (`kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`)
  and the Android Keystore-backed encrypted preferences the app already uses.
- `core`: derive K, the nameplate and the code; `Join` with the derived code;
  the `link`, `linked`, `busy` messages. The `wormhole` package already
  joins any `<digits>-<text>` code; the derived one goes to it directly, not
  through `ParsePairInput`, which only takes what a user types.

## Packaging

- **macOS**: a universal `gnokey-pair.app` (bundle id `land.gno.gnokey-pair`,
  `LSUIElement` so it has no Dock icon), signed with a Developer ID, hardened
  runtime, notarized. Distributed as a DMG and a Homebrew cask. Notification
  permission is asked on first use.
- **Linux**: a tarball and a `.deb` with the binary, the `.desktop` entry and
  icons; `make install-desktop` for a local build. Fyne needs the GL and X11
  or Wayland libraries, listed as package dependencies. Not Flatpak or Snap:
  their sandboxes would have to be opened up to run the user's `gnokey` and
  read its home, which removes their point.
- The CLI ships as today, unchanged.

## Phases

1. **The app with one-shot codes.** Pipeline extraction, tray, pairing,
   review, signing through the pseudo-terminal, settings, start at login,
   packaging. No protocol change; gnokey-mobile unchanged.
2. **Remembered phones.** The link messages and rendezvous in `protocol` and
   `wormhole`, the listeners and keyring in the app, the gnokey-mobile
   changes.

Phase 1 already removes the terminal. Phase 2 is what makes starting at login
pay off; the two can ship together if phase 1 is not released alone.

## Alternatives considered

- **Native UIs** (SwiftUI `MenuBarExtra` on macOS, GTK 4 on Linux, the Go
  pipeline as a library or daemon). Best look and platform behaviour, but two
  UI codebases in two languages and an IPC boundary around the review. Worth
  revisiting if the app grows.
- **Wails v3** (Go with a web view). Nicer styling than Fyne, but its tray
  support is in the v3 alpha, and a web view adds a browser engine to the
  program that shows what is signed.
- **A daemon (systemd user service, launchd agent) with a separate UI.** The
  listening survives without a session, but nothing can be reviewed or signed
  without one, and the split needs authenticated IPC.
- **Opening a terminal for `gnokey sign`** keeps the password out of the app,
  but pops a terminal for every signature, differently on every Linux
  desktop.
- **`-insecure-password-stdin`.** Simpler than a pseudo-terminal, but the
  password then goes through a pipe, and the flag's name tells users it is the
  wrong path.
- **Remembering by keeping one channel open** (several requests per channel).
  The mailbox expires idle channels after a while without a subscriber, and a
  channel does not survive either end restarting; a derived rendezvous does.
- **Persistent pairing without a relay** (LAN discovery with mDNS). Fails on
  guest Wi-Fi, mobile data and VPNs, where the relay works.

## Risks

- **GNOME without AppIndicator.** No tray; the fallback mode keeps it usable,
  but users may think the app is not running. The welcome window says which
  mode is in use.
- **The review is now graphical.** A rendering bug could show something other
  than what is signed. The review model is shared with the CLI and tested
  there; the app's widgets render only its fields, and golden screenshots
  cover each message type.
- **gnokey's prompts are text meant for humans.** The pseudo-terminal bridge
  matches `Enter password` and the Ledger lines; a gnokey release that changes
  them breaks signing. The app shows gnokey's raw output when it does not
  recognise a prompt, and tests run against the gnokey version the module
  requires.
- **Relay load.** Each running app holds one WebSocket per remembered phone.
  Small per user, but the default relay must be sized for it.

## Testing

- **Pipeline extraction.** The CLI's existing tests, golden files and
  end-to-end test pass unchanged.
- **Pseudo-terminal bridge.** A fake `gnokey` script that prompts for a
  password, rejects a wrong one, or prints Ledger lines; the bridge must
  answer, retry and surface each case.
- **Links.** Against the reference relay (`relaytest`): link then meet again;
  the app restarting between; the relay restarting; sleep simulated by
  dropping the connection; forget on either end; two phones at once
  (`busy`); a phone with the wrong K; each end killed between claim and
  release, then meeting again (the cleanup, and the backoff while the other
  end's claim is pruned, with `MAILBOX_EXPIRE` shortening the 11 minutes).
- **Autostart.** The XDG entry written and removed in a temporary
  `XDG_CONFIG_HOME`; on macOS, `SMAppService` checked by hand.
- **Windows.** Fyne's `test` package drives the review and sign windows:
  [Sign…] disabled at first, Return not approving, Escape declining.
- **By hand**: macOS 14 and 15; Ubuntu 24.04 (GNOME with AppIndicator),
  Fedora (GNOME without it), KDE Plasma; with a local key and a Ledger.

## Open questions

1. **Name.** "gnokey-pair" fits the CLI; the app might want a user-facing
   name ("Gnokey Desktop"?), which also sets the bundle and autostart ids.
2. **Signing identity.** Whose Developer ID signs and notarizes the macOS app.
3. **One release or two.** Ship phase 1 alone, or wait for remembered phones.
4. **Default for "Remember this computer"** on the phone: on (fewer codes) or
   off (nothing stored until asked).
