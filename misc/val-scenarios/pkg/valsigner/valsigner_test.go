package valsigner

import (
	"testing"
	"time"

	"github.com/gnolang/gno/tm2/pkg/bft/types"
)

func TestClassifySignBytesProposal(t *testing.T) {
	t.Parallel()

	proposal := types.NewProposal(7, 2, -1, types.BlockID{
		Hash: []byte("blockhash"),
		PartsHeader: types.PartSetHeader{
			Total: 1,
			Hash:  []byte("partshash"),
		},
	})

	signBytes := proposal.SignBytes("dev")
	target, err := ClassifySignBytes(signBytes)
	if err != nil {
		t.Fatalf("ClassifySignBytes() error = %v", err)
	}

	if target.Phase != PhaseProposal || target.Height != 7 || target.Round != 2 {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestClassifySignBytesVote(t *testing.T) {
	t.Parallel()

	vote := &types.Vote{
		Type:      types.PrecommitType,
		Height:    11,
		Round:     3,
		Timestamp: time.Now().UTC(),
		BlockID: types.BlockID{
			Hash: []byte("blockhash"),
			PartsHeader: types.PartSetHeader{
				Total: 1,
				Hash:  []byte("partshash"),
			},
		},
	}

	signBytes := vote.SignBytes("dev")
	target, err := ClassifySignBytes(signBytes)
	if err != nil {
		t.Fatalf("ClassifySignBytes() error = %v", err)
	}

	if target.Phase != PhasePrecommit || target.Height != 11 || target.Round != 3 {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestRuleMatches(t *testing.T) {
	t.Parallel()

	height := int64(12)
	round := 1
	rule := Rule{
		Action: ActionDrop,
		Height: &height,
		Round:  &round,
	}

	if !rule.Matches(SignedTarget{Phase: PhasePrevote, Height: 12, Round: 1}) {
		t.Fatal("expected matching rule")
	}
	if rule.Matches(SignedTarget{Phase: PhasePrevote, Height: 12, Round: 2}) {
		t.Fatal("expected round mismatch")
	}
}

func TestRecordSignLatency(t *testing.T) {
	t.Parallel()

	c := NewController()
	c.RecordSignLatency(PhaseProposal, 10*time.Millisecond)
	c.RecordSignLatency(PhaseProposal, 30*time.Millisecond)
	c.RecordSignLatency(PhasePrevote, 5*time.Millisecond)
	// Unknown phase should be ignored.
	c.RecordSignLatency(Phase("unknown"), 100*time.Millisecond)

	_, stats := c.Snapshot()

	prop := stats[PhaseProposal]
	if prop.SignCount != 2 {
		t.Fatalf("proposal SignCount = %d, want 2", prop.SignCount)
	}
	if prop.TotalNs != int64((40 * time.Millisecond).Nanoseconds()) {
		t.Fatalf("proposal TotalNs = %d, want %d", prop.TotalNs, (40 * time.Millisecond).Nanoseconds())
	}
	if prop.MinNs != int64((10 * time.Millisecond).Nanoseconds()) {
		t.Fatalf("proposal MinNs = %d, want %d", prop.MinNs, (10 * time.Millisecond).Nanoseconds())
	}
	if prop.MaxNs != int64((30 * time.Millisecond).Nanoseconds()) {
		t.Fatalf("proposal MaxNs = %d, want %d", prop.MaxNs, (30 * time.Millisecond).Nanoseconds())
	}

	pre := stats[PhasePrevote]
	if pre.SignCount != 1 {
		t.Fatalf("prevote SignCount = %d, want 1", pre.SignCount)
	}

	if stats[PhasePrecommit].SignCount != 0 {
		t.Fatalf("precommit SignCount = %d, want 0", stats[PhasePrecommit].SignCount)
	}
}

func TestParseRuleRequest(t *testing.T) {
	t.Parallel()

	rule, err := ParseRuleRequest(ruleRequest{
		Action: ActionDelay,
		Delay:  "1500ms",
	})
	if err != nil {
		t.Fatalf("ParseRuleRequest() error = %v", err)
	}
	if rule.Delay != 1500*time.Millisecond {
		t.Fatalf("unexpected delay: %v", rule.Delay)
	}
}
