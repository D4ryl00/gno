package analyze

import (
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
	"github.com/stretchr/testify/require"
)

func TestBuildReportFindsMissingCommitBlock(t *testing.T) {
	now := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	report := BuildReport(Input{
		Genesis: model.Genesis{
			Path:         "/tmp/genesis.json",
			ChainID:      "test5",
			ValidatorNum: 1,
		},
		Sources: []model.Source{
			{Path: "/tmp/validator.log", Node: "validator_1", Role: model.RoleValidator},
		},
		Events: []model.Event{
			{
				Timestamp:    now,
				HasTimestamp: true,
				Node:         "validator_1",
				Role:         model.RoleValidator,
				Path:         "/tmp/validator.log",
				Line:         10,
				Message:      "Attempt to finalize failed. We don't have the commit block.",
				Kind:         model.EventCommitBlockMissing,
			},
		},
	})

	require.True(t, report.CriticalIssuesDetected)
	require.NotEmpty(t, report.Findings)
	require.Equal(t, model.SeverityCritical, report.Findings[0].Severity)
}
