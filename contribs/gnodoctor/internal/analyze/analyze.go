package analyze

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
)

type Input struct {
	Genesis  model.Genesis
	Sources  []model.Source
	Events   []model.Event
	Warnings []string
	Strict   bool
	Verbose  bool
}

func BuildReport(input Input) model.Report {
	nodes := buildNodeSummaries(input.Sources, input.Events)
	findings := buildFindings(input.Genesis, nodes, input.Events, input.Warnings)

	start, end := timeBounds(input.Events)
	report := model.Report{
		Input: model.InputSummary{
			GenesisPath:     input.Genesis.Path,
			ChainID:         input.Genesis.ChainID,
			GenesisTime:     formatMaybeTime(input.Genesis.GenesisTime),
			ValidatorCount:  input.Genesis.ValidatorNum,
			LogFileCount:    len(input.Sources),
			NodeCount:       len(nodes),
			TimeWindowStart: formatMaybeTime(start),
			TimeWindowEnd:   formatMaybeTime(end),
			Strict:          input.Strict,
		},
		Nodes:    nodes,
		Findings: findings,
		Warnings: append([]string(nil), input.Warnings...),
	}

	for _, finding := range findings {
		if finding.Severity == model.SeverityCritical {
			report.CriticalIssuesDetected = true
			break
		}
	}

	report.ConfidenceTooLow = len(findings) == 0 || allFindingsLowConfidence(findings)

	return report
}

func buildNodeSummaries(sources []model.Source, events []model.Event) []model.NodeSummary {
	summaries := map[string]*model.NodeSummary{}

	for _, source := range sources {
		if _, ok := summaries[source.Node]; !ok {
			summaries[source.Node] = &model.NodeSummary{
				Name:  source.Node,
				Role:  source.Role,
				Files: []string{},
			}
		}
		summaries[source.Node].Files = append(summaries[source.Node].Files, source.Path)
		if summaries[source.Node].Role == model.RoleUnknown {
			summaries[source.Node].Role = source.Role
		}
	}

	for _, event := range events {
		summary := summaries[event.Node]
		if summary == nil {
			summary = &model.NodeSummary{Name: event.Node, Role: event.Role}
			summaries[event.Node] = summary
		}
		summary.EventCount++
		if event.HasTimestamp {
			if summary.Start.IsZero() || event.Timestamp.Before(summary.Start) {
				summary.Start = event.Timestamp
			}
			if summary.End.IsZero() || event.Timestamp.After(summary.End) {
				summary.End = event.Timestamp
			}
		}

		switch event.Kind {
		case model.EventFinalizeCommit:
			summary.CommitCount++
			if event.Height > summary.HighestCommit {
				summary.HighestCommit = event.Height
			}
		case model.EventTimeout:
			summary.TimeoutCount++
		case model.EventAddedPeer:
			summary.CurrentPeers++
			if summary.CurrentPeers > summary.MaxPeers {
				summary.MaxPeers = summary.CurrentPeers
			}
		case model.EventStoppedPeer:
			if summary.CurrentPeers > 0 {
				summary.CurrentPeers--
			}
		case model.EventParserWarning:
			summary.ParserWarnings++
		}
	}

	list := make([]model.NodeSummary, 0, len(summaries))
	for _, summary := range summaries {
		sort.Strings(summary.Files)
		list = append(list, *summary)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

func buildFindings(genesis model.Genesis, nodes []model.NodeSummary, events []model.Event, warnings []string) []model.Finding {
	findings := make([]model.Finding, 0)

	if len(warnings) > 0 {
		findings = append(findings, model.Finding{
			ID:         "parser-warnings",
			Title:      "Parser warnings present",
			Severity:   model.SeverityLow,
			Confidence: model.ConfidenceMedium,
			Scope:      "global",
			Summary:    fmt.Sprintf("%d log lines were only partially classified", len(warnings)),
			Evidence:   evidenceFromWarnings(warnings),
		})
	}

	maxCommit := int64(0)
	for _, node := range nodes {
		if node.HighestCommit > maxCommit {
			maxCommit = node.HighestCommit
		}

		if node.Role == model.RoleValidator && node.CommitCount == 0 && node.TimeoutCount > 0 {
			findings = append(findings, model.Finding{
				ID:         "validator-no-first-commit-" + node.Name,
				Title:      fmt.Sprintf("%s never finalized a block in the observed window", node.Name),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceMedium,
				Scope:      node.Name,
				Summary:    "Timeouts were observed but no block commit was finalized.",
				PossibleCauses: []string{
					"insufficient quorum",
					"peer isolation",
					"proposal propagation failure",
				},
			})
		}

		if node.MaxPeers > 0 && node.CurrentPeers == 0 && node.TimeoutCount > 0 {
			findings = append(findings, model.Finding{
				ID:         "peer-starvation-" + node.Name,
				Title:      fmt.Sprintf("Peer starvation on %s", node.Name),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceHigh,
				Scope:      node.Name,
				Summary:    "The node dropped to zero peers and kept timing out.",
				PossibleCauses: []string{
					"unstable peer connectivity",
					"persistent peer misconfiguration",
					"network partition",
				},
			})
		}
	}

	if maxCommit > 0 {
		findings = append(findings, model.Finding{
			ID:         "forward-progress",
			Title:      fmt.Sprintf("Observed forward progress until height %d", maxCommit),
			Severity:   model.SeverityInfo,
			Confidence: model.ConfidenceHigh,
			Scope:      "global",
			Summary:    "At least one node finalized blocks in the observed window.",
		})
	}

	grouped := groupEventsByNode(events)
	for node, nodeEvents := range grouped {
		if count := countByKind(nodeEvents, model.EventCommitBlockMissing); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "missing-commit-block-" + node,
				Title:      fmt.Sprintf("%s failed to finalize because the commit block was missing locally", node),
				Severity:   model.SeverityCritical,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "The node reached commit processing but did not have the block required for finalization.",
				Evidence:   firstEvidence(nodeEvents, model.EventCommitBlockMissing, 3),
				PossibleCauses: []string{
					"proposal block parts were not fully received",
					"reactor propagation failure",
				},
				SuggestedActions: []string{
					"inspect reactor and peer logs around the same height",
					"compare with sentry logs for missing block-part propagation",
				},
			})
		}

		if count := countByKind(nodeEvents, model.EventPrevoteProposalNil); count >= 3 {
			findings = append(findings, model.Finding{
				ID:         "proposal-block-nil-" + node,
				Title:      fmt.Sprintf("%s repeatedly prevoted nil because no proposal block was available", node),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "Repeated nil prevotes indicate missing or incomplete proposal block reception.",
				Evidence:   firstEvidence(nodeEvents, model.EventPrevoteProposalNil, 3),
				PossibleCauses: []string{
					"proposal propagation failure",
					"peer starvation",
				},
			})
		}

		if count := countByKind(nodeEvents, model.EventPrecommitNoMaj23); count >= 3 {
			findings = append(findings, model.Finding{
				ID:         "no-maj23-" + node,
				Title:      fmt.Sprintf("%s repeatedly precommitted nil because +2/3 prevotes were missing", node),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "Consensus rounds advanced without enough prevotes to lock or commit a block.",
				Evidence:   firstEvidence(nodeEvents, model.EventPrecommitNoMaj23, 3),
				PossibleCauses: []string{
					"quorum failure",
					"network partition",
					"validator non-participation",
				},
			})
		}

		if count := countByKind(nodeEvents, model.EventConsensusFailure); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "consensus-panic-" + node,
				Title:      fmt.Sprintf("Consensus panic on %s", node),
				Severity:   model.SeverityCritical,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "A consensus failure panic was logged.",
				Evidence:   firstEvidence(nodeEvents, model.EventConsensusFailure, 1),
			})
		}

		if count := countByKind(nodeEvents, model.EventNodeNotValidator); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "node-not-validator-" + node,
				Title:      fmt.Sprintf("%s reported that it is not a validator", node),
				Severity:   model.SeverityMedium,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "This log source may not correspond to an active validator.",
				Evidence:   firstEvidence(nodeEvents, model.EventNodeNotValidator, 2),
			})
		}

		if count := countByKind(nodeEvents, model.EventRemoteSignerFailure); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "remote-signer-failure-" + node,
				Title:      fmt.Sprintf("Remote signer failures on %s", node),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceMedium,
				Scope:      node,
				Summary:    "Signing requests failed in the observed window.",
				Evidence:   firstEvidence(nodeEvents, model.EventRemoteSignerFailure, 2),
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if severityRank(findings[i].Severity) == severityRank(findings[j].Severity) {
			return findings[i].Title < findings[j].Title
		}
		return severityRank(findings[i].Severity) > severityRank(findings[j].Severity)
	})

	return findings
}

