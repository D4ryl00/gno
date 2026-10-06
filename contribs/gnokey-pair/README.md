# gnokey-pair

Signs on your computer a transaction sent from your phone. Gnokey Mobile holds
only a session key; what only the identity can sign (creating or revoking
sessions, or any call you choose to sign with the identity) goes to the
desktop over an end-to-end encrypted magic-wormhole channel. gnokey-pair shows
its own review and has the unmodified `gnokey` sign.

Design: `docs/superpowers/specs/2026-10-05-gnokey-pair-design.md`.

## Install

gnokey-pair is not in a gno release yet: build it from the `feat/gnokey-pair`
branch. It runs the `gnokey` that holds your keys, so install that too if you
have not.

```sh
git clone --depth 1 --branch feat/gnokey-pair https://github.com/D4ryl00/gno.git
cd gno
make install.gnokey                  # skip if gnokey is already installed
make -C contribs/gnokey-pair install # gnokey-pair, into $(go env GOPATH)/bin
```

## Use

```sh
gnokey-pair -remote https://rpc.gno.land:443
```

It prints a code and a QR. In Gnokey Mobile, choose "Send to my computer" and
scan it, then check that both screens show the same two check words. Review the
request, type `y`, and gnokey asks for your password (or your Ledger). With
`sendtx` gnokey-pair broadcasts the transaction; with `signtx` it hands the
signed bytes back to the phone.

| flag | default | |
|---|---|---|
| `-remote` | ask | node for the request's chain; without it, the phone's suggestion is used only if you agree |
| `-relay` | `wss://gnokey-pair.berty.io/v1` | mailbox server, the same as the phone's (see [Running your own relay](#running-your-own-relay)) |
| `-gnokey` | `$PATH` | gnokey binary; its path and version are printed first |
| `-home` | gnokey's | passed to gnokey |
| `-timeout` | `10m` | wait for the phone |
| `-linger` | `10m` | after answering, wait for the phone to confirm it read the answer |

## Running your own relay

Both ends meet on the default relay. To use another one, such as
a private relay or one on your machine for development, run the reference
magic-wormhole mailbox server, at the version the tests pin:

```sh
python3 -m venv relay && relay/bin/pip install magic-wormhole-mailbox-server==0.8.0
relay/bin/twist wormhole-mailbox --port tcp:4000 --channel-db relay.sqlite
```

It serves `ws://<host>:4000/v1`; put it behind a TLS proxy for `wss://`. Then
start `gnokey-pair -relay ws://<host>:4000/v1`, and in Gnokey Mobile set
Settings → Send to my computer → Relay to the same URL. A scanned QR carries
the relay, so only a typed code needs the setting.

## Packages

- `wormhole`: the magic-wormhole channel, with reconnection; gnokey-mobile
  imports it too.
- `protocol`: the request and result messages, the QR's `gnopair:` URI and
  the default relay.
- `relaytest`: runs the reference mailbox server for tests, here and in
  requesters such as gnokey-mobile.

## Tests

The tests that need a relay run the Python reference mailbox server, pinned in
`relaytest/requirements.txt`: the `wormhole` tests and the end-to-end
test (an in-memory node, the real `gnokey`, a password typed through a pty).

```sh
make test-deps   # python3 -m venv .venv, then pip install the pinned versions
make test        # or make test-relay: with -race, and fails without the venv
```

Without the venv, those tests skip. The contribs CI matrix has no venv;
`.github/workflows/ci-gnokey-pair-relay.yml` runs them.
`WORMHOLE_RELAY_LOG=<file>` keeps the server's log. With `-v`, the end-to-end
test prints what it saw on the terminal.
