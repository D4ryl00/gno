package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
	"github.com/gnolang/gno/tm2/pkg/colors"
)

// TextOptions controls the text renderer behaviour.
type TextOptions struct {
	Verbose          bool
	ShowUnclassified bool // show parser warnings for unclassified log lines
	MaxFindings      int  // 0 = unlimited
	MaxHealth        int  // max node sections in health summary (0 = unlimited)
	Color            bool // emit ANSI color codes (auto-detected from TTY)
}

// colorizer wraps ANSI helpers; a no-op when Color is false.
type colorizer struct{ enabled bool }

func (c colorizer) apply(code, s string) string {
	if !c.enabled || s == "" {
		return s
	}
	return code + s + colors.ANSIReset
}

func (c colorizer) bold(s string) string   { return c.apply(colors.ANSIBright, s) }
func (c colorizer) dim(s string) string    { return c.apply(colors.ANSIDim, s) }
func (c colorizer) red(s string) string    { return c.apply(colors.ANSIFgRed, s) }
func (c colorizer) yellow(s string) string { return c.apply(colors.ANSIFgYellow, s) }
func (c colorizer) green(s string) string  { return c.apply(colors.ANSIFgGreen, s) }
func (c colorizer) cyan(s string) string   { return c.apply(colors.ANSIFgCyan, s) }
func (c colorizer) gray(s string) string   { return c.apply(colors.ANSIFgGray, s) }

func (c colorizer) severity(sev model.Severity) string {
	badge := "[" + string(sev) + "]"
	if !c.enabled {
		return badge
	}
	switch sev {
	case model.SeverityCritical:
		return colors.ANSIBright + colors.ANSIFgRed + badge + colors.ANSIReset
	case model.SeverityHigh:
		return colors.ANSIBright + colors.ANSIFgYellow + badge + colors.ANSIReset
	case model.SeverityMedium:
		return colors.ANSIFgCyan + badge + colors.ANSIReset
	case model.SeverityLow:
		return colors.ANSIFgGray + badge + colors.ANSIReset
	default: // info
		return colors.ANSIFgGray + badge + colors.ANSIReset
	}
}

