package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/analyze"
	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
	"github.com/gnolang/gno/contribs/gnodoctor/internal/parse"
	"github.com/gnolang/gno/contribs/gnodoctor/internal/render"
	"github.com/gnolang/gno/tm2/pkg/commands"
)

type inspectCfg struct {
	configPath       string
	genesisPath      string
	logPaths         multiString
	validatorLogs    multiString
	sentryLogs       multiString
	metadataPaths    multiString
	generateMetadata string
	nodeBindings     multiString
	roleBindings     multiString
	outputFormat     string
	sinceRaw         string
	untilRaw         string
	strict           bool
	verbose          bool
	showUnclassified bool
	maxFindings      int
	maxHealth        int
}

type multiString []string

func (m *multiString) String() string { return strings.Join(*m, ",") }
func (m *multiString) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func newInspectCmd(io commands.IO) *commands.Command {
	cfg := &inspectCfg{}

	return commands.NewCommand(
		commands.Metadata{
			Name:       "inspect",
			ShortUsage: "inspect [flags]",
			ShortHelp:  "inspect genesis and logs and produce a diagnosis report",
		},
		cfg,
		func(ctx context.Context, _ []string) error {
			return execInspect(ctx, cfg, io)
		},
	)
}

func (c *inspectCfg) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.configPath, "config", "", "path to a TOML config file (default: $XDG_CONFIG_HOME/gnodoctor/config.toml)")
	fs.StringVar(&c.genesisPath, "genesis", "", "path to the genesis.json")
	fs.Var(&c.logPaths, "log", "generic log file path; may be repeated")
	fs.Var(&c.validatorLogs, "validator-log", "validator log file path; may be repeated")
	fs.Var(&c.sentryLogs, "sentry-log", "sentry log file path; may be repeated")
	fs.Var(&c.metadataPaths, "metadata", "TOML metadata file path; may be repeated")
	fs.StringVar(&c.generateMetadata, "generate-metadata", "", "write inferred TOML metadata during inspection")
	fs.Var(&c.nodeBindings, "node", "bind a node name to a log path as <name>=<path>")
	fs.Var(&c.roleBindings, "role", "assign a role to a node as <name>=<role>")
	fs.StringVar(&c.outputFormat, "format", "", "report format: text or json (default: text)")
	fs.StringVar(&c.sinceRaw, "since", "", "lower bound of the analysis window in RFC3339")
	fs.StringVar(&c.untilRaw, "until", "", "upper bound of the analysis window in RFC3339")
	fs.BoolVar(&c.strict, "strict", false, "fail on unresolved input quality issues")
	fs.BoolVar(&c.verbose, "verbose", false, "include low-severity findings; show event details in health summary")
	fs.BoolVar(&c.verbose, "v", false, "shorthand for -verbose")
	fs.BoolVar(&c.showUnclassified, "show-unclassified", false, "print unclassified log lines at the end of the report")
	fs.IntVar(&c.maxFindings, "max-findings", 0, "maximum number of findings rendered in text output (default: 20)")
	fs.IntVar(&c.maxHealth, "max-health", 0, "maximum number of nodes shown in health summary (default: 5; 0 in verbose)")
}

