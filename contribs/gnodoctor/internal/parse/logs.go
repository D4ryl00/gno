package parse

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
)

var (
	containerPrefixRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+\s+\|\s+`)
	ansiRE            = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	heightRoundRE     = regexp.MustCompile(`\((\d+)\/(-?\d+)\)`)
	voteSetRE         = regexp.MustCompile(`\+2/3:([^(]+)\([^)]+\) BA\{(\d+):([x_]+)\}`)
)

func ParseLogFile(source model.Source, data []byte) ([]model.Event, []string) {
	lines := strings.Split(string(data), "\n")
	events := make([]model.Event, 0, len(lines))
	warnings := make([]string, 0)

	for i, raw := range lines {
		if raw == "" {
			continue
		}

		event, warning := ParseLogLine(source, raw, i+1)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if event.Raw == "" {
			continue
		}
		events = append(events, event)
	}

	return events, warnings
}

func ParseLogLine(source model.Source, raw string, lineNo int) (model.Event, string) {
	clean := containerPrefixRE.ReplaceAllString(raw, "")
	clean = ansiRE.ReplaceAllString(clean, "")

	switch {
	case strings.HasPrefix(strings.TrimSpace(clean), "{"):
		return parseJSONLine(source, raw, clean, lineNo)
	case looksLikeTimestamp(clean):
		return parseConsoleLine(source, raw, clean, lineNo)
	default:
		event := baseEvent(source, raw, lineNo)
		event.Format = "raw"
		event.Message = strings.TrimSpace(clean)
		event.Kind = classifyMessage(event.Message)
		enrichEvent(&event)
		if event.Kind == model.EventUnknown {
			return event, fmt.Sprintf("%s:%d: unclassified raw line", source.Path, lineNo)
		}
		return event, ""
	}
}

func parseJSONLine(source model.Source, raw, clean string, lineNo int) (model.Event, string) {
	event := baseEvent(source, raw, lineNo)
	event.Format = "json"

	var payload map[string]any
	if err := json.Unmarshal([]byte(clean), &payload); err != nil {
		event.Kind = model.EventParserWarning
		event.Message = strings.TrimSpace(clean)
		return event, fmt.Sprintf("%s:%d: invalid json log line: %v", source.Path, lineNo, err)
	}

	if ts, ok := payload["ts"].(float64); ok {
		sec := int64(ts)
		nsec := int64((ts - float64(sec)) * float64(time.Second))
		event.Timestamp = time.Unix(sec, nsec).UTC()
		event.HasTimestamp = true
	}
	if level, ok := payload["level"].(string); ok {
		event.Level = strings.ToLower(level)
	}
	if msg, ok := payload["msg"].(string); ok {
		event.Message = msg
	}
	delete(payload, "ts")
	delete(payload, "level")
	delete(payload, "msg")
	if len(payload) > 0 {
		event.Fields = payload
	}
	event.Kind = classifyMessage(event.Message)
	enrichEvent(&event)
	if event.Kind == model.EventUnknown {
		return event, fmt.Sprintf("%s:%d: unclassified json message %q", source.Path, lineNo, event.Message)
	}
	return event, ""
}

func parseConsoleLine(source model.Source, raw, clean string, lineNo int) (model.Event, string) {
	event := baseEvent(source, raw, lineNo)
	event.Format = "console"

	tsToken, rest, ok := cutToken(clean)
	if !ok {
		event.Message = strings.TrimSpace(clean)
		event.Kind = model.EventParserWarning
		return event, fmt.Sprintf("%s:%d: unable to split console timestamp", source.Path, lineNo)
	}
	ts, err := time.Parse(time.RFC3339Nano, tsToken)
	if err != nil {
		event.Message = strings.TrimSpace(clean)
		event.Kind = model.EventParserWarning
		return event, fmt.Sprintf("%s:%d: invalid console timestamp %q", source.Path, lineNo, tsToken)
	}
	event.Timestamp = ts.UTC()
	event.HasTimestamp = true

	levelToken, rest, ok := cutToken(rest)
	if !ok {
		event.Message = strings.TrimSpace(rest)
		event.Kind = model.EventParserWarning
		return event, fmt.Sprintf("%s:%d: missing console level", source.Path, lineNo)
	}
	event.Level = strings.ToLower(strings.TrimSpace(levelToken))

	message, fields := splitConsoleMessageAndFields(rest)
	event.Message = message
	if len(fields) > 0 {
		event.Fields = fields
	}
	event.Kind = classifyMessage(event.Message)
	enrichEvent(&event)
	if event.Kind == model.EventUnknown {
		return event, fmt.Sprintf("%s:%d: unclassified console message %q", source.Path, lineNo, event.Message)
	}
	return event, ""
}

func baseEvent(source model.Source, raw string, lineNo int) model.Event {
	return model.Event{
		Node:   source.Node,
		Role:   source.Role,
		Path:   source.Path,
		Line:   lineNo,
		Raw:    raw,
		Fields: map[string]any{},
		Kind:   model.EventUnknown,
	}
}

func looksLikeTimestamp(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) < len("2006-01-02T15:04:05Z") {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, strings.Fields(line)[0])
	return err == nil
}

func cutToken(input string) (string, string, bool) {
	input = strings.TrimLeft(input, " \t")
	if input == "" {
		return "", "", false
	}
	idx := strings.IndexAny(input, " \t")
	if idx < 0 {
		return input, "", true
	}
	return input[:idx], input[idx+1:], true
}

func splitConsoleMessageAndFields(rest string) (string, map[string]any) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", nil
	}

	parts := strings.Split(rest, "\t")
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			trimmed = append(trimmed, part)
		}
	}
	if len(trimmed) == 0 {
		return "", nil
	}

	last := trimmed[len(trimmed)-1]
	if strings.HasPrefix(last, "{") && strings.HasSuffix(last, "}") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(last), &payload); err == nil {
			if len(trimmed) == 1 {
				return "", payload
			}
			return trimmed[len(trimmed)-2], payload
		}
	}

	if idx := strings.LastIndex(rest, "{"); idx >= 0 && strings.HasSuffix(rest, "}") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(rest[idx:])), &payload); err == nil {
			return strings.TrimSpace(rest[:idx]), payload
		}
	}

	return trimmed[len(trimmed)-1], nil
}

func classifyMessage(msg string) model.EventKind {
	switch {
	case msg == "":
		return model.EventUnknown

	// Startup / configuration
	case strings.Contains(msg, "unable to update config field"):
		return model.EventConfigError

	// Fast-sync
	case strings.Contains(msg, "SwitchToConsensus"):
		return model.EventSwitchToConsensus
	case strings.Contains(msg, "BlockchainReactor validation error"):
		return model.EventFastSyncBlockError

	// P2P
	case strings.Contains(msg, "Added peer"):
		return model.EventAddedPeer
	case strings.Contains(msg, "Stopping peer for error"):
		return model.EventStoppedPeer
	case strings.Contains(msg, "unable to dial peer"):
		return model.EventDialFailure
	case strings.Contains(msg, "ignoring dial request: already have max outbound peers"):
		return model.EventMaxOutboundPeers
	case strings.Contains(msg, "no peers to share in discovery request"):
		return model.EventNoPeersToShare

	// Consensus — check specific sub-messages before generic ones
	case strings.Contains(msg, "Added to prevote"):
		return model.EventAddedPrevote
	case strings.Contains(msg, "Added to precommit"):
		return model.EventAddedPrecommit
	case strings.Contains(msg, "Commit is for a block we don't know about"):
		return model.EventCommitUnknownBlock
	case strings.Contains(msg, "Commit is for locked block"):
		return model.EventCommitLockedBlock
	case strings.Contains(msg, "Received a block part when we're not expecting any"):
		return model.EventUnexpectedBlockPart
	case strings.Contains(msg, "Error attempting to add vote"):
		return model.EventAddVoteError
	case strings.Contains(msg, "CONSENSUS FAILURE!!!"):
		return model.EventConsensusFailure
	case strings.Contains(msg, "Found conflicting vote from ourselves"):
		return model.EventConflictingVote
	case strings.Contains(msg, "Error on ApplyBlock"):
		return model.EventApplyBlockError
	case strings.Contains(msg, "enterPrevote: ProposalBlock is nil"):
		return model.EventPrevoteProposalNil
	case strings.Contains(msg, "enterPrecommit: No +2/3 prevotes during enterPrecommit"):
		return model.EventPrecommitNoMaj23
	case strings.Contains(msg, "Attempt to finalize failed. There was no +2/3 majority"):
		return model.EventFinalizeNoMaj23
	case strings.Contains(msg, "Attempt to finalize failed. We don't have the commit block."):
		return model.EventCommitBlockMissing
	case strings.Contains(msg, "Finalizing commit of block"):
		return model.EventFinalizeCommit
	case strings.Contains(msg, "Timed out"):
		return model.EventTimeout
	case strings.Contains(msg, "Received complete proposal block"):
		return model.EventReceivedCompletePart
	case strings.Contains(msg, "Signed proposal"):
		return model.EventSignedProposal

	// Validator identity
	case strings.Contains(msg, "This node is not a validator"):
		return model.EventNodeNotValidator

	// Remote signer
	case strings.Contains(msg, "Sign request failed"):
		return model.EventRemoteSignerFailure
	case strings.Contains(msg, "Connected to server"):
		return model.EventRemoteSignerConnect

	default:
		return model.EventUnknown
	}
}

func enrichEvent(event *model.Event) {
	if event.Fields == nil {
		event.Fields = map[string]any{}
	}

	if event.Height == 0 {
		event.Height = extractHeight(event.Message, event.Fields)
	}
	if event.Round == 0 {
		event.Round = extractRound(event.Message, event.Fields)
	}

	// For prevote/precommit events, parse the VoteSet string to extract vote counts.
	// The VoteSet is stored in "prevotes" or "precommits" field respectively.
	if event.Kind == model.EventAddedPrevote || event.Kind == model.EventAddedPrecommit {
		fieldName := "prevotes"
		if event.Kind == model.EventAddedPrecommit {
			fieldName = "precommits"
		}
		if vs, ok := event.Fields[fieldName].(string); ok {
			recv, total, maj23 := parseVoteSet(vs)
			if total > 0 {
				event.Fields["_vrecv"] = recv
				event.Fields["_vtotal"] = total
				event.Fields["_vmaj23"] = maj23
			}
		}
	}
}

// parseVoteSet extracts vote counts from a TM2 VoteSet string.
// Format: VoteSet{H:19497 R:0 T:2 +2/3:<nil>(0.571) BA{7:x______} map[]}
// Returns received (count of 'x'), total validators, and whether +2/3 majority was reached.
func parseVoteSet(s string) (received, total int, maj23 bool) {
	m := voteSetRE.FindStringSubmatch(s)
	if m == nil {
		return
	}
	// m[1] = "<nil>" or block hash, m[2] = total count, m[3] = bit array string
	maj23 = m[1] != "<nil>"
	total, _ = strconv.Atoi(m[2])
	for _, c := range m[3] {
		if c == 'x' {
			received++
		}
	}
	return
}

func extractHeight(msg string, fields map[string]any) int64 {
	if value, ok := fields["height"]; ok {
		if parsed, ok := toInt64(value); ok {
			return parsed
		}
	}
	// "Added to prevote/precommit" uses "vote height" (with a space) instead of "height".
	if value, ok := fields["vote height"]; ok {
		if parsed, ok := toInt64(value); ok {
			return parsed
		}
	}
	matches := heightRoundRE.FindStringSubmatch(msg)
	if len(matches) == 3 {
		if parsed, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func extractRound(msg string, fields map[string]any) int {
	if value, ok := fields["round"]; ok {
		if parsed, ok := toInt64(value); ok {
			return int(parsed)
		}
	}
	// "Added to prevote/precommit" uses "vote round" (with a space) instead of "round".
	if value, ok := fields["vote round"]; ok {
		if parsed, ok := toInt64(value); ok {
			return int(parsed)
		}
	}
	matches := heightRoundRE.FindStringSubmatch(msg)
	if len(matches) == 3 {
		if parsed, err := strconv.Atoi(matches[2]); err == nil {
			return parsed
		}
	}
	return 0
}

func toInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func DefaultNodeName(path string, used map[string]int) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "-", "_")
	if base == "" {
		base = "node"
	}
	count := used[base]
	used[base]++
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, count+1)
}
