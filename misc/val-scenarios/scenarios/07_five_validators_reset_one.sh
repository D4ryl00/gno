#!/usr/bin/env bash
set -euo pipefail

# 5 validators, stop/reset/restart only 1.
# 4/5 remain during the reset (80% > 2/3 threshold) so the chain must keep
# advancing throughout the whole scenario.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-07"
trap scenario_finish EXIT

gen_validator val1
gen_validator val2
gen_validator val3
gen_validator val4
gen_validator val5

prepare_network
start_all_nodes
assert_chain_advances val1 120 5

stop_validator val2
reset_validator val2

# 4/5 validators still running — chain must keep advancing.
assert_chain_advances val1 60 2

start_validator val2

# val2 must first catch up to the current chain height via block sync, then
# actively produce new blocks (proving it re-joined consensus).
sync_target="$(node_height val1)"
wait_for_height val2 "$sync_target" 120
assert_chain_advances val2 60 2

print_cluster_status
