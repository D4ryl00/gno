package model

import "time"

type Role string

const (
	RoleUnknown   Role = "unknown"
	RoleValidator Role = "validator"
	RoleSentry    Role = "sentry"
	RoleSeed      Role = "seed"
)

func ParseRole(raw string) Role {
	switch Role(raw) {
	case RoleValidator:
		return RoleValidator
	case RoleSentry:
		return RoleSentry
	case RoleSeed:
		return RoleSeed
	default:
		return RoleUnknown
	}
}

type Validator struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	PubKey  string `json:"pub_key"`
	Power   int64  `json:"power"`
}

type Genesis struct {
	Path         string      `json:"path"`
	ChainID      string      `json:"chain_id"`
	GenesisTime  time.Time   `json:"genesis_time"`
	ValidatorNum int         `json:"validator_num"`
	Validators   []Validator `json:"validators"`
}

type Source struct {
	Path         string `json:"path"`
	Node         string `json:"node"`
	Role         Role   `json:"role"`
	ExplicitNode bool   `json:"explicit_node"`
	ExplicitRole bool   `json:"explicit_role"`
}

type Metadata struct {
	Version     int                     `toml:"version" json:"version"`
	ChainID     string                  `toml:"chain_id" json:"chain_id"`
	Nodes       map[string]MetadataNode `toml:"nodes" json:"nodes"`
	Topology    MetadataTopology        `toml:"topology" json:"topology"`
	PeerAliases map[string]string       `toml:"peer_aliases" json:"peer_aliases"`
}

type MetadataNode struct {
	Role             string   `toml:"role" json:"role"`
	Files            []string `toml:"files" json:"files"`
	NodeID           string   `toml:"node_id,omitempty" json:"node_id,omitempty"`
	ValidatorName    string   `toml:"validator_name,omitempty" json:"validator_name,omitempty"`
	ValidatorAddress string   `toml:"validator_address,omitempty" json:"validator_address,omitempty"`
	ValidatorPubKey  string   `toml:"validator_pubkey,omitempty" json:"validator_pubkey,omitempty"`
}

type MetadataTopology struct {
	ValidatorToSentries map[string][]string `toml:"validator_to_sentries" json:"validator_to_sentries"`
}

type EventKind string

const (
	EventUnknown              EventKind = "unknown"
	EventParserWarning        EventKind = "parser_warning"
	EventConfigError          EventKind = "config_error"
	EventAddedPeer            EventKind = "added_peer"
	EventStoppedPeer          EventKind = "stopping_peer"
	EventDialFailure          EventKind = "dial_failure"
	EventMaxOutboundPeers     EventKind = "max_outbound_peers"
	EventNoPeersToShare       EventKind = "no_peers_to_share"
	EventTimeout              EventKind = "timeout"
	EventSwitchToConsensus    EventKind = "switch_to_consensus"
	EventPrevoteProposalNil   EventKind = "prevote_proposal_nil"
	EventPrecommitNoMaj23     EventKind = "precommit_no_maj23"
	EventFinalizeNoMaj23      EventKind = "finalize_no_maj23"
	EventCommitBlockMissing   EventKind = "commit_block_missing"
	EventFinalizeCommit       EventKind = "finalize_commit"
	EventConsensusFailure     EventKind = "consensus_failure"
	EventConflictingVote      EventKind = "conflicting_vote"
	EventApplyBlockError      EventKind = "apply_block_error"
	EventNodeNotValidator     EventKind = "node_not_validator"
	EventSignedProposal       EventKind = "signed_proposal"
	EventRemoteSignerFailure  EventKind = "remote_signer_failure"
	EventRemoteSignerConnect  EventKind = "remote_signer_connected"
	EventReceivedCompletePart EventKind = "received_complete_proposal_block"
	EventFastSyncBlockError   EventKind = "fastsync_block_validation_error"
	EventAddedPrevote         EventKind = "added_prevote"
	EventAddedPrecommit       EventKind = "added_precommit"
	EventCommitUnknownBlock   EventKind = "commit_unknown_block"
	EventCommitLockedBlock    EventKind = "commit_locked_block"
	EventUnexpectedBlockPart  EventKind = "unexpected_block_part"
	EventAddVoteError         EventKind = "add_vote_error"
)

type Event struct {
	Timestamp    time.Time      `json:"timestamp"`
	HasTimestamp bool           `json:"has_timestamp"`
	Node         string         `json:"node"`
	Role         Role           `json:"role"`
	Path         string         `json:"path"`
	Line         int            `json:"line"`
	Format       string         `json:"format"`
	Level        string         `json:"level,omitempty"`
	Message      string         `json:"message"`
	Fields       map[string]any `json:"fields,omitempty"`
	Kind         EventKind      `json:"kind"`
	Height       int64          `json:"height,omitempty"`
	Round        int            `json:"round,omitempty"`
	Raw          string         `json:"raw"`
}

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

