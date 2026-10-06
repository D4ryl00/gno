# gnokey-pair: hand a transaction from a phone to desktop `gnokey`

Design doc. Status: accepted. The desktop side is implemented in
`contribs/gnokey-pair`, the gnokey-mobile side on its `feat/send-to-computer`
branch. Signing offline and signing by hand (below) are implemented in
gnokey-pair; their gnokey-mobile side is not.

The GnoConnect sections cited below (launch links, `signtx`, sessions,
"Obtaining the identity's signature") are in `docs/resources/gnoconnect.md` as
revised by #5970, which is not merged yet.

## Context

Gnokey Mobile holds a session key, never the identity's (master) key. Anything
only the identity can sign has to happen on another device: creating, renewing
and revoking sessions (`auth/*`, which a session can never sign), and any call
the user chooses to sign with the identity itself. Today the app shows the
command to run there, as text and as a QR (`gnokey-mobile/core/gnoconnect_tx.go`,
`BuildSessionTx`; `docs/resources/gnoconnect.md`, "Obtaining the identity's
signature"):

```
printf '%s\n' '<amino JSON>' > tx.json && gnokey sign -tx-path tx.json \
  -chainid <id> -account-number <n> -account-sequence <seq> <key> \
  && gnokey broadcast -dry-run -remote <rpc> tx.json \
  && gnokey broadcast -remote <rpc> tx.json
```

Getting that command from a phone onto a computer is the hard part. A desktop
has no camera to scan the QR, and the command is too long to retype. Once it
arrives it has more problems:

- **The review is weak.** `gnokey sign` never shows the transaction; it asks
  for a password, or for confirmation on the Ledger. With a keyfile, the user's
  only review is reading the JSON in the command.
- **It goes stale.** The account number and sequence are filled in by the
  phone, so the command fails if the identity sends anything else first.
- **It is shell text from another device.** Pasting it runs whatever it
  contains.

The constraint for this design: **`gnokey` is not modified.**

## Decision

A separate desktop program, `gnokey-pair`, receives the transaction over
an end-to-end encrypted channel, shows its own review, looks up the account
itself, and runs the unmodified `gnokey sign` to sign. gnokey stays the only
program that touches keys.

```
 phone (gnokey-mobile)                    relay                 desktop
 ─────────────────────                    ─────                 ───────
                                                     $ gnokey-pair
                                    ◀── allocate ──  code 7-guitarist-revenge
                                                     + terminal QR
 scan QR / type code ── claim ──▶
 ◀────────────── SPAKE2: shared key, one guess for an attacker ──────────────▶
 ◀───────────── versions (capabilities) ────────────────────────
 request {tx, chainid, signer} ──────────────────────────────▶ decode, validate
                                                               query account
                                                               review → [y/N]
                                                               gnokey sign ◀▶ Ledger
                                                               dry-run, broadcast
 ◀──────────────────────────────── result {status, hash} ──────
 watch the chain (as today)
```

## Scope

In scope:

1. The channel: a Go package implementing the magic-wormhole generic protocol
   (mailbox and PAKE), shared by gnokey-pair and gnokey-mobile's Go core.
2. The `gnokey-pair` CLI, accepting one kind of request: an unsigned
   transaction (`tx`).
3. gnokey-mobile: a "Send to my computer" path next to the existing command and
   QR on the handoff screen.

Out of scope for v1, designed for later (see Phase 2):

- GnoConnect intents (`sendtx` / `signtx` / `connect` / `disconnect`) sent by
  dapps directly.
- Browser requesters.
- More than one request per channel.
- `MsgAddPackage`, `MsgRun`, multisig and session signers on the desktop.

## The channel

### Protocol

The channel implements the magic-wormhole **generic** API, not its file
transfer:

- A mailbox (rendezvous) server over WebSocket.
- A short code (`<nameplate>-<word>-<word>`).
- SPAKE2 key agreement from the code.
- One encrypted message per phase.

The application ID is `gno.land/gnokey-pair/v1`. The magic-wormhole
documentation requires both sides to use the same ID, and it keeps this traffic
apart from other wormhole applications on a shared server.

**The desktop allocates the code.** That lets the phone, which has a camera,
scan it. The generic protocol allows either side to allocate.

**Library.** `wormhole-william` (v1.0.8) cannot be used as is:

- Its own `wormhole` package exposes only text, file and directory transfer,
  always lets the sender allocate, and has no generic message API.
- Its exported mailbox client (`rendezvous.Client`) cannot reattach. Any read
  error closes it for good, `Connect` runs only once per client, and both the
  `open` call and the mailbox ID are unexported. A suspended phone could
  therefore never get back into its mailbox (see Reachability).

So our `wormhole` package has its own mailbox client over
`github.com/coder/websocket`, the maintained successor of `nhooyr.io/websocket`,
which `wormhole-william` uses. It reuses from upstream only what is
self-contained:

- `salsa.debian.org/vasudev/gospake2` for SPAKE2 (symmetric, the app ID as
  identity);
- `wormhole-william/wordlist` for the code words.

The rest of the generic layer is about 100 lines, mirroring the protocol:

- the `pake` phase (`{"pake_v1": hex}`, hex-encoded, unencrypted);
- HKDF-SHA256 phase keys
  (`"wormhole:phase:" ‖ SHA256(side) ‖ SHA256(phase)`) and the verifier
  (`"wormhole:verifier"`);
- NaCl secretbox with a random 24-byte nonce in front;
- the encrypted `version` phase carrying `app_versions`;
- application phases `"0"`, `"1"`, … holding the raw message bytes.

**Spike result (2026-10-05).** A prototype of this layer was tested against the
Python reference implementation: `magic-wormhole` 0.24.0 as the peer and
`magic-wormhole-mailbox-server` 0.8.0 as the relay.

- Codes, verifiers, versions and application messages interoperate in both
  directions.
- A wrong code fails on both sides.
- A peer reattaches after its socket is killed, even after the other side has
  closed and gone.
- `wss://` works through an HTTP CONNECT proxy.

These become the package's interoperability tests, so that "it is
magic-wormhole" stays true rather than becoming a fork with the same name.

The package belongs in the gnokey-pair module (`contribs/gnokey-pair/wormhole`),
and gnokey-mobile imports it.

**Nameplate release.** Each side MUST release the nameplate as soon as the
peer's `pake` message arrives, as the reference clients do. This frees the
number for reuse right away. It is also required: with
`magic-wormhole-mailbox-server` 0.8.0, closing a mailbox whose nameplate is
still claimed raises a foreign-key error on the server. The server then drops
the connection without answering `closed`.

### Code and QR

gnokey-pair prints the code as text and as a terminal QR (`rsc.io/qr`, drawn
with half blocks in black on white so it scans on dark terminals too). The QR
holds:

```
gnopair:<code>?relay=<percent-encoded mailbox URL>
```

The relay is in the QR because both sides must use the same server. A typed
code uses the phone's configured relay. gnokey-mobile scans from inside the
handoff screen, not through the OS camera, so the scan always attaches to the
request on screen.

The default is two words: 16 bits per code. SPAKE2 gives an attacker **one
online guess** per code, and a wrong guess shows on both sides: the PAKE fails
and the channel closes "scary". gnokey-pair accepts one peer per code. A code is
single-use and expires after 10 minutes without a peer (`-timeout`).

### Relay

The mailbox server is `wss://gnokey-pair.berty.io/v1`, the default of both
ends. The public `ws://relay.magic-wormhole.io:4000/v1` works
for the two native ends, since the content is encrypted end to end. It still
exposes metadata in clear, it is a dependency the project does not control, and
a browser on an `https:` page cannot reach a `ws:` server (Phase 2). Both ends
take `-relay` / a setting.

### Reachability

The two devices never need to reach each other: each opens one **outbound**
WebSocket to the relay, which forwards the encrypted messages.

```
phone (4G, CGNAT) ──outbound wss──▶ relay ◀──outbound wss── desktop (home NAT)
```

- **NAT, firewalls, different networks.** No inbound port, port forwarding,
  UPnP or hole punching is needed. A phone on mobile data and a desktop behind
  a home or office NAT behave like two devices on one Wi-Fi network.
  Magic-wormhole's *transit* layer, which tries direct connections for file
  transfer, is not used: every message goes through the mailbox.
- **Port and transport.** The gno relay listens on `wss://` port 443, so to a
  firewall it looks like any HTTPS traffic. Networks that allow only 80/443, or
  that inspect plain WebSocket upgrades, block the public relay on
  `ws://…:4000` but not this one.
- **Proxies.** The desktop channel dials with an `http.Client` whose transport
  uses Go's `http.ProxyFromEnvironment`, so `HTTPS_PROXY` / `NO_PROXY` apply.
  For `wss://` the handshake goes through an HTTP CONNECT tunnel; the spike
  confirmed this works. As everywhere in Go, a proxy is never used for
  `localhost` or loopback addresses.
- **Interrupted connections.** The phone's socket will often drop: iOS and
  Android suspend a backgrounded app while the user walks to the computer, and
  mobile networks change. So the `wormhole` package MUST:
  - reconnect with backoff;
  - bind again with the **same side ID**;
  - `open` the same mailbox ID. It may not `claim` again: the nameplate is
    already released.
  - ignore what the server replays on `open` (it replays every message in the
    mailbox) for phases already processed, and its own messages.

  The spike confirmed this against the reference server: the replay includes
  messages posted while the side was away, and messages posted by a side that
  has since closed.
- **How long a mailbox lives.** In `magic-wormhole-mailbox-server` 0.8.0:
  - A mailbox is deleted when every side that opened it has sent `close`. A side
    whose socket merely dropped still counts as open.
  - A mailbox is kept as long as **any** side is connected.
  - Once no side is connected, the mailbox is pruned after 11 minutes without
    activity. The server checks every 5 minutes, so it is gone 11 to 16 minutes
    after the last activity. This is a constant in the server
    (`CHANNEL_EXPIRATION_TIME`), not an option, so a gno relay running the
    reference server cannot lengthen it without patching.
- **The result outlives gnokey-pair.** gnokey-pair posts `result`, then stays
  connected until the phone sends `received` (see Messages), at most `-linger`
  (default 10 minutes). While gnokey-pair is connected, the mailbox lives
  however long the phone is away. After gnokey-pair closes, the result survives
  at least another 11 minutes. So a phone suspended during the review gets the
  answer if it comes back within roughly 20 minutes of the result. Later than
  that, it learns the outcome by watching the chain, as it does today.
- **What it still needs.** Both ends need internet access to the relay. The
  desktop also needs a node, to read the account and broadcast. The relay is the
  one shared dependency: when it is down, nothing pairs, and the user falls back
  to the command on the phone's screen. Air-gapped signing stays with the
  existing `gnokey sign` commands.

### Versions

Each side's `app_versions` (the wormhole `version` phase, sent right after the
PAKE succeeds) carries:

