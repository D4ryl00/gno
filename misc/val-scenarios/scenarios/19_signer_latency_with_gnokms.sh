#!/usr/bin/env bash
set -euo pipefail

SCENARIO_CI=false

# 4 validators where each controllable signer forwards Sign() requests to a
# colocated gnokms sidecar (filekey backend). The chain therefore goes:
#   validator -> valsignerd (metrics, gnokms backend) -> gnokms -> file key
# Run for ~100 blocks and print per-phase Sign() latency on each validator.
# Subtracting scenario 18's numbers from these gives the gnokms overlay cost
# (TCP roundtrip + gnokms processing).

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-19"
trap scenario_finish EXIT

TARGET_BLOCKS="${TARGET_BLOCKS:-100}"

gen_validator val1 --gnokms-backed-signer
gen_validator val2 --gnokms-backed-signer
gen_validator val3 --gnokms-backed-signer
gen_validator val4 --gnokms-backed-signer

prepare_network
start_all_nodes

assert_chain_advances val1 120 2

log "measuring sign latency over ${TARGET_BLOCKS} blocks (with gnokms overlay)"
wait_for_blocks val1 "$TARGET_BLOCKS" $((TARGET_BLOCKS * 3 + 60))

print_cluster_status
print_all_signer_metrics