func groupEventsByNode(events []model.Event) map[string][]model.Event {
	grouped := make(map[string][]model.Event)
	for _, event := range events {
		grouped[event.Node] = append(grouped[event.Node], event)
	}
	return grouped
}

func countByKind(events []model.Event, kind model.EventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func firstEvidence(events []model.Event, kind model.EventKind, limit int) []model.Evidence {
	out := make([]model.Evidence, 0, limit)
	for _, event := range events {
		if event.Kind != kind {
			continue
		}
		out = append(out, model.Evidence{
			Node:      event.Node,
			Timestamp: formatMaybeTime(event.Timestamp),
			Path:      event.Path,
			Line:      event.Line,
			Message:   event.Message,
		})
		if len(out) == limit {
			break
		}
	}
	return out
}

func evidenceFromWarnings(warnings []string) []model.Evidence {
	limit := 3
	if len(warnings) < limit {
		limit = len(warnings)
	}
	out := make([]model.Evidence, 0, limit)
	for _, warning := range warnings[:limit] {
		out = append(out, model.Evidence{Message: warning})
	}
	return out
}

func timeBounds(events []model.Event) (time.Time, time.Time) {
	var start time.Time
	var end time.Time
	for _, event := range events {
		if !event.HasTimestamp {
			continue
		}
		if start.IsZero() || event.Timestamp.Before(start) {
			start = event.Timestamp
		}
		if end.IsZero() || event.Timestamp.After(end) {
			end = event.Timestamp
		}
	}
	return start, end
}

func severityRank(severity model.Severity) int {
	switch severity {
	case model.SeverityCritical:
		return 5
	case model.SeverityHigh:
		return 4
	case model.SeverityMedium:
		return 3
	case model.SeverityLow:
		return 2
	default:
		return 1
	}
}

func allFindingsLowConfidence(findings []model.Finding) bool {
	if len(findings) == 0 {
		return true
	}
	for _, finding := range findings {
		if finding.Confidence != model.ConfidenceLow {
			return false
		}
		if !strings.Contains(strings.ToLower(finding.Summary), "partially") {
			return false
		}
	}
	return true
}

func formatMaybeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}