```json
{ "gnokey-pair": { "v": 1, "kinds": ["tx"] } }
```

gnokey-pair also sends `"modes": ["sendtx", "signtx"]`. A requester that wants a
kind or mode the peer did not list does not send it. It tells the user the
desktop needs a newer gnokey-pair. Unknown fields are ignored on both sides.

gnokey-pair also sends `"features": ["offline"]` when it accepts a `signtx`
request marked `offline` (Signing offline). A phone sends that marker only to
a gnokey-pair that lists it: an older one would ignore the field and query its
node anyway.

### Check words

After the PAKE, both sides MUST show the same two words derived from the
wormhole verifier: the app on the waiting screen, gnokey-pair at the top of the
review.
The review asks the user to compare them. A matching pair means the review the
user is reading came from their own phone. An attacker who guesses the code
(one chance in 65,536) and claims it first would produce different words, and
the real phone would also fail the PAKE.

## Messages

All messages are JSON, one per application phase. After `version` the phone
sends `request` (phase `"0"`) and then `received` (phase `"1"`). gnokey-pair
sends `result` (phase `"0"`).

### `request` (phone → desktop)

```json
{
  "kind": "tx",
  "mode": "sendtx",
  "chainid": "gnoland-1",
  "rpc": "https://rpc.gno.land:443",
  "signer": "g1…",
  "tx": { "msg": [ … ], "fee": { … }, "signatures": null, "memo": "" },
  "requester": { "name": "Gnokey Mobile", "for": "game.example" }
}
```

