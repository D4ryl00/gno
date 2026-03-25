package render

import (
	"fmt"
	"strings"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
)

// TextOptions controls the text renderer behaviour.
type TextOptions struct {
	Verbose          bool
	ShowUnclassified bool // show parser warnings for unclassified log lines
	MaxFindings      int  // 0 = unlimited
	MaxHealth        int  // max node sections in health summary (0 = unlimited)
}

func Text(report model.Report, opts TextOptions) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Chain: %s\n", report.Input.ChainID)
	fmt.Fprintf(&b, "Genesis validators: %d\n", report.Input.ValidatorCount)
	fmt.Fprintf(&b, "Logs analyzed: %d files, %d nodes", report.Input.LogFileCount, report.Input.NodeCount)
	if report.Input.TimeWindowStart != "" || report.Input.TimeWindowEnd != "" {
		fmt.Fprintf(&b, ", window %s -> %s", emptyDash(report.Input.TimeWindowStart), emptyDash(report.Input.TimeWindowEnd))
	}
	b.WriteString("\n\n")

	b.WriteString("Health summary\n")
	maxCommit := int64(0)
	for _, node := range report.Nodes {
		if node.HighestCommit > maxCommit {
			maxCommit = node.HighestCommit
		}
	}
	if maxCommit > 0 {
		fmt.Fprintf(&b, "- Forward progress observed until height %d\n", maxCommit)
	} else {
		b.WriteString("- No finalized commit observed in the analyzed window\n")
	}
	if report.MetadataGeneratedPath != "" {
		fmt.Fprintf(&b, "- Metadata template written to %s\n", report.MetadataGeneratedPath)
	}

	shown := 0
	for i, node := range report.Nodes {
		// Timeout counts are only shown in verbose mode: in default mode they
		// are either transient (node committed) or already expressed as a finding
		// ("never finalized a block"). Showing the raw count without that context
		// just creates confusion about missing findings.
		showTimeouts := node.TimeoutCount > 0 && opts.Verbose
		hasPeers := node.MaxPeers > 0
		if !showTimeouts && !hasPeers {
			continue
		}
		if opts.MaxHealth > 0 && !opts.Verbose && shown >= opts.MaxHealth {
			remaining := 0
			for _, n := range report.Nodes[i:] {
				if n.MaxPeers > 0 {
					remaining++
				}
			}
			fmt.Fprintf(&b, "- ... %d more node(s) omitted; use --verbose to see all\n", remaining)
			break
		}
		shown++

		if showTimeouts {
			plural := "s"
			if node.TimeoutCount == 1 {
				plural = ""
			}
			fmt.Fprintf(&b, "- %s saw %d timeout event%s\n", node.Name, node.TimeoutCount, plural)
			if opts.Verbose {
				for _, sample := range node.TimeoutSamples {
					if sample.Path != "" {
						fmt.Fprintf(&b, "  %s:%d %s\n", sample.Path, sample.Line, sample.Message)
					} else {
						fmt.Fprintf(&b, "  %s\n", sample.Message)
					}
				}
				if node.TimeoutCount > len(node.TimeoutSamples) {
					fmt.Fprintf(&b, "  ... %d more\n", node.TimeoutCount-len(node.TimeoutSamples))
				}
			}
		}
		if hasPeers {
			fmt.Fprintf(&b, "- %s peer count max=%d current=%d\n", node.Name, node.MaxPeers, node.CurrentPeers)
		}
	}

	// Consensus state section — only when at least one node has position data.
	anyConsensusState := false
	for _, node := range report.Nodes {
		if node.LastHeight > 0 {
			anyConsensusState = true
			break
		}
	}
	if anyConsensusState {
		// Find the max height across all nodes for divergence annotation.
		maxLastHeight := int64(0)
		for _, node := range report.Nodes {
			if node.LastHeight > maxLastHeight {
				maxLastHeight = node.LastHeight
			}
		}

		b.WriteString("\nConsensus state (end of window)\n")
		for _, node := range report.Nodes {
			if node.LastHeight == 0 {
				if node.Role == model.RoleValidator {
					fmt.Fprintf(&b, "- %s [%s] no consensus events observed\n", node.Name, node.Role)
				}
				continue
			}
			lag := ""
			if maxLastHeight > node.LastHeight {
				lag = fmt.Sprintf(" [!%d behind]", maxLastHeight-node.LastHeight)
			}
			step := ""
			if node.LastStep != "" {
				step = " step=" + node.LastStep
			}
			ts := ""
			if !node.LastEventTime.IsZero() {
				ts = " (last: " + node.LastEventTime.UTC().Format("15:04:05Z") + ")"
			}
			fmt.Fprintf(&b, "- %s [%s] height=%d round=%d%s%s%s\n",
				node.Name, node.Role,
				node.LastHeight, node.LastRound,
				step, ts, lag,
			)
		}
	}

	b.WriteString("\nFindings\n")
	rendered := 0
	for _, finding := range report.Findings {
		if !opts.Verbose && (finding.Severity == model.SeverityInfo || finding.Severity == model.SeverityLow) {
			continue
		}
		rendered++
		if opts.MaxFindings > 0 && rendered > opts.MaxFindings {
			break
		}
		fmt.Fprintf(&b, "[%s] %s\n", finding.Severity, finding.Title)
		fmt.Fprintf(&b, "  %s\n", finding.Summary)
		for _, evidence := range finding.Evidence {
			if evidence.Message == "" {
				continue
			}
			if evidence.Path != "" {
				fmt.Fprintf(&b, "  evidence: %s:%d %s\n", evidence.Path, evidence.Line, evidence.Message)
			} else {
				fmt.Fprintf(&b, "  evidence: %s\n", evidence.Message)
			}
		}
		for _, cause := range finding.PossibleCauses {
			fmt.Fprintf(&b, "  possible cause: %s\n", cause)
		}
		for _, action := range finding.SuggestedActions {
			fmt.Fprintf(&b, "  suggested: %s\n", action)
		}
	}

	if opts.ShowUnclassified && len(report.Warnings) > 0 {
		b.WriteString("\nUnclassified log lines\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
