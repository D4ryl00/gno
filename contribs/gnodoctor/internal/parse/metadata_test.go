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

func TestBuildGeneratedMetadataDoesNotGuessWhenAmbiguous(t *testing.T) {
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
			{Path: "/tmp/validator-a.log", Node: "validator_a", Role: model.RoleValidator},
			{Path: "/tmp/validator-b.log", Node: "validator_b", Role: model.RoleValidator},
		},
	)

	require.Empty(t, meta.Nodes["validator_a"].ValidatorAddress)
	require.Empty(t, meta.Nodes["validator_b"].ValidatorAddress)
}

func TestMergeMetadataMergesNodeFields(t *testing.T) {
	merged := MergeMetadata(
		model.Metadata{
			Version: 1,
			Nodes: map[string]model.MetadataNode{
				"validator_1": {
					Role:  "validator",
					Files: []string{"/tmp/validator.log"},
				},
			},
		},
		model.Metadata{
			Version: 1,
			Nodes: map[string]model.MetadataNode{
				"validator_1": {
					ValidatorAddress: "g1example",
				},
			},
		},
	)

	require.Equal(t, "validator", merged.Nodes["validator_1"].Role)
	require.Equal(t, []string{"/tmp/validator.log"}, merged.Nodes["validator_1"].Files)
	require.Equal(t, "g1example", merged.Nodes["validator_1"].ValidatorAddress)
}