| field | required | meaning |
|---|---|---|
| `kind` | yes | `tx`: an unsigned `std.Tx` (amino JSON), exactly what gnokey-mobile already composes as `SessionTx.Document` |
| `mode` | yes | `sendtx` (sign, dry-run, broadcast) or `signtx` (sign, return the signed bytes). Required, so a desktop never has to guess whether to broadcast; same rationale as GnoConnect's hosts |
| `chainid` | yes | the chain the transaction is for; it goes into the sign bytes |
| `rpc` | no | advisory, as in GnoConnect: offered to the user when gnokey-pair has no node for `chainid` |
| `signer` | yes | the identity that must sign: the only signer the messages may name |
| `tx` | yes | at most 64 KiB, with no signatures |
| `requester` | no | display only, always shown labeled as *claimed*. `for` is the GnoConnect producer the phone is brokering for (its callback host), when there is one |
| `offline` | no | `true`: sign only, with no network operation on the computer (Signing offline). Valid only with `signtx` |
| `account` | with `offline` | `{"number": "12", "sequence": "5"}`, decimal strings as in amino JSON: the signer's account number and sequence, which gnokey-pair cannot read without a node. Ignored otherwise |

Without `offline`, gnokey-pair reads the account number and sequence from the
chain just before signing, which removes the staleness problem.
Absolute times inside the transaction, such as `MsgCreateSession.ExpiresAt`,
were set by the phone. gnokey-pair shows them and never changes them.