type Evidence struct {
	Node      string `json:"node,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	Message   string `json:"message"`
}

type Finding struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Severity         Severity   `json:"severity"`
	Confidence       Confidence `json:"confidence"`
	Scope            string     `json:"scope"`
	Summary          string     `json:"summary"`
	Evidence         []Evidence `json:"evidence,omitempty"`
	PossibleCauses   []string   `json:"possible_causes,omitempty"`
	SuggestedActions []string   `json:"suggested_actions,omitempty"`
}

type NodeSummary struct {
	Name           string     `json:"name"`
	Role           Role       `json:"role"`
	Files          []string   `json:"files"`
	Start          time.Time  `json:"start,omitempty"`
	End            time.Time  `json:"end,omitempty"`
	EventCount     int        `json:"event_count"`
	HighestCommit  int64      `json:"highest_commit"`
	CommitCount    int        `json:"commit_count"`
	TimeoutCount   int        `json:"timeout_count"`
	TimeoutSamples []Evidence `json:"timeout_samples,omitempty"`
	MaxPeers       int        `json:"max_peers"`
	CurrentPeers   int        `json:"current_peers"`
	ParserWarnings int        `json:"parser_warnings"`

	// Last known consensus state (updated per consensus event with Height > 0).
	LastHeight    int64     `json:"last_height,omitempty"`
	LastRound     int       `json:"last_round,omitempty"`
	LastStep      string    `json:"last_step,omitempty"`
	LastEventTime time.Time `json:"last_event_time,omitempty"`

	// Incident phase segmentation.
	LastCommitTime      time.Time     `json:"last_commit_time,omitempty"`
	AvgBlockTime        time.Duration `json:"avg_block_time_ns,omitempty"`
	StallDuration       time.Duration `json:"stall_duration_ns,omitempty"`
	MaxOutboundPeersHit int           `json:"max_outbound_peers_hit,omitempty"`

	// Vote state from the most recent VoteSet debug logs (0 = not observed).
	VoteStateHeight   int64 `json:"vote_state_height,omitempty"`
	PrevotesReceived  int   `json:"prevotes_received,omitempty"`
	PrevotesTotal     int   `json:"prevotes_total,omitempty"`
	PrevotesMaj23     bool  `json:"prevotes_maj23,omitempty"`
	PrecommitsReceived int  `json:"precommits_received,omitempty"`
	PrecommitsTotal   int   `json:"precommits_total,omitempty"`
	PrecommitsMaj23   bool  `json:"precommits_maj23,omitempty"`

	// Fast-sync: set when SwitchToConsensus was observed in this node's logs.
	JoinedViaFastSync    bool  `json:"joined_via_fast_sync,omitempty"`
	FastSyncSwitchHeight int64 `json:"fast_sync_switch_height,omitempty"`

	// Proposer participation.
	ProposalSignedCount int `json:"proposal_signed_count,omitempty"`

	// Round escalation: highest round reached at any single height.
	MaxRoundSeen   int   `json:"max_round_seen,omitempty"`
	MaxRoundHeight int64 `json:"max_round_height,omitempty"`

	// Remote signer cycling.
	SignerFailureCount int `json:"signer_failure_count,omitempty"`
	SignerConnectCount int `json:"signer_connect_count,omitempty"`

	// Peer connectivity.
	DialFailureCount int `json:"dial_failure_count,omitempty"`
}

type InputSummary struct {
	GenesisPath     string `json:"genesis_path"`
	ChainID         string `json:"chain_id"`
	GenesisTime     string `json:"genesis_time,omitempty"`
	ValidatorCount  int    `json:"validator_count"`
	LogFileCount    int    `json:"log_file_count"`
	NodeCount       int    `json:"node_count"`
	TimeWindowStart string `json:"time_window_start,omitempty"`
	TimeWindowEnd   string `json:"time_window_end,omitempty"`
	Strict          bool   `json:"strict"`
}

type Report struct {
	Input                  InputSummary  `json:"input"`
	Nodes                  []NodeSummary `json:"nodes"`
	Findings               []Finding     `json:"findings"`
	Warnings               []string      `json:"warnings,omitempty"`
	MetadataGeneratedPath  string        `json:"metadata_generated_path,omitempty"`
	ConfidenceTooLow       bool          `json:"confidence_too_low"`
	CriticalIssuesDetected bool          `json:"critical_issues_detected"`
}
