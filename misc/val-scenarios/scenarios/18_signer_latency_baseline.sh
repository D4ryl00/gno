#!/usr/bin/env bash
set -euo pipefail

SCENARIO_CI=false

# 4 validators with controllable signer sidecars (local backend).
# Run for ~100 blocks and print per-phase Sign() latency on each validator.
# This is the baseline measurement to compare against scenario 19, which puts
# gnokms in front of the same controllable signer.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-18"
trap scenario_finish EXIT

TARGET_BLOCKS="${TARGET_BLOCKS:-100}"

gen_validator val1 --controllable-signer
gen_validator val2 --controllable-signer
gen_validator val3 --controllable-signer
gen_validator val4 --controllable-signer

prepare_network
start_all_nodes

assert_chain_advances val1 120 2

log "measuring sign latency over ${TARGET_BLOCKS} blocks"
wait_for_blocks val1 "$TARGET_BLOCKS" $((TARGET_BLOCKS * 3 + 60))

print_cluster_status
print_all_signer_metrics