func Text(report model.Report, opts TextOptions) string {
	var b strings.Builder
	c := colorizer{opts.Color}

	// ── Header ──────────────────────────────────────────────────────────────
	fmt.Fprintf(&b, "Chain: %s\n", report.Input.ChainID)
	fmt.Fprintf(&b, "Genesis validators: %d\n", report.Input.ValidatorCount)
	fmt.Fprintf(&b, "Logs analyzed: %d files, %d nodes", report.Input.LogFileCount, report.Input.NodeCount)
	if report.Input.TimeWindowStart != "" || report.Input.TimeWindowEnd != "" {
		fmt.Fprintf(&b, ", window %s -> %s", emptyDash(report.Input.TimeWindowStart), emptyDash(report.Input.TimeWindowEnd))
	}
	b.WriteString("\n")

	// ── Health summary ───────────────────────────────────────────────────────
	b.WriteString("\n" + c.bold("Health summary") + "\n")
	maxCommit := int64(0)
	for _, node := range report.Nodes {
		if node.HighestCommit > maxCommit {
			maxCommit = node.HighestCommit
		}
	}
	if maxCommit > 0 {
		fmt.Fprintf(&b, "%s\n", c.green(fmt.Sprintf("- Forward progress observed until height %d", maxCommit)))
	} else {
		fmt.Fprintf(&b, "%s\n", c.red("- No finalized commit observed in the analyzed window"))
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
		hasSigner := node.SignerFailureCount > 0
		if !showTimeouts && !hasPeers && !hasSigner {
			continue
		}
		if opts.MaxHealth > 0 && !opts.Verbose && shown >= opts.MaxHealth {
			remaining := 0
			for _, n := range report.Nodes[i:] {
				if n.MaxPeers > 0 {
					remaining++
				}
			}
			fmt.Fprintf(&b, "%s\n", c.dim(fmt.Sprintf("- ... %d more node(s) omitted; use --verbose to see all", remaining)))
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
					fmt.Fprintf(&b, "  %s\n", c.dim(fmt.Sprintf("... %d more", node.TimeoutCount-len(node.TimeoutSamples))))
				}
			}
		}
		if hasPeers {
			stall := ""
			if node.StallDuration > 0 {
				stall = c.red(fmt.Sprintf(" stalled %s", formatDuration(node.StallDuration)))
			}
			fmt.Fprintf(&b, "- %s peer count max=%d current=%d%s\n", node.Name, node.MaxPeers, node.CurrentPeers, stall)
		}
		if hasSigner {
			if node.SignerConnectCount > 0 {
				fmt.Fprintf(&b, "- %s %s failures=%d reconnects=%d\n",
					node.Name, c.red("remote signer unstable:"),
					node.SignerFailureCount, node.SignerConnectCount)
			} else {
				fmt.Fprintf(&b, "- %s %s\n",
					node.Name, c.red(fmt.Sprintf("remote signer: %d failure(s), no reconnect observed", node.SignerFailureCount)))
			}
		}
	}

	// ── Consensus state ──────────────────────────────────────────────────────
	anyConsensusState := false
	for _, node := range report.Nodes {
		if node.LastHeight > 0 {
			anyConsensusState = true
			break
		}
	}
	if anyConsensusState {
		maxLastHeight := int64(0)
		for _, node := range report.Nodes {
			if node.LastHeight > maxLastHeight {
				maxLastHeight = node.LastHeight
			}
		}

		b.WriteString("\n" + c.bold("Consensus state (end of window)") + "\n")
		for _, node := range report.Nodes {
			if node.LastHeight == 0 {
				if node.Role == model.RoleValidator {
					fmt.Fprintf(&b, "- %s [%s] no consensus events observed\n", node.Name, c.dim(string(node.Role)))
				}
				continue
			}
			lag := ""
			if maxLastHeight > node.LastHeight {
				lag = c.red(fmt.Sprintf(" [!%d behind]", maxLastHeight-node.LastHeight))
			}
			step := ""
			if node.LastStep != "" {
				step = " step=" + c.dim(node.LastStep)
			}
			ts := ""
			if !node.LastEventTime.IsZero() {
				ts = c.dim(" (last: " + node.LastEventTime.UTC().Format("15:04:05Z") + ")")
			}
			fastsync := ""
			if node.JoinedViaFastSync {
				if node.FastSyncSwitchHeight > 0 {
					fastsync = c.yellow(fmt.Sprintf(" [fast-sync@h%d]", node.FastSyncSwitchHeight))
				} else {
					fastsync = c.yellow(" [fast-sync]")
				}
			}
			fmt.Fprintf(&b, "- %s [%s] height=%d round=%d%s%s%s%s\n",
				node.Name, c.dim(string(node.Role)),
				node.LastHeight, node.LastRound,
				step, ts, lag, fastsync,
			)
			if node.PrevotesTotal > 0 || node.PrecommitsTotal > 0 {
				prevMaj, precomMaj := "", ""
				if node.PrevotesMaj23 {
					prevMaj = c.green(" +2/3")
				}
				if node.PrecommitsMaj23 {
					precomMaj = c.green(" +2/3")
				}
				fmt.Fprintf(&b, "  prevotes: %d/%d%s  precommits: %d/%d%s\n",
					node.PrevotesReceived, node.PrevotesTotal, prevMaj,
					node.PrecommitsReceived, node.PrecommitsTotal, precomMaj,
				)
			}
			if node.MaxRoundSeen >= 3 {
				fmt.Fprintf(&b, "  %s max_round=%d at h%d\n",
					c.yellow("round escalation:"), node.MaxRoundSeen, node.MaxRoundHeight)
			}
			if node.ProposalSignedCount > 0 {
				fmt.Fprintf(&b, "  proposals signed: %d\n", node.ProposalSignedCount)
			}
		}
	}

	// ── Findings ─────────────────────────────────────────────────────────────
	b.WriteString("\n" + c.bold("Findings") + "\n")
	rendered := 0
	for _, finding := range report.Findings {
		if !opts.Verbose && (finding.Severity == model.SeverityInfo || finding.Severity == model.SeverityLow) {
			continue
		}
		rendered++
		if opts.MaxFindings > 0 && rendered > opts.MaxFindings {
			break
		}
		if rendered > 1 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s %s\n", c.severity(finding.Severity), c.bold(finding.Title))
		fmt.Fprintf(&b, "  %s\n", finding.Summary)
		for _, evidence := range finding.Evidence {
			if evidence.Message == "" {
				continue
			}
			prefix := c.gray("evidence:")
			if evidence.Path != "" {
				fmt.Fprintf(&b, "  %s %s:%d %s\n", prefix, evidence.Path, evidence.Line, evidence.Message)
			} else if evidence.Node != "" {
				fmt.Fprintf(&b, "  %s [%s] %s\n", prefix, evidence.Node, evidence.Message)
			} else {
				fmt.Fprintf(&b, "  %s %s\n", prefix, evidence.Message)
			}
		}
		for _, cause := range finding.PossibleCauses {
			fmt.Fprintf(&b, "  %s %s\n", c.yellow("possible cause:"), cause)
		}
		for _, action := range finding.SuggestedActions {
			fmt.Fprintf(&b, "  %s %s\n", c.cyan("suggested:"), action)
		}
	}

	// ── Unclassified log lines ────────────────────────────────────────────────
	if opts.ShowUnclassified && len(report.Warnings) > 0 {
		b.WriteString("\n" + c.bold("Unclassified log lines") + "\n")
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
