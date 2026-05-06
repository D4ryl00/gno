#!/usr/bin/env bash
set -euo pipefail

SCENARIO_CI=false

# Single ledger-backed validator. The chain has no other validators, so every
# proposal/prevote/precommit goes through gnokms+ledger. The per-phase Sign()
# latency on val1 is then the unmixed gnokms+ledger cost (no peer waiting).
#
# Compare against scenario 18 (single signer, no gnokms) to get the full
# overlay; against scenario 19 (gnokms+gnokey) to isolate the ledger device
# cost.
#
# Prerequisites:
#   - Tendermint validator app installed on the Ledger device.
#   - gnokms-ledger binary at $GNOKMS_LEDGER_BIN (default /tmp/gnokms-ledger).
#     Build it once with: make build-gnokms-ledger
#   - Ledger Live (or anything else claiming the device) must be quit.
#
# Chain: validator -> valsignerd (metrics) -> host gnokms (ledger) -> Ledger

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-21"
trap scenario_finish EXIT

TARGET_BLOCKS="${TARGET_BLOCKS:-100}"

gen_validator val1 --ledger-backed-signer

prepare_network
start_all_nodes

assert_chain_advances val1 120 2

log "measuring sign latency over ${TARGET_BLOCKS} blocks"
wait_for_blocks val1 "$TARGET_BLOCKS" $((TARGET_BLOCKS * 3 + 60))

print_cluster_status
print_all_signer_metrics
