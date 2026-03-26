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
	Verbose  bool
	Metadata model.Metadata // optional; zero value means not provided
}

func BuildReport(input Input) model.Report {
	nodes := buildNodeSummaries(input.Sources, input.Events)
	findings := buildFindings(input.Genesis, nodes, input.Events, input.Warnings, input.Metadata)

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
		},
		Nodes:    nodes,
		Warnings: append([]string(nil), input.Warnings...),
	}

	// Downgrade global finding confidence when only one node has events —
	// a single-node view cannot confirm network-wide conclusions.
	nodesWithEvents := 0
	for _, n := range nodes {
		if n.EventCount > 0 {
			nodesWithEvents++
		}
	}
	if nodesWithEvents <= 1 {
		for i, f := range findings {
			if f.Scope == "global" && f.Confidence == model.ConfidenceHigh {
				findings[i].Confidence = model.ConfidenceMedium
			}
		}
	}
	report.Findings = findings

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
	firstCommitByNode := map[string]time.Time{}
	// Per-node: height → max round seen at that height.
	maxRoundByNode := map[string]map[int64]int{}

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
		if event.Level == "debug" {
			summary.HasDebugLogs = true
		}
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
			if event.HasTimestamp {
				if firstCommitByNode[event.Node].IsZero() {
					firstCommitByNode[event.Node] = event.Timestamp
				}
				if event.Timestamp.After(summary.LastCommitTime) {
					summary.LastCommitTime = event.Timestamp
				}
			}
		case model.EventMaxOutboundPeers:
			summary.MaxOutboundPeersHit++
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
		case model.EventSwitchToConsensus:
			summary.JoinedViaFastSync = true
			if event.Height > 0 {
				summary.FastSyncSwitchHeight = event.Height
			}
		case model.EventAddedPrevote:
			if total, ok := event.Fields["_vtotal"].(int); ok && total > 0 {
				summary.PrevotesReceived, _ = event.Fields["_vrecv"].(int)
				summary.PrevotesTotal = total
				summary.PrevotesMaj23, _ = event.Fields["_vmaj23"].(bool)
				if event.Height >= summary.VoteStateHeight {
					summary.VoteStateHeight = event.Height
				}
			}
		case model.EventAddedPrecommit:
			if total, ok := event.Fields["_vtotal"].(int); ok && total > 0 {
				summary.PrecommitsReceived, _ = event.Fields["_vrecv"].(int)
				summary.PrecommitsTotal = total
				summary.PrecommitsMaj23, _ = event.Fields["_vmaj23"].(bool)
				if event.Height >= summary.VoteStateHeight {
					summary.VoteStateHeight = event.Height
				}
			}
		case model.EventSignedProposal:
			summary.ProposalSignedCount++
		case model.EventRemoteSignerFailure:
			summary.SignerFailureCount++
		case model.EventRemoteSignerConnect:
			summary.SignerConnectCount++
		case model.EventDialFailure:
			summary.DialFailureCount++
		}

		// Track the highest round seen at each height for round-escalation detection.
		if event.Height > 0 && event.Round > 0 {
			if maxRoundByNode[event.Node] == nil {
				maxRoundByNode[event.Node] = map[int64]int{}
			}
			if event.Round > maxRoundByNode[event.Node][event.Height] {
				maxRoundByNode[event.Node][event.Height] = event.Round
			}
		}
	}

	// Second pass: compute derived timing fields now that all events are consumed.
	for name, summary := range summaries {
		if summary.CommitCount >= 2 && !summary.LastCommitTime.IsZero() {
			if first, ok := firstCommitByNode[name]; ok {
				span := summary.LastCommitTime.Sub(first)
				if span > 0 {
					summary.AvgBlockTime = span / time.Duration(summary.CommitCount-1)
				}
			}
		}
		if !summary.LastCommitTime.IsZero() && !summary.End.IsZero() &&
			summary.End.After(summary.LastCommitTime) {
			summary.StallDuration = summary.End.Sub(summary.LastCommitTime)
		}
		// Compute max round seen across all heights for this node.
		if rounds, ok := maxRoundByNode[name]; ok {
			for h, r := range rounds {
				if r > summary.MaxRoundSeen || (r == summary.MaxRoundSeen && h > summary.MaxRoundHeight) {
					summary.MaxRoundSeen = r
					summary.MaxRoundHeight = h
				}
			}
		}
		summaries[name] = summary
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

func buildFindings(genesis model.Genesis, nodes []model.NodeSummary, events []model.Event, warnings []string, meta model.Metadata) []model.Finding {
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
	nodeSummaries := make(map[string]model.NodeSummary, len(nodes))
	for _, n := range nodes {
		nodeRoles[n.Name] = n.Role
		nodeSummaries[n.Name] = n
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
			// Find the panic event (first occurrence).
			panicIdx := -1
			panicHeight := int64(0)
			panicPath := ""
			lastH := int64(0)
			for i, e := range nodeEvents {
				if e.Height > 0 {
					lastH = e.Height
				}
				if e.Kind == model.EventConsensusFailure {
					panicIdx = i
					panicHeight = e.Height
					if panicHeight == 0 {
						panicHeight = lastH
					}
					panicPath = e.Path
					break
				}
			}

			// Collect the last occurrence of each precursor kind before the panic:
			// - When panicHeight > 0: filter by that height.
			// - Otherwise: look back within the last 300 events in the same file
			//   (avoids grabbing startup events from a completely different incident).
			precursorKinds := []model.EventKind{
				model.EventSwitchToConsensus,
				model.EventPrevoteProposalNil,
				model.EventPrecommitNoMaj23,
				model.EventCommitUnknownBlock,
				model.EventCommitBlockMissing,
				model.EventConsensusFailure,
			}
			lastByKind := make(map[model.EventKind]model.Evidence)
			for i, e := range nodeEvents {
				if i > panicIdx {
					break
				}
				if panicHeight > 0 {
					if e.Height > 0 && e.Height != panicHeight {
						continue
					}
				} else {
					// No height on panic: limit to recent events in the same file.
					if panicIdx-i > 300 {
						continue
					}
					if panicPath != "" && e.Path != panicPath {
						continue
					}
				}
				for _, kind := range precursorKinds {
					if e.Kind == kind {
						msg := e.Message
						if kind == model.EventConsensusFailure {
							if stack, ok := e.Fields["stack"].(string); ok && stack != "" {
								msg = msg + " | stack: " + stack
							}
						}
						lastByKind[kind] = model.Evidence{
							Node:      e.Node,
							Timestamp: formatMaybeTime(e.Timestamp),
							Path:      e.Path,
							Line:      e.Line,
							Message:   msg,
						}
						break
					}
				}
			}
			ev := make([]model.Evidence, 0, len(precursorKinds))
			for _, kind := range precursorKinds {
				if e, ok := lastByKind[kind]; ok {
					ev = append(ev, e)
				}
			}
			if len(ev) == 0 {
				ev = firstEvidence(nodeEvents, model.EventConsensusFailure, 1)
			}

			// Detect "joined mid-round via fast-sync": if SwitchToConsensus was
			// collected in the evidence window (height-matched or within 300 events),
			// it happened just before the panic — the node likely panicked on its
			// first consensus round after fast-sync.
			possibleCauses := []string{
				"check the panic stack trace for the root cause",
			}
			ns := nodeSummaries[node]
			if ns.JoinedViaFastSync {
				if _, switchInWindow := lastByKind[model.EventSwitchToConsensus]; switchInWindow {
					possibleCauses = append([]string{
						"node joined consensus mid-round via fast-sync without the proposal block",
					}, possibleCauses...)
				}
			}

			findings = append(findings, model.Finding{
				ID:             "consensus-panic-" + node,
				Title:          fmt.Sprintf("Consensus panic on %s", node),
				Severity:       model.SeverityCritical,
				Confidence:     model.ConfidenceHigh,
				Scope:          node,
				Summary:        "A CONSENSUS FAILURE!!! panic was logged. The node process terminated.",
				Evidence:       ev,
				PossibleCauses: possibleCauses,
				SuggestedActions: []string{
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

		// Validator address mismatch — the genesis validator set on this node differs
		// from what the rest of the network is using.
		if count := countByKind(nodeEvents, model.EventAddVoteError); count > 0 {
			// Only flag when the error is specifically a validator address mismatch.
			addrMismatch := 0
			for _, e := range nodeEvents {
				if e.Kind == model.EventAddVoteError {
					if err, ok := e.Fields["err"].(string); ok && strings.Contains(err, "invalid validator address") {
						addrMismatch++
					}
				}
			}
			if addrMismatch > 0 {
				findings = append(findings, model.Finding{
					ID:         "validator-address-mismatch-" + node,
					Title:      fmt.Sprintf("Genesis validator set mismatch on %s", node),
					Severity:   model.SeverityCritical,
					Confidence: model.ConfidenceHigh,
					Scope:      node,
					Summary: fmt.Sprintf(
						"%d vote(s) were rejected because validator addresses in received votes do not match "+
							"the local genesis. This node cannot participate in consensus correctly.",
						addrMismatch,
					),
					Evidence: firstEvidence(nodeEvents, model.EventAddVoteError, 2),
					PossibleCauses: []string{
						"this node was started with a different genesis.json than the rest of the network",
						"the genesis file was regenerated after some validators had already started",
					},
					SuggestedActions: []string{
						"compare the validators section of this node's genesis.json with another node's genesis.json",
						"restart the node with the correct genesis.json",
					},
				})
			}
		}

		// CommitBlockMissing — the node reached commit phase but lacks the block.
		// This appears transiently during catch-up; only flag when it recurs (>= 3).
		if count := countByKind(nodeEvents, model.EventCommitBlockMissing); count >= 3 {
			ev := firstEvidence(nodeEvents, model.EventCommitBlockMissing, 3)

			// Cross-node analysis: at the first incident height, check which other
			// nodes had the block (committed it or received the complete proposal).
			// This reveals whether the block existed on the network but was not
			// propagated to this node.
			incidentHeight := int64(0)
			for _, e := range nodeEvents {
				if e.Kind == model.EventCommitBlockMissing && e.Height > 0 {
					incidentHeight = e.Height
					break
				}
			}

			nodesWithBlock := make([]string, 0)
			nodesAlsoMissed := make([]string, 0)
			if incidentHeight > 0 {
				for otherNode, otherEvents := range grouped {
					if otherNode == node {
						continue
					}
					hadBlock := false
					alsoMissed := false
					for _, e := range otherEvents {
						if e.Height != incidentHeight {
							continue
						}
						if e.Kind == model.EventFinalizeCommit || e.Kind == model.EventReceivedCompletePart {
							hadBlock = true
						}
						if e.Kind == model.EventCommitBlockMissing {
							alsoMissed = true
						}
					}
					if hadBlock {
						nodesWithBlock = append(nodesWithBlock, otherNode)
					} else if alsoMissed {
						nodesAlsoMissed = append(nodesAlsoMissed, otherNode)
					}
				}
				sort.Strings(nodesWithBlock)
				sort.Strings(nodesAlsoMissed)
			}

			// Append cross-node evidence to the finding.
			if len(nodesWithBlock) > 0 {
				for _, other := range nodesWithBlock {
					ev = append(ev, model.Evidence{
						Node:    other,
						Message: fmt.Sprintf("had the commit block at h%d — block existed on network but was not delivered to %s", incidentHeight, node),
					})
				}
			} else if len(nodesAlsoMissed) > 0 {
				for _, other := range nodesAlsoMissed {
					ev = append(ev, model.Evidence{
						Node:    other,
						Message: fmt.Sprintf("also missing the commit block at h%d — block was absent on multiple nodes", incidentHeight),
					})
				}
			} else if incidentHeight > 0 {
				ev = append(ev, model.Evidence{
					Message: fmt.Sprintf("at h%d: no other observed node has events at this height — add logs from peers active at this height to trace propagation", incidentHeight),
				})
			}

			// Check if block parts were received but rejected at the same heights.
			// Collect heights where the commit block was missing.
			missingHeights := make(map[int64]bool)
			for _, e := range nodeEvents {
				if e.Kind == model.EventCommitBlockMissing && e.Height > 0 {
					missingHeights[e.Height] = true
				}
			}
			// Find unexpected block parts that overlap with missing-block heights.
			rejectedAtMissingHeight := false
			for _, e := range nodeEvents {
				if e.Kind == model.EventUnexpectedBlockPart && e.Height > 0 && missingHeights[e.Height] {
					ev = append(ev, model.Evidence{
						Node:      e.Node,
						Timestamp: formatMaybeTime(e.Timestamp),
						Path:      e.Path,
						Line:      e.Line,
						Message:   fmt.Sprintf("block part for h%d was received but rejected (node not in proposal-receive state)", e.Height),
					})
					rejectedAtMissingHeight = true
					break // one example is enough
				}
			}

			// Tailor the possible causes and suggested actions.
			possibleCauses := []string{
				"proposal block parts were not fully received before commit",
				"reactor propagation failure between sentry and validator",
			}
			if rejectedAtMissingHeight {
				possibleCauses = append([]string{
					"block parts arrived but were rejected because the node was not in proposal-receive state — possible consensus state machine desync",
				}, possibleCauses...)
			}

			suggestedActions := []string{}
			if len(nodesWithBlock) > 0 {
				suggestedActions = append(suggestedActions,
					fmt.Sprintf("check peer connectivity and block-part propagation between %s and the nodes that had the block", node),
				)
			} else if len(nodesAlsoMissed) > 0 {
				suggestedActions = append(suggestedActions,
					"block was missing on multiple nodes — check whether the proposer broadcast the block parts",
				)
			} else {
				suggestedActions = append(suggestedActions,
					fmt.Sprintf("add logs from nodes that were active around h%d to trace the propagation path", incidentHeight),
				)
			}

			findings = append(findings, model.Finding{
				ID:               "missing-commit-block-" + node,
				Title:            fmt.Sprintf("%s repeatedly failed to finalize because the commit block was missing locally", node),
				Severity:         model.SeverityHigh,
				Confidence:       model.ConfidenceHigh,
				Scope:            node,
				Summary:          fmt.Sprintf("Seen %d times. The node reached commit processing but did not have the block required for finalization.", count),
				Evidence:         ev,
				PossibleCauses:   possibleCauses,
				SuggestedActions: suggestedActions,
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

		ns := nodeSummaries[node]
		if ns.SignerFailureCount > 0 {
			// Detect reconnection cycling: multiple failures with reconnects in between.
			isCycling := ns.SignerFailureCount >= 2 && ns.SignerConnectCount >= 1
			sev := model.SeverityHigh
			var possibleCauses []string
			var suggestedActions []string
			var summary string
			if isCycling {
				summary = fmt.Sprintf(
					"Remote signer cycled %d time(s): %d signing failure(s) with %d reconnect(s). "+
						"The KMS connection was unstable during the incident window.",
					ns.SignerConnectCount, ns.SignerFailureCount, ns.SignerConnectCount,
				)
				possibleCauses = []string{
					"KMS process crashing or restarting repeatedly",
					"network instability between the validator and the KMS host",
					"KMS socket or TCP connection timing out under load",
				}
				suggestedActions = []string{
					"check KMS process logs for crashes or restarts during the incident window",
					"verify network stability between validator and KMS host",
					"review KMS connection timeout configuration",
				}
			} else {
				summary = fmt.Sprintf("%d signing request failure(s) observed.", ns.SignerFailureCount)
				possibleCauses = []string{
					"KMS process not running or not reachable on the configured socket",
					"key not loaded in the KMS",
				}
				suggestedActions = []string{
					"verify the KMS process is running and the socket path matches config",
				}
			}
			// Correlate: if this node never signed proposals while having signer failures,
			// the signer was unavailable the entire time — severity escalates to critical.
			if ns.ProposalSignedCount == 0 && ns.Role == model.RoleValidator {
				sev = model.SeverityCritical
				summary += " No proposals were signed during this window — the validator was unable to propose."
			}
			findings = append(findings, model.Finding{
				ID:               "remote-signer-failure-" + node,
				Title:            fmt.Sprintf("Remote signer failures on %s", node),
				Severity:         sev,
				Confidence:       model.ConfidenceMedium,
				Scope:            node,
				Summary:          summary,
				Evidence:         firstEvidence(nodeEvents, model.EventRemoteSignerFailure, 2),
				PossibleCauses:   possibleCauses,
				SuggestedActions: suggestedActions,
			})
		}

		// ── Round escalation ─────────────────────────────────────────────────
		// Flag when a node reached high rounds at a single height, indicating
		// repeated consensus failures before that height was committed (or not).
		if ns.MaxRoundSeen >= 3 {
			findings = append(findings, model.Finding{
				ID:         "round-escalation-" + node,
				Title:      fmt.Sprintf("%s reached round %d at height %d", node, ns.MaxRoundSeen, ns.MaxRoundHeight),
				Severity:   model.SeverityMedium,
				Confidence: model.ConfidenceMedium,
				Scope:      node,
				Summary: fmt.Sprintf(
					"Consensus at height %d required at least %d round(s) before committing or stalling. "+
						"Multiple rounds at the same height indicate repeated agreement failures.",
					ns.MaxRoundHeight, ns.MaxRoundSeen,
				),
				PossibleCauses: []string{
					"proposer repeatedly failed to deliver a valid proposal block",
					"quorum was borderline: a single absent or slow validator forced round changes",
					"network latency between nodes exceeded consensus timeout thresholds",
				},
				SuggestedActions: []string{
					fmt.Sprintf("examine logs from all validators around height %d to find which step stalled", ns.MaxRoundHeight),
					"compare timeout_propose and timeout_commit in config.toml against observed block times",
				},
			})
		}

		// ── Repeated dial failures ────────────────────────────────────────────
		if ns.DialFailureCount >= 5 {
			findings = append(findings, model.Finding{
				ID:         "dial-failures-" + node,
				Title:      fmt.Sprintf("%s had %d dial failures to peers", node, ns.DialFailureCount),
				Severity:   model.SeverityMedium,
				Confidence: model.ConfidenceMedium,
				Scope:      node,
				Summary: fmt.Sprintf(
					"%d outbound dial attempts failed. This may indicate misconfigured persistent_peers "+
						"or a network-level barrier preventing outbound connections.",
					ns.DialFailureCount,
				),
				Evidence: firstEvidence(nodeEvents, model.EventDialFailure, 3),
				PossibleCauses: []string{
					"persistent_peers list contains stale addresses or incorrect node IDs",
					"firewall rules blocking outbound connections on the P2P port",
					"peer nodes are offline or have changed their listening address",
				},
				SuggestedActions: []string{
					"verify that all addresses in persistent_peers are reachable and listening",
					"check whether the target peers are online and accepting connections",
				},
			})
		}
	}

	// ── Stall detection ─────────────────────────────────────────────────────
	// Flag when a node's last commit was well before the end of its log window,
	// suggesting it halted or lost quorum.
	for _, node := range nodes {
		if node.StallDuration <= 0 || node.LastCommitTime.IsZero() {
			continue
		}
		threshold := 60 * time.Second
		if node.AvgBlockTime > 0 {
			if dyn := 5 * node.AvgBlockTime; dyn > 30*time.Second {
				threshold = dyn
			} else {
				threshold = 30 * time.Second
			}
		}
		if node.StallDuration < threshold {
			continue
		}

		// Collect events that occurred after the last commit — the stall window.
		// Primary filter: timestamp-based. Fallback for events that lack a
		// timestamp (raw lines, stack traces): include them if they appear on a
		// line number after the last commit event in the same file.
		lastCommitLineByPath := map[string]int{}
		for _, ev := range grouped[node.Name] {
			if ev.Kind == model.EventFinalizeCommit && ev.Height == node.HighestCommit {
				if ev.Line > lastCommitLineByPath[ev.Path] {
					lastCommitLineByPath[ev.Path] = ev.Line
				}
			}
		}
		stallEvents := make([]model.Event, 0)
		for _, ev := range grouped[node.Name] {
			if ev.HasTimestamp {
				if ev.Timestamp.After(node.LastCommitTime) {
					stallEvents = append(stallEvents, ev)
				}
			} else if commitLine, ok := lastCommitLineByPath[ev.Path]; ok && ev.Line > commitLine {
				stallEvents = append(stallEvents, ev)
			}
		}

		// Inspect what actually happened during the stall window.
		peerIsolated := node.CurrentPeers == 0 && node.MaxPeers > 0
		hasCrash := countByKind(stallEvents, model.EventConsensusFailure) > 0 ||
			countByKind(stallEvents, model.EventApplyBlockError) > 0
		noMaj23Count := countByKind(stallEvents, model.EventFinalizeNoMaj23) +
			countByKind(stallEvents, model.EventPrecommitNoMaj23)
		nilPrevoteCount := countByKind(stallEvents, model.EventPrevoteProposalNil)
		missingBlockCount := countByKind(stallEvents, model.EventCommitBlockMissing)
		signerUnavailable := node.SignerFailureCount > 0 && node.ProposalSignedCount == 0

		// Cross-node quorum analysis: find other validators that also failed to
		// commit the next height. A validator is "covering the stall window" if its
		// log extends past this node's last commit time — that makes its absence of
		// commits meaningful rather than just a missing-log artefact.
		nextH := node.HighestCommit + 1
		type failedPeer struct {
			name    string
			reason  string // "crashed" | "stalled"
			details string
		}
		var failedPeers []failedPeer
		for _, other := range nodes {
			if other.Name == node.Name {
				continue
			}
			// Skip known non-validators; include validators and unknown-role nodes
			// (unknown-role nodes may be validators supplied via --log).
			if other.Role == model.RoleSentry || other.Role == model.RoleSeed {
				continue
			}
			// Other validator also didn't commit nextH and has logs covering the window.
			coversWindow := !other.LastEventTime.IsZero() && other.LastEventTime.After(node.LastCommitTime)
			if !coversWindow || other.HighestCommit >= nextH {
				continue
			}
			// Classify how this validator failed.
			reason := "stalled"
			details := fmt.Sprintf("last commit at h%d, no commit at h%d", other.HighestCommit, nextH)
			// Check if this peer actually crashed (has ConsensusFailure near the stall height).
			// Track the last seen height as we scan so that a panic event without a height
			// field can be attributed to the most recent consensus position.
			lastSeenH := int64(0)
			for _, ev := range grouped[other.Name] {
				if ev.Height > 0 {
					lastSeenH = ev.Height
				}
				if ev.Kind != model.EventConsensusFailure {
					continue
				}
				if ev.Height == 0 || ev.Height == nextH || ev.Height == node.HighestCommit {
					reason = "crashed"
					panicH := ev.Height
					if panicH == 0 {
						panicH = lastSeenH
					}
					if panicH > 0 {
						details = fmt.Sprintf("consensus panic at h%d", panicH)
					} else {
						details = "consensus panic (height not recorded in log)"
					}
					break
				}
			}
			failedPeers = append(failedPeers, failedPeer{other.Name, reason, details})
		}

		// Build evidence list.
		var stallEv []model.Evidence
		if peerIsolated {
			stallEv = append(stallEv, model.Evidence{
				Node:    node.Name,
				Message: fmt.Sprintf("peer count dropped to 0 (max seen during window: %d)", node.MaxPeers),
			})
		}
		if hasCrash {
			for _, k := range []model.EventKind{model.EventConsensusFailure, model.EventApplyBlockError} {
				for _, ev := range stallEvents {
					if ev.Kind == k {
						stallEv = append(stallEv, model.Evidence{
							Node:      ev.Node,
							Timestamp: formatMaybeTime(ev.Timestamp),
							Path:      ev.Path,
							Line:      ev.Line,
							Message:   ev.Message,
						})
						break
					}
				}
			}
		}
		for _, fp := range failedPeers {
			stallEv = append(stallEv, model.Evidence{Node: fp.name, Message: fp.details})
		}

		// Build specific, evidence-driven causes — only list what we can observe.
		possibleCauses := []string{}
		if hasCrash {
			if len(failedPeers) > 0 {
				// Other validators also failed — crash is part of a broader quorum failure.
				possibleCauses = append(possibleCauses,
					"node panic or application crash (see related finding); combined with other validator failures listed below, this likely caused the quorum loss")
			} else {
				// No other validator logs available — crash is known but we can't say whether quorum was lost because of it alone.
				possibleCauses = append(possibleCauses,
					fmt.Sprintf("node panic or application crash (see related finding) — "+
						"if other validators also crashed around h%d, their combined failures could explain the quorum loss; obtain their logs to confirm", nextH))
			}
		}
		if peerIsolated {
			possibleCauses = append(possibleCauses,
				fmt.Sprintf("peer isolation: all %d P2P connections were lost and not recovered", node.MaxPeers))
		}
		if len(failedPeers) > 0 {
			// Count crashed vs stalled to phrase the cause accurately.
			crashedNames, stalledNames := []string{}, []string{}
			for _, fp := range failedPeers {
				if fp.reason == "crashed" {
					crashedNames = append(crashedNames, fp.name)
				} else {
					stalledNames = append(stalledNames, fp.name)
				}
			}
			parts := []string{}
			if len(crashedNames) > 0 {
				parts = append(parts, fmt.Sprintf("%d crashed (%s)", len(crashedNames), strings.Join(crashedNames, ", ")))
			}
			if len(stalledNames) > 0 {
				parts = append(parts, fmt.Sprintf("%d stalled (%s)", len(stalledNames), strings.Join(stalledNames, ", ")))
			}
			possibleCauses = append(possibleCauses,
				fmt.Sprintf("quorum loss caused by other validators also failing at h%d: %s — "+
					"if their combined voting power exceeds 1/3, consensus cannot proceed",
					nextH, strings.Join(parts, " and ")))
		} else if noMaj23Count > 0 {
			// noMaj23 without identified failed peers — quorum issue without known cause.
			possibleCauses = append(possibleCauses,
				fmt.Sprintf("+2/3 voting quorum was not reached %d time(s) — not enough validator votes arrived; logs from other validators are not available to identify which ones failed", noMaj23Count))
		}
		if nilPrevoteCount > 0 {
			possibleCauses = append(possibleCauses,
				fmt.Sprintf("no proposal block arrived %d time(s) — the proposer may be offline or its block parts were not propagated to this node", nilPrevoteCount))
		}
		if missingBlockCount > 0 {
			possibleCauses = append(possibleCauses,
				fmt.Sprintf("commit block was not available locally %d time(s) — block-part propagation from peers failed before finalization", missingBlockCount))
		}
		if signerUnavailable {
			possibleCauses = append(possibleCauses,
				"remote signer was unavailable throughout the stall window — this node could not sign proposals or votes")
		}
		if len(possibleCauses) == 0 {
			// No specific cause detectable from available events.
			if node.HasDebugLogs {
				possibleCauses = append(possibleCauses,
					"cause not determinable from available logs — no consensus failure, quorum error, or peer loss was recorded in the stall window even with debug-level logs present")
			} else {
				possibleCauses = append(possibleCauses,
					"cause not determinable from available logs — no consensus failure, quorum error, or peer loss was recorded in the stall window; enable debug-level logging to get more detail")
			}
		}

		// Build suggested actions.
		suggestedActions := []string{
			fmt.Sprintf("provide logs from %s onward to confirm whether the node recovered", node.LastCommitTime.UTC().Format(time.RFC3339)),
		}
		if peerIsolated {
			suggestedActions = append(suggestedActions,
				"check network connectivity and persistent_peers configuration on this node")
		}
		if len(failedPeers) > 0 {
			suggestedActions = append(suggestedActions,
				fmt.Sprintf("investigate why %d other validator(s) also failed at h%d — fix the root cause on those nodes first", len(failedPeers), nextH))
		} else if hasCrash {
			suggestedActions = append(suggestedActions,
				fmt.Sprintf("obtain logs from other validators covering h%d to check whether they also crashed or stalled simultaneously — if their combined voting power exceeds 1/3, that would explain the quorum loss", nextH))
		} else if noMaj23Count > 0 || nilPrevoteCount > 0 {
			suggestedActions = append(suggestedActions,
				"provide logs from other validators covering the same height range to determine whether the stall was global")
		}

		// Lower confidence when only this node has events after the stall point.
		conf := model.ConfidenceMedium
		active := 0
		for _, n := range nodes {
			if !n.LastEventTime.IsZero() && n.LastEventTime.After(node.LastCommitTime) {
				active++
			}
		}
		if active <= 1 {
			conf = model.ConfidenceLow
		}

		avgBlockNote := ""
		if node.AvgBlockTime > 0 {
			avgBlockNote = fmt.Sprintf(" Average block time was %s.", formatDuration(node.AvgBlockTime))
		}
		findings = append(findings, model.Finding{
			ID:         "stall-after-last-commit-" + node.Name,
			Title:      fmt.Sprintf("%s: no commits for %s after height %d", node.Name, formatDuration(node.StallDuration), node.HighestCommit),
			Severity:   model.SeverityHigh,
			Confidence: conf,
			Scope:      node.Name,
			Summary: fmt.Sprintf(
				"Last commit at h%d; no further commits observed for %s to the end of the log window.%s",
				node.HighestCommit, formatDuration(node.StallDuration), avgBlockNote,
			),
			Evidence:         stallEv,
			PossibleCauses:   possibleCauses,
			SuggestedActions: suggestedActions,
		})
	}

	// ── Sentry vs validator cross-role analysis ───────────────────────────────
	{
		var isolatedValidators []model.NodeSummary
		var reachableSentries []model.NodeSummary
		for _, n := range nodes {
			if n.Role == model.RoleValidator && n.CurrentPeers == 0 && n.MaxPeers > 0 {
				isolatedValidators = append(isolatedValidators, n)
			}
			if n.Role == model.RoleSentry && n.MaxPeers > 0 {
				reachableSentries = append(reachableSentries, n)
			}
		}

		if len(isolatedValidators) > 0 && len(reachableSentries) > 0 {
			// Topology-aware: emit precise per-pair findings when topology is known.
			if len(meta.Topology.ValidatorToSentries) > 0 {
				for _, val := range isolatedValidators {
					pairedSentries, ok := meta.Topology.ValidatorToSentries[val.Name]
					if !ok {
						continue
					}
					for _, sentryName := range pairedSentries {
						sentrySummary, found := nodeSummaries[sentryName]
						if !found || sentrySummary.MaxPeers == 0 {
							continue
						}
						findings = append(findings, model.Finding{
							ID:         "validator-isolated-from-sentry-" + val.Name + "-" + sentryName,
							Title:      fmt.Sprintf("Validator %s lost connection to its sentry %s", val.Name, sentryName),
							Severity:   model.SeverityHigh,
							Confidence: model.ConfidenceHigh,
							Scope:      val.Name,
							Summary: fmt.Sprintf(
								"%s dropped to zero peers while its paired sentry %s remained reachable (max_peers=%d).",
								val.Name, sentryName, sentrySummary.MaxPeers,
							),
							Evidence: []model.Evidence{
								{Node: val.Name, Message: "validator: 0 current peers"},
								{Node: sentryName, Message: fmt.Sprintf("sentry: max_peers=%d during window", sentrySummary.MaxPeers)},
							},
							PossibleCauses: []string{
								"private peer connection between sentry and validator broke and was not re-established",
								"validator's persistent_peers does not include the sentry",
							},
							SuggestedActions: []string{
								"verify persistent_peers on the validator includes the sentry node ID",
								"verify private_peer_ids on the sentry includes the validator node ID",
							},
						})
					}
				}
			} else {
				// Coarse (no topology metadata): one global finding.
				ev := make([]model.Evidence, 0, len(isolatedValidators)+len(reachableSentries))
				for _, v := range isolatedValidators {
					ev = append(ev, model.Evidence{Node: v.Name, Message: "validator: 0 current peers"})
				}
				for _, s := range reachableSentries {
					ev = append(ev, model.Evidence{Node: s.Name, Message: fmt.Sprintf("sentry: max_peers=%d during window", s.MaxPeers)})
				}
				findings = append(findings, model.Finding{
					ID:         "validator-isolated-despite-sentry",
					Title:      "Validator isolated despite reachable sentry",
					Severity:   model.SeverityHigh,
					Confidence: model.ConfidenceMedium,
					Scope:      "global",
					Summary: fmt.Sprintf(
						"%d validator(s) dropped to zero peers while %d sentry node(s) remained reachable.",
						len(isolatedValidators), len(reachableSentries),
					),
					Evidence: ev,
					PossibleCauses: []string{
						"sentry-validator private peer connection misconfigured or dropped",
						"validator's persistent_peers does not list the sentry node ID",
					},
					SuggestedActions: []string{
						"verify private_peer_ids on the validator matches the sentry node ID",
						"verify persistent_peers on the sentry includes the validator",
						"provide topology metadata (--metadata) to get per-pair findings",
					},
				})
			}
		}
	}

	// ── Low max_outbound_peers resilience ────────────────────────────────────
	for _, node := range nodes {
		if node.MaxOutboundPeersHit > 0 && node.MaxPeers <= 2 {
			findings = append(findings, model.Finding{
				ID:         "max-outbound-peers-low-" + node.Name,
				Title:      fmt.Sprintf("Low max_outbound_peers on %s increases connectivity risk", node.Name),
				Severity:   model.SeverityMedium,
				Confidence: model.ConfidenceMedium,
				Scope:      node.Name,
				Summary: fmt.Sprintf(
					"Node hit its outbound peer cap %d time(s) with a ceiling of %d. "+
						"A single peer loss leaves the node under-connected.",
					node.MaxOutboundPeersHit, node.MaxPeers,
				),
				PossibleCauses: []string{
					"max_num_outbound_peers set too low in config.toml",
				},
				SuggestedActions: []string{
					"increase max_num_outbound_peers in config.toml (recommended: >= 5 for sentries, >= 2 for validators with sentries)",
				},
			})
		}
	}

	// ── Proposer analysis ────────────────────────────────────────────────────
	// Find the apparent incident height: the height with the most prevote-nil events.
	// If no proposal was signed there by any node, the proposer was absent or unable to sign.
	{
		heightFreq := map[int64]int{}
		for _, ev := range events {
			if ev.Kind == model.EventPrevoteProposalNil && ev.Height > 0 {
				heightFreq[ev.Height]++
			}
		}
		incidentH := int64(0)
		maxFreq := 0
		for h, freq := range heightFreq {
			if freq > maxFreq || (freq == maxFreq && h > incidentH) {
				maxFreq = freq
				incidentH = h
			}
		}
		if incidentH > 0 && maxFreq >= 2 {
			// Collect nodes that signed a proposal at the incident height.
			proposerNodes := []string{}
			seen := map[string]bool{}
			for _, ev := range events {
				if ev.Kind == model.EventSignedProposal && ev.Height == incidentH && !seen[ev.Node] {
					proposerNodes = append(proposerNodes, ev.Node)
					seen[ev.Node] = true
				}
			}
			sort.Strings(proposerNodes)

			// Collect nodes that received the complete proposal block at the incident height.
			receiverNodes := []string{}
			seen = map[string]bool{}
			for _, ev := range events {
				if ev.Kind == model.EventReceivedCompletePart && ev.Height == incidentH && !seen[ev.Node] {
					receiverNodes = append(receiverNodes, ev.Node)
					seen[ev.Node] = true
				}
			}

			// Build evidence from nil-prevote nodes.
			nilPrevoteEv := []model.Evidence{}
			for _, ev := range events {
				if ev.Kind == model.EventPrevoteProposalNil && ev.Height == incidentH && len(nilPrevoteEv) < 3 {
					nilPrevoteEv = append(nilPrevoteEv, model.Evidence{
						Node:      ev.Node,
						Timestamp: formatMaybeTime(ev.Timestamp),
						Path:      ev.Path,
						Line:      ev.Line,
						Message:   ev.Message,
					})
				}
			}

			if len(proposerNodes) == 0 {
				// No node signed a proposal — proposer was absent or couldn't sign.
				findings = append(findings, model.Finding{
					ID:         fmt.Sprintf("no-proposal-signed-at-h%d", incidentH),
					Title:      fmt.Sprintf("No proposal was signed at stall height %d", incidentH),
					Severity:   model.SeverityHigh,
					Confidence: model.ConfidenceMedium,
					Scope:      "global",
					Summary: fmt.Sprintf(
						"At height %d, %d node(s) prevoted nil because no proposal block was available, "+
							"and no node in the analyzed set signed a proposal. "+
							"The proposer for this round was either offline, failed to connect to its remote signer, or not included in the provided logs.",
						incidentH, maxFreq,
					),
					Evidence: nilPrevoteEv,
					PossibleCauses: []string{
						"the proposer validator was offline or crashed before sending the proposal",
						"remote signer failure prevented the proposer from signing",
						"the proposer's logs are not included in the analyzed set",
					},
					SuggestedActions: []string{
						fmt.Sprintf("provide logs from all validators covering height %d to identify the proposer", incidentH),
						"check remote signer logs on the proposer node for signing failures",
					},
				})
			} else if len(receiverNodes) == 0 {
				// Proposal was signed but no node received the complete block.
				ev := append(nilPrevoteEv, model.Evidence{
					Node:    proposerNodes[0],
					Message: fmt.Sprintf("signed a proposal at height %d", incidentH),
				})
				findings = append(findings, model.Finding{
					ID:         fmt.Sprintf("proposal-not-propagated-h%d", incidentH),
					Title:      fmt.Sprintf("Proposal signed at height %d was not received by peers", incidentH),
					Severity:   model.SeverityHigh,
					Confidence: model.ConfidenceMedium,
					Scope:      "global",
					Summary: fmt.Sprintf(
						"%s signed a proposal at height %d, but no other node in the analyzed set received "+
							"the complete proposal block. Block part propagation failed.",
						strings.Join(proposerNodes, ", "), incidentH,
					),
					Evidence: ev,
					PossibleCauses: []string{
						"proposer's P2P connections dropped after signing, before block parts were broadcast",
						"block part messages were dropped or rejected by receiving peers",
					},
					SuggestedActions: []string{
						"check peer connectivity on the proposer node around the signing time",
						"look for reactor errors or block-part rejection messages on receiving nodes",
					},
				})
			}
		}
	}

	// ── Clock-skew detection ─────────────────────────────────────────────────
	// Compare timestamps of FinalizeCommit events at the same height across nodes.
	// A large spread is a signal of clock skew, which can cause spurious timeouts.
	{
		commitsByHeight := map[int64][]commitPoint{}
		for _, ev := range events {
			if ev.Kind == model.EventFinalizeCommit && ev.HasTimestamp && ev.Height > 0 {
				commitsByHeight[ev.Height] = append(commitsByHeight[ev.Height], commitPoint{ev.Node, ev.Timestamp})
			}
		}
		maxSkew := time.Duration(0)
		skewH := int64(0)
		earlyNode, lateNode := "", ""
		for h, commits := range commitsByHeight {
			if len(commits) < 2 {
				continue
			}
			minTs, maxTs := commits[0].ts, commits[0].ts
			minNode, maxNode := commits[0].node, commits[0].node
			for _, c := range commits[1:] {
				if c.ts.Before(minTs) {
					minTs = c.ts
					minNode = c.node
				}
				if c.ts.After(maxTs) {
					maxTs = c.ts
					maxNode = c.node
				}
			}
			if skew := maxTs.Sub(minTs); skew > maxSkew {
				maxSkew = skew
				skewH = h
				earlyNode = minNode
				lateNode = maxNode
			}
		}
		const skewThreshold = 5 * time.Second
		if maxSkew >= skewThreshold && skewH > 0 {
			findings = append(findings, model.Finding{
				ID:         fmt.Sprintf("clock-skew-%s-%s", earlyNode, lateNode),
				Title:      fmt.Sprintf("Clock skew of %s detected between %s and %s", formatDuration(maxSkew), earlyNode, lateNode),
				Severity:   model.SeverityMedium,
				Confidence: model.ConfidenceLow,
				Scope:      "global",
				Summary: fmt.Sprintf(
					"At height %d, %s committed %s before %s. "+
						"A skew this large can cause spurious consensus timeouts and vote rejection.",
					skewH, earlyNode, formatDuration(maxSkew), lateNode,
				),
				Evidence: []model.Evidence{
					{Node: earlyNode, Message: fmt.Sprintf("committed h%d at %s", skewH, minCommitTime(commitsByHeight[skewH]))},
					{Node: lateNode, Message: fmt.Sprintf("committed h%d at %s", skewH, maxCommitTime(commitsByHeight[skewH]))},
				},
				PossibleCauses: []string{
					"system clock not synchronized (NTP misconfigured or unreachable)",
					"different time zones configured on host systems",
				},
				SuggestedActions: []string{
					"verify NTP is running and synchronized on all validator and sentry hosts",
					"check `timedatectl status` or `chronyc tracking` on each host",
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

type commitPoint struct {
	node string
	ts   time.Time
}

func minCommitTime(commits []commitPoint) string {
	if len(commits) == 0 {
		return ""
	}
	min := commits[0].ts
	for _, c := range commits[1:] {
		if c.ts.Before(min) {
			min = c.ts
		}
	}
	return min.UTC().Format(time.RFC3339)
}

func maxCommitTime(commits []commitPoint) string {
	if len(commits) == 0 {
		return ""
	}
	max := commits[0].ts
	for _, c := range commits[1:] {
		if c.ts.After(max) {
			max = c.ts
		}
	}
	return max.UTC().Format(time.RFC3339)
}

func formatMaybeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
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
	case model.EventAddedPrevote, model.EventPrevoteProposalNil:
		return "Prevote"
	case model.EventAddedPrecommit, model.EventPrecommitNoMaj23:
		return "Precommit"
	case model.EventFinalizeNoMaj23:
		return "PrecommitWait"
	case model.EventCommitBlockMissing, model.EventCommitUnknownBlock, model.EventFinalizeCommit:
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