func execInspect(_ context.Context, cfg *inspectCfg, io commands.IO) error {
	// Load config file and apply defaults for flags left at their sentinel (zero) values.
	ucfg, err := loadConfig(cfg.configPath)
	if err != nil {
		return err
	}
	cfg.verbose = cfg.verbose || ucfg.Verbose
	cfg.strict = cfg.strict || ucfg.Strict
	if cfg.outputFormat == "" {
		if ucfg.Format != "" {
			cfg.outputFormat = ucfg.Format
		} else {
			cfg.outputFormat = "text"
		}
	}
	if cfg.maxFindings == 0 {
		if ucfg.MaxFindings > 0 {
			cfg.maxFindings = ucfg.MaxFindings
		} else {
			cfg.maxFindings = 20
		}
	}
	if cfg.maxHealth == 0 && !cfg.verbose {
		if ucfg.MaxHealth > 0 {
			cfg.maxHealth = ucfg.MaxHealth
		} else {
			cfg.maxHealth = 5
		}
	}

	if cfg.genesisPath == "" {
		return errors.New("missing required --genesis")
	}

	since, until, err := parseWindow(cfg.sinceRaw, cfg.untilRaw)
	if err != nil {
		return err
	}

	genesisPath := parse.NormalizePath(cfg.genesisPath)
	genesis, err := parse.LoadGenesis(genesisPath)
	if err != nil {
		return err
	}
	genesis.Path = genesisPath

	metadata, metadataWarnings, err := loadAndMergeMetadata(cfg.metadataPaths)
	if err != nil {
		return err
	}

	sources, err := buildSources(cfg, metadata)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return errors.New("at least one log file is required")
	}

	events := make([]model.Event, 0)
	warnings := append([]string(nil), metadataWarnings...)

	for _, source := range sources {
		data, readErr := os.ReadFile(source.Path)
		if readErr != nil {
			return fmt.Errorf("unable to read log %s: %w", source.Path, readErr)
		}
		fileEvents, fileWarnings := parse.ParseLogFile(source, data)
		events = append(events, filterWindow(fileEvents, since, until)...)
		warnings = append(warnings, fileWarnings...)
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].HasTimestamp && events[j].HasTimestamp {
			if events[i].Timestamp.Equal(events[j].Timestamp) {
				if events[i].Path == events[j].Path {
					return events[i].Line < events[j].Line
				}
				return events[i].Path < events[j].Path
			}
			return events[i].Timestamp.Before(events[j].Timestamp)
		}
		if events[i].HasTimestamp != events[j].HasTimestamp {
			return events[i].HasTimestamp
		}
		if events[i].Path == events[j].Path {
			return events[i].Line < events[j].Line
		}
		return events[i].Path < events[j].Path
	})

	report := analyze.BuildReport(analyze.Input{
		Genesis:  genesis,
		Sources:  sources,
		Events:   events,
		Warnings: warnings,
		Strict:   cfg.strict,
		Verbose:  cfg.verbose,
	})

	if cfg.generateMetadata != "" {
		outPath := parse.NormalizePath(cfg.generateMetadata)
		if _, statErr := os.Stat(outPath); statErr == nil {
			warnings = append(warnings, fmt.Sprintf("metadata output path already exists: %s", outPath))
		} else {
			meta := parse.BuildGeneratedMetadata(genesis, sources)
			if writeErr := parse.WriteMetadata(outPath, meta); writeErr != nil {
				warnings = append(warnings, fmt.Sprintf("unable to write generated metadata %s: %v", outPath, writeErr))
			} else {
				report.MetadataGeneratedPath = outPath
			}
		}
	}
	report.Warnings = append([]string(nil), warnings...)

	if cfg.strict {
		if strictErr := strictValidation(report, warnings); strictErr != nil {
			io.ErrPrintln(strictErr.Error())
			return commands.ExitCodeError(2)
		}
	}

	// Enable colors only when stdout is a real terminal and NO_COLOR is unset.
	colorOutput := term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""

	switch cfg.outputFormat {
	case "text":
		io.Printf("%s", render.Text(report, render.TextOptions{
			Verbose:          cfg.verbose,
			ShowUnclassified: cfg.showUnclassified,
			MaxFindings:      cfg.maxFindings,
			MaxHealth:        cfg.maxHealth,
			Color:            colorOutput,
		}))
	case "json":
		payload, jsonErr := render.JSON(report)
		if jsonErr != nil {
			return jsonErr
		}
		io.Printf("%s\n", payload)
	default:
		return fmt.Errorf("unsupported --format %q", cfg.outputFormat)
	}

	if report.CriticalIssuesDetected {
		return commands.ExitCodeError(1)
	}
	if report.ConfidenceTooLow {
		return commands.ExitCodeError(3)
	}
	return nil
}

func loadAndMergeMetadata(paths []string) (model.Metadata, []string, error) {
	items := make([]model.Metadata, 0, len(paths))
	warnings := make([]string, 0)
	for _, path := range paths {
		normalized := parse.NormalizePath(path)
		item, err := parse.LoadMetadata(normalized)
		if err != nil {
			return model.Metadata{}, nil, fmt.Errorf("unable to load metadata %s: %w", normalized, err)
		}
		for name, node := range item.Nodes {
			for index, file := range node.Files {
				itemNode := item.Nodes[name]
				itemNode.Files[index] = parse.NormalizePath(file)
				item.Nodes[name] = itemNode
			}
		}
		items = append(items, item)
	}
	merged := parse.MergeMetadata(items...)
	if merged.Version == 0 {
		warnings = append(warnings, "metadata version not set; defaulting to version 1")
	}
	return merged, warnings, nil
}

