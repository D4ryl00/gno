#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-04-counter-realm-churn"
trap scenario_finish EXIT

gen_validator val1
gen_validator val2
gen_validator val3
gen_validator val4

prepare_network
start_all_nodes
wait_for_blocks val1 5 120

add_pkg val1 "${ROOT_DIR}/packages/scenario-counter" "gno.land/r/demo/scenario_counter"
wait_for_seconds 5

call_realm val1 "gno.land/r/demo/scenario_counter" "Increment"
call_realm val1 "gno.land/r/demo/scenario_counter" "Increment"
call_realm val1 "gno.land/r/demo/scenario_counter" "Increment"
wait_for_seconds 10

stop_validator val3
reset_validator val3
start_validator val3
wait_for_seconds 20

call_realm val1 "gno.land/r/demo/scenario_counter" "Increment"
query_render val1 "gno.land/r/demo/scenario_counter:"
print_cluster_status
