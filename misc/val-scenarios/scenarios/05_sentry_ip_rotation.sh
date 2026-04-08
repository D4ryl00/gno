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

rotate_sentry_ip sentry1
wait_for_blocks val4 5 120

print_cluster_status
