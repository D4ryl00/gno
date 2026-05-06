#!/usr/bin/env bash
set -euo pipefail

SCENARIO_CI=false

# 1 ledger-backed validator + 3 gnokey-backed validators for quorum.
# The ledger-backed validator routes signing through gnokms running on the
# host (CGO + USB), while the others use the in-docker gnokms gnokey backend.
# Run for ~TARGET_BLOCKS blocks and print per-phase Sign() latency on each
# validator. Compare val1 against val2-4 to see the gnokms+ledger overlay.
#
# Prerequisites:
#   - macOS or Linux host with the Tendermint validator app installed on a
#     Ledger device.
#   - gnokms-ledger binary at $GNOKMS_LEDGER_BIN (default /tmp/gnokms-ledger).
#     Build it with: make build-gnokms-ledger
#   - Ledger Live (or any other client claiming the device) must be quit.
#
# Chain: validator -> valsignerd (metrics) -> host gnokms (ledger) -> Ledger

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-20"
trap scenario_finish EXIT

TARGET_BLOCKS="${TARGET_BLOCKS:-100}"

gen_validator val1 --ledger-backed-signer
gen_validator val2 --gnokms-backed-signer
gen_validator val3 --gnokms-backed-signer
gen_validator val4 --gnokms-backed-signer

prepare_network
start_all_nodes

assert_chain_advances val1 120 2

log "measuring sign latency over ${TARGET_BLOCKS} blocks"
wait_for_blocks val1 "$TARGET_BLOCKS" $((TARGET_BLOCKS * 3 + 60))

print_cluster_status
print_all_signer_metrics
