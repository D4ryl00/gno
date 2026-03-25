package analyze

import (
	"fmt"
	"sort"
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

	// ConfidenceTooLow: not enough classifiable events to draw conclusions.
	// Zero findings from good logs is a clean result (exit 0), not low confidence.
	totalClassified := 0
	for _, ev := range input.Events {
		if ev.Kind != model.EventUnknown && ev.Kind != model.EventParserWarning {
			totalClassified++
		}
	}
	report.ConfidenceTooLow = totalClassified == 0

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
			if len(summary.TimeoutSamples) < 3 {
				summary.TimeoutSamples = append(summary.TimeoutSamples, model.Evidence{
					Node:      event.Node,
					Timestamp: formatMaybeTime(event.Timestamp),
					Path:      event.Path,
					Line:      event.Line,
					Message:   event.Message,
				})
			}
		}

		updateLastConsensusState(summary, event)

		switch event.Kind {
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

	if genesis.ValidatorNum == 0 {
		findings = append(findings, model.Finding{
			ID:         "genesis-no-validators",
			Title:      "Genesis has no validators",
			Severity:   model.SeverityCritical,
			Confidence: model.ConfidenceHigh,
			Scope:      "global",
			Summary:    "The genesis file contains an empty validator set; the chain cannot produce blocks.",
		})
	}

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
				SuggestedActions: []string{
					"check persistent_peers in config.toml",
					"verify network connectivity to peer addresses",
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

	// Validator height divergence: emit when validators are at meaningfully
	// different heights at the end of the observed window.
	{
		type valPos struct {
			name   string
			height int64
		}
		var valPositions []valPos
		for _, node := range nodes {
			if node.Role == model.RoleValidator && node.LastHeight > 0 {
				valPositions = append(valPositions, valPos{node.Name, node.LastHeight})
			}
		}
		if len(valPositions) > 1 {
			minH, maxH := valPositions[0].height, valPositions[0].height
			for _, vp := range valPositions[1:] {
				if vp.height < minH {
					minH = vp.height
				}
				if vp.height > maxH {
					maxH = vp.height
				}
			}
			if gap := maxH - minH; gap > 0 {
				evidence := make([]model.Evidence, 0, len(valPositions))
				for _, vp := range valPositions {
					evidence = append(evidence, model.Evidence{
						Node:    vp.name,
						Message: fmt.Sprintf("last height: %d", vp.height),
					})
				}
				findings = append(findings, model.Finding{
					ID:         "validator-height-divergence",
					Title:      fmt.Sprintf("Validator height divergence (gap: %d)", gap),
					Severity:   model.SeverityHigh,
					Confidence: model.ConfidenceMedium,
					Scope:      "global",
					Summary:    fmt.Sprintf("Validators are at different heights at the end of the window (min=%d, max=%d).", minH, maxH),
					Evidence:   evidence,
					PossibleCauses: []string{
						"one validator crashed or was restarted mid-session",
						"network partition isolating some validators",
						"consensus stall on a subset of validators",
					},
				})
			}
		}
	}

	nodeRoles := make(map[string]model.Role, len(nodes))
	for _, n := range nodes {
		nodeRoles[n.Name] = n.Role
	}

	grouped := groupEventsByNode(events)
	for node, nodeEvents := range grouped {
		// Config errors — emitted before the first structured log line.
		if count := countByKind(nodeEvents, model.EventConfigError); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "config-error-" + node,
				Title:      fmt.Sprintf("Configuration error on %s", node),
				Severity:   model.SeverityMedium,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    fmt.Sprintf("%d unrecognized or invalid configuration field(s) detected at startup.", count),
				Evidence:   firstEvidence(nodeEvents, model.EventConfigError, 3),
				SuggestedActions: []string{
					"check config.toml for typos in field names",
					"compare against the reference config from `gnoland config init`",
				},
			})
		}

		// Consensus panic — node crashed; always critical.
		if count := countByKind(nodeEvents, model.EventConsensusFailure); count > 0 {
			ev := firstEvidence(nodeEvents, model.EventConsensusFailure, 1)
			// Attach the stack trace from the Fields if available.
			if len(ev) > 0 {
				for _, e := range nodeEvents {
					if e.Kind == model.EventConsensusFailure {
						if stack, ok := e.Fields["stack"].(string); ok && stack != "" {
							ev[0].Message = ev[0].Message + "\n  stack: " + stack
						}
						break
					}
				}
			}
			findings = append(findings, model.Finding{
				ID:         "consensus-panic-" + node,
				Title:      fmt.Sprintf("Consensus panic on %s", node),
				Severity:   model.SeverityCritical,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "A CONSENSUS FAILURE!!! panic was logged. The node process terminated.",
				Evidence:   ev,
				SuggestedActions: []string{
					"check the panic stack trace for the root cause",
					"restart the node after resolving the underlying issue",
					"file a bug report if the panic message is `not yet implemented`",
				},
			})
		}

		// Conflicting vote from self — possible double-signing or unsafe reset.
		if count := countByKind(nodeEvents, model.EventConflictingVote); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "conflicting-vote-" + node,
				Title:      fmt.Sprintf("Conflicting vote from self on %s", node),
				Severity:   model.SeverityCritical,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "The node detected a conflicting vote originating from its own key.",
				Evidence:   firstEvidence(nodeEvents, model.EventConflictingVote, 2),
				PossibleCauses: []string{
					"unsafe_reset_all was run on a live validator without resetting the KMS",
					"the same private key is used on more than one validator simultaneously",
				},
				SuggestedActions: []string{
					"immediately stop all nodes sharing this key",
					"investigate whether a double-sign slashing event occurred",
				},
			})
		}

		// ApplyBlock error — application-level crash.
		if count := countByKind(nodeEvents, model.EventApplyBlockError); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "apply-block-error-" + node,
				Title:      fmt.Sprintf("ApplyBlock error on %s", node),
				Severity:   model.SeverityCritical,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    "The application returned an error when applying a block. The node may need a restart or a rollback.",
				Evidence:   firstEvidence(nodeEvents, model.EventApplyBlockError, 2),
				SuggestedActions: []string{
					"check the error field in the log line for the root cause",
					"consider running `gnoland unsafe_reset_all` and re-syncing if the data is corrupted",
				},
			})
		}

		// CommitBlockMissing — the node reached commit phase but lacks the block.
		// This appears transiently during catch-up; only flag when it recurs (>= 3).
		if count := countByKind(nodeEvents, model.EventCommitBlockMissing); count >= 3 {
			findings = append(findings, model.Finding{
				ID:         "missing-commit-block-" + node,
				Title:      fmt.Sprintf("%s repeatedly failed to finalize because the commit block was missing locally", node),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    fmt.Sprintf("Seen %d times. The node reached commit processing but did not have the block required for finalization.", count),
				Evidence:   firstEvidence(nodeEvents, model.EventCommitBlockMissing, 3),
				PossibleCauses: []string{
					"proposal block parts were not fully received before commit",
					"reactor propagation failure between sentry and validator",
				},
				SuggestedActions: []string{
					"inspect reactor and peer logs around the same height",
					"compare with sentry logs for missing block-part propagation",
				},
			})
		}

		if count := countByKind(nodeEvents, model.EventFinalizeNoMaj23); count >= 3 {
			findings = append(findings, model.Finding{
				ID:         "finalize-no-maj23-" + node,
				Title:      fmt.Sprintf("%s failed to finalize because +2/3 majority was absent", node),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    fmt.Sprintf("Seen %d times. Finalization was attempted but quorum was not reached.", count),
				Evidence:   firstEvidence(nodeEvents, model.EventFinalizeNoMaj23, 3),
				PossibleCauses: []string{
					"quorum failure: not enough validators online",
					"network partition isolating a majority of validators",
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
				Summary:    fmt.Sprintf("Seen %d times. Repeated nil prevotes indicate missing or incomplete proposal block reception.", count),
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
				Summary:    fmt.Sprintf("Seen %d times. Consensus rounds advanced without enough prevotes to lock or commit a block.", count),
				Evidence:   firstEvidence(nodeEvents, model.EventPrecommitNoMaj23, 3),
				PossibleCauses: []string{
					"quorum failure",
					"network partition",
					"validator non-participation",
				},
			})
		}

		// Only flag "not a validator" for nodes declared as validators.
		// Sentry nodes legitimately emit this message; it is expected.
		if nodeRoles[node] == model.RoleValidator {
			if count := countByKind(nodeEvents, model.EventNodeNotValidator); count > 0 {
				findings = append(findings, model.Finding{
					ID:         "node-not-validator-" + node,
					Title:      fmt.Sprintf("%s reported that it is not a validator", node),
					Severity:   model.SeverityMedium,
					Confidence: model.ConfidenceHigh,
					Scope:      node,
					Summary:    "This log source was supplied as a validator but the node key is not in the genesis validator set.",
					Evidence:   firstEvidence(nodeEvents, model.EventNodeNotValidator, 2),
					PossibleCauses: []string{
						"wrong key configured; node key is not in the genesis validator set",
						"log file belongs to a sentry node and was supplied via --validator-log by mistake",
					},
				})
			}
		}

		if count := countByKind(nodeEvents, model.EventFastSyncBlockError); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "fastsync-block-error-" + node,
				Title:      fmt.Sprintf("Fast-sync block validation errors on %s", node),
				Severity:   model.SeverityMedium,
				Confidence: model.ConfidenceHigh,
				Scope:      node,
				Summary:    fmt.Sprintf("%d peer(s) were dropped for providing a block that did not match the expected commit during fast-sync.", count),
				Evidence:   firstEvidence(nodeEvents, model.EventFastSyncBlockError, 3),
				PossibleCauses: []string{
					"node has divergent local state relative to the network",
					"possible chain fork affecting a subset of peers",
				},
				SuggestedActions: []string{
					"run `gnoland unsafe_reset_all` and re-sync from a trusted peer",
				},
			})
		}

		if count := countByKind(nodeEvents, model.EventRemoteSignerFailure); count > 0 {
			findings = append(findings, model.Finding{
				ID:         "remote-signer-failure-" + node,
				Title:      fmt.Sprintf("Remote signer failures on %s", node),
				Severity:   model.SeverityHigh,
				Confidence: model.ConfidenceMedium,
				Scope:      node,
				Summary:    fmt.Sprintf("%d signing request failure(s) observed.", count),
				Evidence:   firstEvidence(nodeEvents, model.EventRemoteSignerFailure, 2),
				PossibleCauses: []string{
					"KMS process not running or not reachable on the configured socket",
					"key not loaded in the KMS",
				},
				SuggestedActions: []string{
					"verify the KMS process is running and the socket path matches config",
				},
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
	for _, finding := range findings {
		if finding.Confidence != model.ConfidenceLow {
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

// updateLastConsensusState updates a node's last known consensus position from
// the event. Only events that carry a height > 0 are considered. When the event
// is at a higher height (or same height, higher/equal round) than what was
// previously recorded, the position is updated.
func updateLastConsensusState(summary *model.NodeSummary, event model.Event) {
	if event.Height <= 0 {
		return
	}
	step := inferStepFromEvent(event)
	advance := event.Height > summary.LastHeight ||
		(event.Height == summary.LastHeight && event.Round > summary.LastRound) ||
		(event.Height == summary.LastHeight && event.Round == summary.LastRound && step != "")

	if advance {
		summary.LastHeight = event.Height
		summary.LastRound = event.Round
		if step != "" {
			summary.LastStep = step
		}
	}
	if event.HasTimestamp && event.Timestamp.After(summary.LastEventTime) {
		summary.LastEventTime = event.Timestamp
	}
}

// inferStepFromEvent returns the consensus step name implied by the event kind.
// For timeout events the step field in Fields is consulted first.
func inferStepFromEvent(event model.Event) string {
	switch event.Kind {
	case model.EventSignedProposal, model.EventReceivedCompletePart:
		return "Propose"
	case model.EventPrevoteProposalNil:
		return "Prevote"
	case model.EventPrecommitNoMaj23:
		return "Precommit"
	case model.EventFinalizeNoMaj23:
		return "PrecommitWait"
	case model.EventCommitBlockMissing:
		return "Commit"
	case model.EventFinalizeCommit:
		return "Commit"
	case model.EventTimeout:
		return inferStepFromTimeoutFields(event.Fields)
	}
	return ""
}

// roundStepNames maps the TM2 RoundStepType numeric values to human-readable names.
var roundStepNames = map[int]string{
	1: "NewHeight", 2: "NewRound", 3: "Propose",
	4: "Prevote", 5: "PrevoteWait", 6: "Precommit",
	7: "PrecommitWait", 8: "Commit",
}

func inferStepFromTimeoutFields(fields map[string]any) string {
	raw, ok := fields["step"]
	if !ok {
		return "Timeout"
	}
	switch v := raw.(type) {
	case float64:
		if name, ok := roundStepNames[int(v)]; ok {
			return name + "Timeout"
		}
	case string:
		// e.g. "RoundStepPrevote" — strip the "RoundStep" prefix for brevity
		name := v
		if len(name) > len("RoundStep") {
			name = name[len("RoundStep"):]
		}
		return name + "Timeout"
	}
	return "Timeout"
}
