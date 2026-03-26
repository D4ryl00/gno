package analyze

import (
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/valdoctor/internal/model"
	"github.com/stretchr/testify/require"
)

// TestBuildReportMissingCommitBlockSingle verifies that a single occurrence of
// EventCommitBlockMissing is not flagged — it is transient during catch-up.
func TestBuildReportMissingCommitBlockSingle(t *testing.T) {
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

	require.False(t, report.CriticalIssuesDetected)
	for _, f := range report.Findings {
		require.NotEqual(t, "missing-commit-block-validator_1", f.ID)
	}
}

// TestBuildReportMissingCommitBlockRepeated verifies that three or more
// occurrences of EventCommitBlockMissing produce a high-severity finding.
func TestBuildReportMissingCommitBlockRepeated(t *testing.T) {
	now := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	events := make([]model.Event, 3)
	for i := range events {
		events[i] = model.Event{
			Timestamp:    now.Add(time.Duration(i) * time.Second),
			HasTimestamp: true,
			Node:         "validator_1",
			Role:         model.RoleValidator,
			Path:         "/tmp/validator.log",
			Line:         10 + i,
			Message:      "Attempt to finalize failed. We don't have the commit block.",
			Kind:         model.EventCommitBlockMissing,
		}
	}
	report := BuildReport(Input{
		Genesis: model.Genesis{
			Path:         "/tmp/genesis.json",
			ChainID:      "test5",
			ValidatorNum: 1,
		},
		Sources: []model.Source{
			{Path: "/tmp/validator.log", Node: "validator_1", Role: model.RoleValidator},
		},
		Events: events,
	})

	found := false
	for _, f := range report.Findings {
		if f.ID == "missing-commit-block-validator_1" {
			require.Equal(t, model.SeverityHigh, f.Severity)
			found = true
		}
	}
	require.True(t, found, "expected missing-commit-block finding")
}