### `result` (desktop → phone)

```json
{ "status": "success", "hash": "<txhash>", "height": 1234 }
{ "status": "success", "signedtx": "<base64 amino-binary>" }
{ "status": "success", "manual": true }
{ "status": "cancelled" }
{ "status": "error", "code": "tx_failed", "detail": "dry-run: insufficient funds …" }
```

`status` and `code` use GnoConnect's closed vocabulary and keep its meanings:

- `invalid_request`: malformed or unsupported message type, wrong signer set,
  too large, already signed.
- `network_declined`: no node for the chain, the user declined the advertised
  one, or a chain id mismatch.
- `signer_unavailable`: the keybase has no key for `signer`.
- `tx_failed`: signing, dry-run or broadcast failed.

`signedtx` uses the same encoding as GnoConnect's `signtx` callback, and the
same rule applies: the receiver broadcasts it unmodified. `hash` has the
`sendtx` callback's encoding.

`signedtx` always means "signed, not broadcast": it answers `signtx`,
`offline` or not, and the phone broadcasts it.

`manual: true` answers `sendtx` when the user chose to run the gnokey command
themselves (Signing by hand): gnokey-pair has not signed anything, and the
phone watches the chain for the outcome. A phone that predates the field sees
a success with no block and keeps watching the chain, which is right.

`detail` is optional human-readable text **for display only**. Unlike a URL
callback, this channel is private, so the diagnostic can travel. A receiver
never branches on it.

As in GnoConnect, the result is a hint. The phone keeps confirming on chain,
which it already does after a handoff. If gnokey-pair stops between broadcasting
and answering, the chain is still the answer.

gnokey-pair always answers before closing, `cancelled` included. Ctrl-C during
the review counts as `cancelled`.

### `received` (phone → desktop)

```json
{ "received": true }
```

The phone sends this once it has read `result`, then closes. It only tells the
gnokey-pair it may leave (see Reachability). If it never comes, gnokey-pair
closes after `-linger`, and nothing else depends on it.

## The desktop program

### Command line

```
gnokey-pair [flags]

  -remote <url>        node for the request's chain (default: ask, see Network)
  -relay <url>         mailbox server
  -gnokey <path>       gnokey binary (default: $PATH lookup)
  -home <dir>          passed to gnokey as -home
  -timeout <duration>  wait for a peer (default 10m)
  -linger <duration>   after answering, wait for the phone's `received` (default 10m)
  -manual              print the gnokey command instead of running gnokey
                       (see Signing by hand)
```

At startup gnokey-pair prints the resolved `gnokey` path and its version, so the
user can see which binary will handle their key.

### Pipeline

1. **Receive.** Allocate a code, show it and the QR, and wait. Then run the
   PAKE and the version exchange, show the check words, and receive the request.
2. **Validate** before showing anything:
   - The request parses, `kind` and `mode` are known, and `tx` is within the
     size bound.
   - `tx` decodes as an amino JSON `std.Tx` with no signatures.
   - Every message is one gnokey-pair can render (below). An unknown type is
     `invalid_request`: anything not rendered is not reviewed, so it is not
     signed.
   - `tx.GetSigners()` is exactly `[signer]`.
3. **Network.**
   - With `-remote`: query the node's status and require its chain id to equal
     `chainid`.
   - Without it: show the advertised `rpc` and ask whether to use it, with the
     same chain id check. No means `network_declined`.
   - A lying node can at worst produce a stale account (signing then fails), a
     wrong simulation, or a dropped broadcast. It cannot change what is signed.
