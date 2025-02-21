import (
	"std"

	pVals "gno.land/p/sys/validators"
	validators "gno.land/r/sys/validators/v2"
  valopers "gno.land/r/gnoland/valopers"
	"gno.land/r/gov/dao/bridge"
)

// GovDAOProposal creates a proposal to the GovDAO
// for adding the given valoper to the validator set.
// This function is meant to serve as a helper
// for generating the govdao proposal
func GovDAOProposal(address std.Address) {
	var (
		valoper     = valopers.GetByAddr(address)
		votingPower = uint64(1)
	)

	// Make sure the valoper is the caller
	if std.GetOrigCaller() != address {
		panic(errValoperNotCaller)
	}

	// Determine the voting power
	if !valoper.Active {
		votingPower = uint64(0)
	}

	changesFn := func() []pVals.Validator {
		return []pVals.Validator{
			{
				Address:     valoper.Address,
				PubKey:      valoper.PubKey,
				VotingPower: votingPower,
			},
		}
	}

	// Create the executor
	executor := validators.NewPropExecutor(changesFn)

	// Craft the proposal description
	description := ufmt.Sprintf(
		"Add valoper %s (Address: %s; PubKey: %s) to the valset",
		valoper.Name,
		valoper.Address.String(),
		valoper.PubKey,
	)

	prop := dao.ProposalRequest{
		Description: description,
		Executor:    executor,
	}

	// Create the govdao proposal
	bridge.GovDAO().Propose(prop)
}
