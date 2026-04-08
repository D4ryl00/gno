#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-01-five-validators-reset-four"
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
stop_validator val3
stop_validator val4
stop_validator val5

reset_validator val2
reset_validator val3
reset_validator val4
reset_validator val5

start_validator val2
start_validator val3
start_validator val4
start_validator val5
assert_chain_advances val1 120 2

print_cluster_status