4. **Signer.** Run `gnokey list [-home]` and look for `signer` among the listed
   addresses. Note whether it is a Ledger key, to tell the user to look at the
   device. If it is not there, answer `signer_unavailable`.
   - This parses human-oriented output; see Risks.
   - If parsing fails, gnokey-pair does not guess. It goes on to signing and
     treats a gnokey "key not found" failure as `signer_unavailable`.
5. **Account.** Query `auth/accounts/<signer>` for the account number,
   sequence and balance. When the transaction revokes sessions, also query
   `auth/accounts/<signer>/sessions`, so the review can show what is revoked.
   - Where the chain allows unsigned simulation (everything v1 accepts; see
     `txNeedsSimulationSignature` in `tm2/pkg/crypto/keys/client/maketx.go`),
     simulate now. The review can then say "would fail: …" or show the gas
     used, before the user is asked anything.
6. **Review**, then a typed `y` (default No). See below.
7. **Sign.**
   - Write the unsigned transaction to a file in a fresh directory: `0700`
     directory, `0600` file, removed on exit.
   - Run:

     ```
     gnokey sign -tx-path <file> -chainid <chainid> \
       -account-number <n> -account-sequence <seq> [-home <dir>] <signer address>
     ```

     `gnokey sign` accepts a name or an address (`GetByNameOrAddress`), so the
     phone never needs to know the key's local name. gnokey-mobile currently
     asks the user for it.
   - The child process inherits the terminal, so the password prompt and the
     Ledger flow reach the user unchanged. A non-zero exit is `tx_failed`. The
     user has already seen gnokey's own message.
8. **Verify** the file gnokey wrote before using it:
   - Messages, fee and memo are identical to what was reviewed (compared as
     amino JSON).
   - There is exactly one signature.
   - Its public key's address is `signer`.
   - It verifies over the sign bytes for `(chainid, n, seq)`.

   This is cheap. It guarantees gnokey-pair never returns or broadcasts
   something other than what the user approved, whatever gnokey version is
   installed.
9. **Finish.**
   - `signtx`: answer with `signedtx`.
   - `sendtx`: simulate the signed transaction, then broadcast it (`commit`) and
     answer with `hash` and `height`. A failed simulation is `tx_failed` with
     nothing broadcast. Only signing is delegated to gnokey: simulation and
     broadcast use no key, and doing them in-process gives structured errors
     instead of parsed CLI output.
10. **Close** the channel once the phone sends `received`, or after `-linger`.
    One request per run.

### Signing offline (local network)

For a computer on the same local network as the phone but without internet
access. The phone asks for a signature only, and broadcasts it itself.

- **The phone decides.** It sends `mode: "signtx"` with `offline: true` and
  `account`. gnokey-pair then makes no network operation at all, even with
  `-remote` or a node for the chain: the phone asked for none, and keeping to
  that makes the behaviour predictable. Nothing on the computer turns this on,
  and gnokey-pair never falls back to it on its own: a node that cannot be
  reached stays `network_declined` for a request without `offline`.
- **The relay is on the local network**: the reference mailbox server, run on
  the computer or another machine there (README, "Running your own relay"),
  and `gnokey-pair -relay ws://<lan address>:4000/v1`. The QR carries the relay
  (`gnopair:<code>?relay=…`), so a scanned code needs no setting on the phone;
  a typed code needs the phone's relay setting.
- **What changes in the pipeline:**
  - Validate (step 2): `offline` with a mode other than `signtx`, or without
    `account`, is `invalid_request`.
  - Network (step 3): skipped. `chainid` comes from the request, unchecked;
    it is what the signature binds, and the review shows it.
  - Account (step 5): the number and sequence come from `account`. No
    balance, no gas price, no simulation, no session list, no `qdoc`
    parameter names.
  - Finish (step 9): `signedtx`, as for any `signtx`.
- **The review says the phone asked for it, and what was not checked**:

  ```
  Offline     your phone asked to sign without checking against the chain
  Network     gnoland-1   (not checked)
  Account     number 12, sequence 5, from the phone
  Fee         0.0021 GNOT (gas 2,000,000); balance unknown
  Simulation  not run
  ```

  A revocation shows the revoked key with "not shown offline: no node to read
  the sessions from"; a call shows positional arguments.
