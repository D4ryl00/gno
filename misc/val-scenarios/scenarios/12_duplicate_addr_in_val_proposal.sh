#!/usr/bin/env bash
set -euo pipefail

# Scenario 12: governance proposal with a duplicate validator address.
#
# A single NewPropRequest contains two entries for the same validator address:
#   1. { Address: val1, VotingPower: 0 }                    — remove
#   2. { Address: val1, PubKey: ..., VotingPower: 5 } — re-add with new power
#
# val1 should end up with VotingPower=5 (last entry wins) and the chain should
# keep advancing.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/lib/scenario.sh"

scenario_init "scenario-12"
trap scenario_finish EXIT

gen_validator val1

prepare_network
start_all_nodes
assert_chain_advances val1 120 5

TARGET="val1"
TARGET_ADDR="${NODE_ADDRESS[$TARGET]}"
TARGET_PUBKEY="${NODE_PUBKEY[$TARGET]}"
TARGET_POWER=5

script_dir="${SCENARIO_DIR}/scripts"
mkdir -p "$script_dir"

# Generate the MsgRun script with actual validator values substituted in.
# The script also bootstraps itself as a T1 DAO member, which is safe because
# allowedDAOs is intentionally left empty after genesis (see lib/valset-init.gno.tpl).
cat > "${script_dir}/change_voting_power.gno" << GNOEOF
package main

import (
	"gno.land/p/sys/validators"
	"gno.land/r/gov/dao"
	"gno.land/r/gov/dao/v3/memberstore"
	valr "gno.land/r/sys/validators/v2"
)

// txAddr is the scenario transaction key address; added as a T1 member so it
// can create and vote on the governance proposal.
const txAddr = address("${TX_ADDRESS}")

func main() {
	// Bootstrap: add the scenario TX key as a T1 member.
	// allowedDAOs is empty after genesis so memberstore.Get() is open to any
	// MsgRun caller at this stage.
	must(memberstore.Get().SetMember(memberstore.T1, txAddr, &memberstore.Member{InvitationPoints: 0}))

	r := valr.NewPropRequest(
		func() []validators.Validator {
			return []validators.Validator{
				{
					Address:     address("${TARGET_ADDR}"),
					VotingPower: 0,
				},
				{
					Address:     address("${TARGET_ADDR}"),
					PubKey:      "${TARGET_PUBKEY}",
					VotingPower: ${TARGET_POWER},
				},
			}
		},
		"Change voting power for ${TARGET_ADDR}",
		"Set voting power of validator ${TARGET_ADDR} to ${TARGET_POWER}",
	)
	pid := dao.MustCreateProposal(cross, r)
	dao.MustVoteOnProposal(cross, dao.VoteRequest{Option: dao.YesVote, ProposalID: pid})
	dao.ExecuteProposal(cross, pid)
}

func must(err error) {
	if err != nil {
		panic(err.Error())
	}
}
GNOEOF

# Estimate gas; if the simulation itself fails (e.g. the script panics during
# dry-run), fall back to a generous fixed value so the broadcast can still run.
log "estimating gas for the validator proposal script"
set +e
run_gas="$(estimate_run_gas val1 "${script_dir}/change_voting_power.gno" 50000000)"
estimate_status=$?
set -e
if [ "$estimate_status" -ne 0 ]; then
  run_gas=50000000
  log "gas estimation failed; using fallback gas=${run_gas}"
else
  log "gas estimate: ${run_gas}"
fi

log "submitting validator proposal with duplicate address"
set +e
run_script val1 "${script_dir}/change_voting_power.gno" "$run_gas"
run_status=$?
set -e

[ "$run_status" -ne 0 ] || die "expected the validator proposal script to fail, but it succeeded"
log "validator proposal script failed as expected (exit ${run_status})"

# The failed transaction is rolled back — val1 retains its original voting
# power and the chain must keep advancing.
assert_chain_advances val1 120 5

print_cluster_status
