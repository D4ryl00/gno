package render

import (
	"fmt"
	"strings"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
)

func Text(report model.Report, verbose bool, maxFindings int) string {
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
	for _, node := range report.Nodes {
		if node.TimeoutCount > 0 {
			fmt.Fprintf(&b, "- %s saw %d timeout events\n", node.Name, node.TimeoutCount)
		}
		if node.MaxPeers > 0 {
			fmt.Fprintf(&b, "- %s peer count max=%d current=%d\n", node.Name, node.MaxPeers, node.CurrentPeers)
		}
	}

	b.WriteString("\nFindings\n")
	rendered := 0
	for _, finding := range report.Findings {
		if !verbose && (finding.Severity == model.SeverityInfo || finding.Severity == model.SeverityLow) {
			continue
		}
		rendered++
		if maxFindings > 0 && rendered > maxFindings {
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
	}

	if verbose && len(report.Warnings) > 0 {
		b.WriteString("\nWarnings\n")
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