- **Why the phone may ask for fewer checks.** It is the user's own wallet,
  authenticated by the code and the check words; a GnoConnect producer cannot
  set the marker, which the wallet adds itself. The review says what was not
  checked, and the user can decline. A wrong account number or sequence only
  makes a signature the chain rejects when the phone broadcasts; neither can
  change what is signed, which the user reviewed.

### Signing by hand

Off by default. With `-manual` (or the app's setting), approving the review
gives the user the gnokey command instead of running gnokey: for a user who
wants to run gnokey themselves, with their own flags, or who does not want
gnokey-pair near their password. The command does what gnokey-pair would
otherwise do itself. In the CLI the review then ends with `Approve and print
the gnokey command? [y/N]` instead of `Sign? [y/N]`.

- **`sendtx` with a node**: sign, dry-run and broadcast.

  ```
  gnokey sign -tx-path <file> -chainid gnoland-1 -account-number 12 \
    -account-sequence 5 [-home <dir>] <signer address> \
    && gnokey broadcast -dry-run -remote <rpc> <file> \
    && gnokey broadcast -remote <rpc> <file>
  ```

  gnokey-pair answers `manual: true` at once and closes the channel; the
  phone watches the chain. The check after signing (step 8) is not run: the
  user runs their own gnokey on the file gnokey-pair wrote, and broadcasts
  it themselves.
- **`signtx`, `offline` or not**: `gnokey sign` alone. gnokey-pair waits for
  the file to come back signed (it checks it every half second, up to
  `-timeout`), verifies it as in step 8, and answers with `signedtx`.

The file is the one step 7 writes (`0700` directory, `0600` file). With the
full command, gnokey-pair cannot know when the user is done with it: the CLI
waits for Enter before removing it, the app removes it on [Done]. The command
is printed with every value shell-quoted; nothing from the request goes into
it except as a quoted argument.

### Review

```
gnokey-pair: request from your phone
Check words:  guitarist  revenge      (compare with the phone; stop if they differ)

Requester   "Gnokey Mobile" for "game.example"   (claimed, not verified)
Network     gnoland-1 via https://rpc.gno.land:443
Signer      g1abc…xyz  "main" (Ledger: confirm on the device)
Action      sign and broadcast

1. Create session  gpub1… (g1def…)
   Allowed      vm/exec:gno.land/r/demo/game
   Budget       5 GNOT per hour, at most 3,600 GNOT over 30 days
   Expires      2026-11-04 14:00 UTC (in 30 days)
2. Revoke session  gpub1… (g1ghi…)
   Allowed      vm/exec:gno.land/r/demo/game
   Expires      2026-09-30 12:00 UTC (expired)

Fee         0.0021 GNOT (gas 2,000,000), balance 152.3 GNOT
Simulation  ok, 1,412,330 gas

Sign? [y/N]
```

What the review must show:

- **Everything is rendered from the decoded transaction.** Nothing comes from
  the requester's description, which can only label (`requester`, shown as
  claimed).