func buildSources(cfg *inspectCfg, meta model.Metadata) ([]model.Source, error) {
	nodeByPath := map[string]string{}
	roleByNode := map[string]model.Role{}
	usedNames := map[string]int{}

	for name, node := range meta.Nodes {
		roleByNode[name] = model.ParseRole(node.Role)
		for _, file := range node.Files {
			nodeByPath[file] = name
		}
	}

	for _, binding := range cfg.nodeBindings {
		name, path, err := splitBinding(binding)
		if err != nil {
			return nil, fmt.Errorf("invalid --node value %q: %w", binding, err)
		}
		nodeByPath[parse.NormalizePath(path)] = name
	}

	for _, binding := range cfg.roleBindings {
		name, rawRole, err := splitBinding(binding)
		if err != nil {
			return nil, fmt.Errorf("invalid --role value %q: %w", binding, err)
		}
		role := model.ParseRole(rawRole)
		if role == model.RoleUnknown && rawRole != string(model.RoleUnknown) {
			return nil, fmt.Errorf("invalid role %q", rawRole)
		}
		roleByNode[name] = role
	}

	type pendingSource struct {
		path string
		role model.Role
	}
	pending := make([]pendingSource, 0, len(cfg.logPaths)+len(cfg.validatorLogs)+len(cfg.sentryLogs))
	for _, path := range cfg.logPaths {
		pending = append(pending, pendingSource{path: parse.NormalizePath(path), role: model.RoleUnknown})
	}
	for _, path := range cfg.validatorLogs {
		pending = append(pending, pendingSource{path: parse.NormalizePath(path), role: model.RoleValidator})
	}
	for _, path := range cfg.sentryLogs {
		pending = append(pending, pendingSource{path: parse.NormalizePath(path), role: model.RoleSentry})
	}

	seen := map[string]model.Source{}
	for _, item := range pending {
		source, ok := seen[item.path]
		if !ok {
			nodeName := nodeByPath[item.path]
			explicitNode := nodeName != ""
			if nodeName == "" {
				nodeName = parse.DefaultNodeName(item.path, usedNames)
			}
			role := item.role
			if role == model.RoleUnknown {
				if mapped, ok := roleByNode[nodeName]; ok {
					role = mapped
				}
			}
			if role == model.RoleUnknown {
				if metaNode, ok := meta.Nodes[nodeName]; ok {
					role = model.ParseRole(metaNode.Role)
				}
			}
			source = model.Source{
				Path:         item.path,
				Node:         nodeName,
				Role:         role,
				ExplicitNode: explicitNode,
				ExplicitRole: item.role != model.RoleUnknown,
			}
		} else if source.Role == model.RoleUnknown && item.role != model.RoleUnknown {
			source.Role = item.role
			source.ExplicitRole = true
		}
		if source.Role == model.RoleUnknown {
			if mapped, ok := roleByNode[source.Node]; ok {
				source.Role = mapped
			}
		}
		seen[item.path] = source
	}

	for name := range roleByNode {
		if _, ok := meta.Nodes[name]; !ok {
			found := false
			for _, source := range seen {
				if source.Node == name {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("--role references unknown node %q", name)
			}
		}
	}

	sources := make([]model.Source, 0, len(seen))
	for _, source := range seen {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Path < sources[j].Path
	})

	return sources, nil
}

func splitBinding(raw string) (string, string, error) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return "", "", errors.New("expected <key>=<value>")
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	if left == "" || right == "" {
		return "", "", errors.New("empty key or value")
	}
	return left, right, nil
}

func parseWindow(sinceRaw, untilRaw string) (time.Time, time.Time, error) {
	var since time.Time
	var until time.Time
	var err error

	if sinceRaw != "" {
		since, err = time.Parse(time.RFC3339, sinceRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --since value: %w", err)
		}
	}
	if untilRaw != "" {
		until, err = time.Parse(time.RFC3339, untilRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --until value: %w", err)
		}
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return time.Time{}, time.Time{}, errors.New("--until must be greater than or equal to --since")
	}

	return since.UTC(), until.UTC(), nil
}

func filterWindow(events []model.Event, since, until time.Time) []model.Event {
	if since.IsZero() && until.IsZero() {
		return events
	}
	filtered := make([]model.Event, 0, len(events))
	for _, event := range events {
		if event.HasTimestamp {
			if !since.IsZero() && event.Timestamp.Before(since) {
				continue
			}
			if !until.IsZero() && event.Timestamp.After(until) {
				continue
			}
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func strictValidation(report model.Report, warnings []string) error {
	if len(warnings) > 0 {
		return fmt.Errorf("strict mode failed due to %d parser or input warnings", len(warnings))
	}
	for _, node := range report.Nodes {
		if node.Role == model.RoleUnknown {
			return fmt.Errorf("strict mode failed because node %q has unresolved role", node.Name)
		}
	}
	return nil
}
