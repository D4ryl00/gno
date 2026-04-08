#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-05-sentry-ip-rotation"
trap scenario_finish EXIT

gen_validator val1
gen_validator val2
gen_validator val3
gen_sentry sentry1
gen_validator val4 --sentry sentry1
gen_validator val5 --sentry sentry1

prepare_network
start_all_nodes
assert_chain_advances val1 120 5

# When sentry1 is down val4 and val5 are isolated, leaving only 3/5 validators
# reachable — below the 2/3 threshold. The chain must halt.
_while_sentry_down() { assert_chain_halted val1 30; }

rotate_sentry_ip sentry1 _while_sentry_down

# Chain must resume once sentry1 is back and all 5 validators are connected.
assert_chain_advances val1 120 2

# val4 and val5 must reconnect through the sentry, catch up to the current
# chain height, and actively produce new blocks.
sync_target="$(node_height val1)"
wait_for_height val4 "$sync_target" 120
wait_for_height val5 "$sync_target" 120
assert_chain_advances val4 60 2
assert_chain_advances val5 60 2

print_cluster_status
