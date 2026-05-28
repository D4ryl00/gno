#!/usr/bin/env bash
set -euo pipefail

SCENARIO_CI=false

# Single validator with a controllable local signer. Set VALIDATOR_KEY_TYPE to
# ed25519 or secp256k1 and compare the per-phase Sign() latency output.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-23"
trap scenario_finish EXIT

TARGET_BLOCKS="${TARGET_BLOCKS:-100}"
VALIDATOR_KEY_TYPE="${VALIDATOR_KEY_TYPE:-ed25519}"

case "$VALIDATOR_KEY_TYPE" in
  ed25519|secp256k1) ;;
  *) die "unsupported VALIDATOR_KEY_TYPE=${VALIDATOR_KEY_TYPE}; expected ed25519 or secp256k1" ;;
esac

gen_validator val1 --controllable-signer --validator-key-type "$VALIDATOR_KEY_TYPE"

prepare_network
start_all_nodes

assert_chain_advances val1 120 2

log "measuring ${VALIDATOR_KEY_TYPE} sign latency over ${TARGET_BLOCKS} blocks"
wait_for_blocks val1 "$TARGET_BLOCKS" $((TARGET_BLOCKS * 3 + 60))

print_cluster_status
print_all_signer_metrics
