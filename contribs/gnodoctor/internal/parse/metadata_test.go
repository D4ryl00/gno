package parse

import (
	"testing"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
	"github.com/stretchr/testify/require"
)

func TestBuildGeneratedMetadata(t *testing.T) {
	meta := BuildGeneratedMetadata(
		model.Genesis{
			ChainID:      "test5",
			ValidatorNum: 1,
			Validators: []model.Validator{
				{
					Name:    "validator-1",
					Address: "g1example",
					PubKey:  "gpub1example",
				},
			},
		},
		[]model.Source{
			{Path: "/tmp/validator.log", Node: "validator_1", Role: model.RoleValidator},
			{Path: "/tmp/sentry.log", Node: "sentry_a", Role: model.RoleSentry},
		},
	)

	require.Equal(t, 1, meta.Version)
	require.Equal(t, "test5", meta.ChainID)
	require.Contains(t, meta.Nodes, "validator_1")
	require.Equal(t, "validator", meta.Nodes["validator_1"].Role)
	require.Equal(t, "g1example", meta.Nodes["validator_1"].ValidatorAddress)
}