- **Per message type:**
  - `vm.MsgCall`: realm, function, arguments labeled with parameter names from
    `vm/qdoc` when the lookup works (never reordered; a failed lookup does not
    block, as in GnoConnect's positional rule), and coins sent.
  - `bank.MsgSend` / `MsgMultiSend`: recipients and amounts.
  - `auth.MsgCreateSession`: the session review from GnoConnect's Review
    section: allow entries with the same warnings, the budget in plain terms and
    worst case, and the absolute expiry.
  - `MsgRevokeSession` / `MsgRevokeAllSessions`: what is revoked, read from
    the chain: each session's allow entries and expiry. A session that is not
    on chain is flagged. If the node does not list the sessions, the review
    says so and does not block.
- **Fee checks.** The fee is the requester's choice and comes out of the
  identity's balance. gnokey-pair shows it next to the balance and warns when it
  exceeds 10× `auth/gasprice` × gas wanted.
- **Terminal hygiene.** Every string from the request or the chain is printed
  with control characters escaped. This includes arguments, memo, `requester`,
  realm paths and `detail`. An argument carrying ANSI escapes could otherwise
  redraw the review the user is reading.

## gnokey-mobile changes

- **Handoff screen.** The app keeps the command and its QR as they are (the
  fallback when gnokey-pair is not installed) and adds **"Send to my computer"**:
  scan gnokey-pair's QR, or type the code.
- **Sending.** The app sends `kind: tx` with the document `BuildSessionTx`
  already produces. It sends `signer` (the identity), not the master key name.
  `mode` is `sendtx`.
- **While waiting.** The app shows the check words and "Review and approve on
  your computer". On `result` it sends `received` and shows the outcome. As
  today, it moves on only once the chain shows it.
- **Versions.** If gnokey-pair does not list `tx` / `sendtx`, the app falls back
  to the command.
- **Offline signing.** A switch on the "Send to my computer" card, remembered:
  "My computer has no internet: it only signs, this phone broadcasts". On, the
  app sends `signtx` with `offline: true` and `account` (it reads the account
  number and sequence before sending, for its own session create and revoke
  too), provided gnokey-pair lists the `offline` feature; otherwise it says the
  computer needs a newer gnokey-pair. On `success` with `signedtx`, it
  broadcasts the signed bytes unmodified through its node, shows "Signed by
  your computer, broadcast by this phone", and watches the chain as usual. A
  broadcast that fails shows the node's error.
- **Signing by hand.** On `success` with `manual: true`, the app shows "Your
  computer gives you the gnokey command; run it there" and watches the chain.
- **Local relay.** A scanned QR names the relay; a `ws://` address on the local
  network needs, on iOS, the local network permission
  (`NSLocalNetworkUsageDescription` in `Info.plist`): without it, connections to
  local addresses fail. Android's cleartext policy applies to the platform's
  HTTP stacks, not to the Go sockets the bridge uses; to confirm on a device.
- **Implementation.** The `wormhole` package is Go, so it lives in `core/` behind
  the existing gomobile bridge. The Swift and Kotlin sides add the screen and a
  QR scanner. The app only generates QR codes today (ZXing's `QRCodeWriter` on
  Android), so scanning is new: VisionKit's `DataScannerViewController` on iOS,
  and on Android ZXing or ML Kit through CameraX.

## Phase 2: GnoConnect intents and dapps

The same channel and envelope take a second kind, `request`. It carries a
GnoConnect launch link minus `callback`: `sendtx` / `signtx` / `connect` /
`disconnect` with their parameters and grammar unchanged, including proofs of
possession, which cover the host and every parameter. gnokey-pair then composes
the transaction itself, under the same rules as a launch-link wallet:

- named arguments through `vm/qdoc`, failing on lookup failure;
- the `signer` pin;
- session grants, `old`, and housekeeping.

It answers with the callback vocabulary as a `result`.

The difference from a launch link is identity. There is no callback host to
show, so the requester has no anchor at all. Its claimed name is shown as
claimed, and the check words and the review are the whole defense. This is a
third GnoConnect transport. It gets its own section in `gnoconnect.md`
(Callback → channel, identity anchor, payload size limit instead of the URL
limit).

Browser requesters additionally need:

- a JS (or WASM) client of the generic protocol;
- the `wss://` relay, since `https:` pages cannot open `ws:`;
- a way for the user to type the code: a desktop page has no camera to scan the
  terminal QR.

Most dapps should not need any of this: they reach gnokey-mobile with a launch
link or in-page, and gnokey-mobile forwards what it cannot sign through this
channel.

## Alternatives considered

- **Modify gnokey** (`gnokey sign --from-phone`): excluded by the constraint.
  It would also put a network listener inside the key tool.
- **gnokey-pair signs in-process** with the tm2 keybase library: the user would type
  their password into a new program, and Ledger support would be
  re-implemented. Delegating to the gnokey binary keeps one program in charge
  of keys.
- **WalletConnect.** Its pairing QR is shown by the requester, the phone here,
  and scanned by the signer, a camera-less terminal: the direction this design
  exists to fix. It also needs a wallet-side implementation in Go that has no
  official SDK, and a hosted relay with a project ID.
- **Stock `wormhole receive` + paste the command.** Nothing new to install
  beyond magic-wormhole, but it keeps the weak review, the staleness and
  executing pasted shell text. It remains a fallback anyone can use.
- **Desktop web page instead of a CLI.** Better review than pasting, but the
  sign step is still a pasted command, and it needs the `wss://` relay on day
  one.
- **LAN-direct (QR with IP and key, no relay).** No infrastructure, but fails
  on client-isolated, corporate and hotel networks, and across devices on
  different networks.
- **Ledger over Bluetooth from the phone.** Complementary, not a replacement:
  it removes the desktop for BLE Ledgers, and does nothing for keyfiles or
  USB-only devices.

## Risks

- **`gnokey list` output is not an API.** If its format changes, the signer
  pre-check stops matching. gnokey-pair degrades to the sign attempt (step 4).
  Mitigation: a test pinned against the gnokey version the gnokey-pair module
  requires.
- **The generic wormhole layer is new code.** SPAKE2, HKDF, secretbox and phase
  handling are written for this. Mitigation: interoperability tests against
  Python `magic-wormhole`, and no deviation from its key schedule.
- **Our own mailbox client.** `wormhole-william`'s client cannot reattach (see
  Library), so the `wormhole` package carries its own. It is small, and the
  reference server is the arbiter. The spike already found one behaviour that
  only shows against it: closing without releasing the nameplate breaks the
  server. Mitigation: CI runs the package against
  `magic-wormhole-mailbox-server` and the Python client, and the version of
  each is pinned.
