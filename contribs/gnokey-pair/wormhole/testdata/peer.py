# Reference-client peer for the interop tests.
#   peer.py <relay> allocate   prints "code <code>", then waits for the peer
#   peer.py <relay> <code>     joins the code
# Then prints the verifier and the peer's versions, sends one message, prints
# the one it receives, and closes.
import json
import sys

import wormhole
from twisted.internet import defer
from twisted.internet.task import react

APPID = "gno.land/gnokey-pair/v1"


@defer.inlineCallbacks
def main(reactor, relay, code):
    w = wormhole.create(APPID, relay, reactor,
                        versions={"gnokey-pair": {"v": 1, "kinds": ["tx"]}})
    if code == "allocate":
        w.allocate_code(2)
        print("code", (yield w.get_code()), flush=True)
    else:
        w.set_code(code)
    verifier = yield w.get_verifier()
    print("verifier", verifier.hex(), flush=True)
    print("versions", json.dumps((yield w.get_versions())), flush=True)
    w.send_message(b"from python")
    print("received", (yield w.get_message()).decode(), flush=True)
    yield w.close()


react(main, sys.argv[1:])
