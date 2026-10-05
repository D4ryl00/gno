# gnokey-pair

Hands a transaction from a phone to desktop `gnokey` over a magic-wormhole
channel. Design: `docs/superpowers/specs/2026-10-05-gnokey-pair-design.md`.

The CLI is not written yet. This module currently holds the channel,
`wormhole`, which gnokey-mobile also imports.

## Tests

The `wormhole` tests run against the Python reference mailbox server and
client, pinned in `wormhole/testdata/requirements.txt`. Each test run starts
its own server.

```sh
make test-deps   # python3 -m venv .venv, then pip install the pinned versions
make test        # or make test-relay: with -race, and fails without the venv
```

Without the venv, the tests that need a relay skip. The contribs CI matrix
has no venv; `.github/workflows/ci-gnokey-pair-relay.yml` runs them.
`WORMHOLE_RELAY_LOG=<file>` keeps the server's log.