- **Mailbox lifetime is the server's constant.** The 11 minutes come from the
  reference server and cannot be configured. A phone away for longer than
  `-linger` plus about 11 minutes misses the result and falls back to the
  chain. If that turns out too short in practice, the gno relay needs a patched
  or different server.
- **The relay is infrastructure** someone must run. Without it, the public
  server works for v1, minus metadata privacy and browser reach.
- **Absolute expiry set by the phone.** If the user takes hours to reach the
  computer, a session's `ExpiresAt` is shorter than granted. The review shows
  the absolute date. A request whose expiry is already past is `invalid_request`
  before review.
- **Signing offline checks less, at the phone's request.** No balance,
  simulation or session details; the chain id and the account come from the
  phone. Mitigation: only when the request says `offline`, never as a
  fallback; the review says the phone asked for it and lists what was not
  checked. What is signed is still exactly what was reviewed.
- **Signing by hand skips the check after signing** with the full command,
  since the user's gnokey writes and broadcasts. That is the user's choice; the
  check still runs when gnokey-pair takes the signed file back.

## Testing

- **Channel.** Against `magic-wormhole-mailbox-server` in CI:
  - Go to Go: success; a wrong code (both sides see the failure); timeout; a
    third peer refused.
  - Go to the Python client: the verifier, versions and application messages
    match in both directions.
  - The four spike cases, kept as tests: interop with Python, wrong code,
    reattach after gnokey-pair exited, `wss://` through a CONNECT proxy.
- **Reachability.**
  - Drop the phone peer's socket before the request, during the review, and
    after `result` is posted. Each time it reconnects, reattaches, processes no
    phase twice, and receives the result even when gnokey-pair has already
    exited.
  - Drop it for longer than `-linger` plus the server's expiry (shortened by
    patching the constant in the test server): the phone reports the channel
    lost and falls back to watching the chain.
  - Close gnokey-pair without releasing the nameplate: the test documents the
    server failure the release rule exists for.
  - Dial through an HTTP CONNECT proxy set in `HTTPS_PROXY`.
- **Validation.** One case per rule in step 2, plus the size bound and an
  already signed transaction.
- **Review rendering.** Golden files per message type, including control
  characters in every request string.
- **End to end.** An in-memory gno.land node from `gno.land/pkg/integration`,
  the real `gnokey` built from the required module, and a keyfile key with the
  password fed through a pty. Cover `sendtx` creating a session, revoking it
  with the review showing the session read from the chain, `signtx` returning
  bytes that broadcast as-is, a dry-run failure (nothing broadcast), and a
  stale sequence: the identity sends something after the phone composed the
  request, and gnokey-pair re-reads the sequence, so it succeeds.
- **Verify step.** A fake `gnokey` that alters the fee, adds a second
  signature, or signs with another key. Each must be caught before anything
  leaves gnokey-pair.
- **Offline.** `signtx` with `offline` and `account` returns `signedtx` that
  broadcasts as-is, with no request to the node even when one is configured
  (a node that records calls); `offline` with `sendtx`, or without `account`,
  is `invalid_request`; without `offline`, an unreachable node stays
  `network_declined`; the `offline` feature in gnokey-pair's versions; the
  review's offline lines (golden file).
- **By hand.** The printed command, shell-quoted, signs and broadcasts against
  the in-memory node; `manual: true` is answered; for `signtx`, gnokey-pair
  picks up the file the user signed, verifies it (an altered one is refused),
  and returns it; the timeout while waiting.

## Open questions

1. **More than one request per channel** (pairing once for a whole session's
   renewals). Deferred until there is demand: one request per code keeps
   gnokey-pair stateless.
