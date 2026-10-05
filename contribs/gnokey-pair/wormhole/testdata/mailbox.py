# Runs the reference mailbox server for the tests: mailbox.py <twist args>.
# MAILBOX_EXPIRE (seconds) shortens the pruning delay, a constant in the server.
import os
import sys

from twisted.application.twist._twist import Twist
from wormhole_mailbox_server import server_tap

if "MAILBOX_EXPIRE" in os.environ:
    server_tap.CHANNEL_EXPIRATION_TIME = float(os.environ["MAILBOX_EXPIRE"])
    server_tap.EXPIRATION_CHECK_PERIOD = server_tap.CHANNEL_EXPIRATION_TIME / 2

Twist.main(["twist", "wormhole-mailbox"] + sys.argv[1:])
