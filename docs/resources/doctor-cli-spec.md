# Doctor CLI Specification

## Status

Draft specification for a Go CLI tool, working name `doctor`.

Note on naming: repository conventions for `contribs/` prefer binaries starting with `gno`, `gnoland`, or `gnokey`. If this tool is added to this repository, the final binary name may need to become `gnodoctor` or `gnoland-doctor`. This specification keeps `doctor` as the product name because that is the requested UX.

## Problem

When a chain halts, pauses, fails to start, or behaves inconsistently, operators usually have:

- a `genesis.json`
- one or more validator logs
- one or more sentry logs
- only a partial time window around the incident

Today, debugging requires manually correlating consensus events, peer churn, reactor traffic, and validator identity with little tooling help. The goal of `doctor` is to turn those artifacts into an operator-oriented diagnosis report.

## Goal

Build a Go CLI that analyzes a `genesis.json` plus validator and/or sentry logs, then reports:

- whether chain startup and block production appear healthy
- where the node(s) stopped making forward progress
- which subsystem most likely caused the issue
- what evidence supports that conclusion
- what data is missing because logs are partial

## Non-Goals

- Replaying the chain or validating application state transitions
- Requiring access to a live node, RPC endpoint, WAL, or data directory
- Proving Byzantine behavior
- Producing perfect diagnoses from incomplete logs
- Replacing raw log inspection for deep incident response

## Primary Users

- Validator operators debugging halts or missed progress
- SREs correlating validator and sentry incidents
- Core contributors investigating field reports

## Core Use Cases

1. A validator started but never produced the first block.
2. A chain progressed for some heights, then halted at height `H`.
3. A validator saw rounds/timeouts increase but never committed.
4. A validator committed blocks, then stopped receiving proposal parts or votes.
5. A sentry remained connected to peers, but its paired validator starved.
6. A node could not join because of bad genesis or validator identity mismatch.
7. Logs are incomplete, but the operator still wants a best-effort diagnosis.

## User Experience

### Primary command

```sh
doctor inspect \
  --genesis ./genesis.json \
  --validator-log ./logs/validator.log \
  --sentry-log ./logs/sentry-a.log \
  --sentry-log ./logs/sentry-b.log
```

### Other useful invocations

```sh
doctor inspect --genesis ./genesis.json --log ./incident/*.log
doctor inspect --genesis ./genesis.json --log validator.json --format json
doctor inspect --genesis ./genesis.json --log ./logs/* --since '2026-03-24T10:00:00Z'
doctor inspect --genesis ./genesis.json --log ./logs/* --node validator-1 --strict
```

### Exit codes

- `0`: no critical issue detected
- `1`: at least one critical issue detected
- `2`: invalid input or parse failure
- `3`: analysis completed, but confidence is too low because inputs are too incomplete

## Inputs

### Required

- `genesis.json`
- at least one log file

### Supported log sources

- validator logs
- sentry logs
- mixed logs from multiple nodes
- partial logs
- rotated logs supplied in chronological or non-chronological order

### Supported log formats

- structured JSON logs
- console logs emitted by the existing zap console logger

The parser should be best-effort. Unknown lines should be retained as raw evidence, not discarded silently.

### Optional metadata flags

Because logs may not always encode enough identity information, the CLI should accept operator hints:

- `--node <name>=<path>`
- `--role <name>=validator|sentry|seed|unknown`
- `--peer-map <node-id>=<friendly-name>`
- `--validator-addr <name>=<bech32>`
- `--validator-pubkey <name>=<bech32>`
- `--since`, `--until`
- `--timezone`

## Output

### Human-readable report

The default output should be a concise incident report:

1. Input summary
2. Timeline summary
3. Findings ordered by severity
4. Likely root causes
5. Missing evidence / confidence notes
6. Suggested next checks

Example shape:

```text
Chain: test5
Genesis validators: 4
Logs analyzed: 3 files, 2 nodes, 18m24s window

Health summary
- Forward progress stopped at height 812
- validator-1 remained in rounds 3..11 at height 813
- sentry-a had stable peers but stopped delivering block parts after 10:14:22Z

Findings
[critical] validator-1 never observed a complete proposal block at height 813
  evidence: repeated "ProposalBlock is nil", "Attempt to finalize failed. We don't have the commit block."

[high] peer starvation on validator-1
  evidence: peer count dropped from 6 to 0 within 14s; repeated "Stopping peer for error"

[medium] logs are partial after 10:15:01Z
  impact: cannot confirm whether quorum existed elsewhere
```

### Machine-readable output

`--format json` should produce:

- input metadata
- detected nodes
- parsed timeline stats
- findings with severity, confidence, evidence, and remediation
- per-node summaries
- parser warnings

This JSON should be stable enough to support CI and incident pipelines.

## High-Level Architecture

The tool should be organized as five layers:

1. `cmd/doctor`
   CLI, flags, exit codes, report rendering
2. `internal/input`
   file loading, log file expansion, gzip support if added later
3. `internal/parse`
   genesis parsing and log parsing
4. `internal/model`
   normalized events, node identities, consensus timeline, peer graph
5. `internal/analyze`
   rule engine that emits findings

Suggested repository shape if implemented as a contrib tool:

```text
contribs/gnodoctor/
  main.go
  README.md
  Makefile
  internal/cmd/
  internal/input/
  internal/parse/
  internal/model/
  internal/analyze/
  internal/render/
  testdata/
```

The CLI should follow the existing `tm2/pkg/commands` pattern used elsewhere in this repository, not introduce Cobra unless there is a strong reason.

## Data Model

### Genesis model

Extract at minimum:

- `chain_id`
- `genesis_time`
- validator set
- validator names
- validator addresses
- validator public keys
- validator voting powers
- consensus params

### Node model

Each log source should map into a logical node:

- `name`
- `role`
- `files`
- inferred node ID if present
- inferred validator identity if present
- time range covered by the logs

### Event model

Normalize parsed lines into events such as:

- node start / stop
- waiting for genesis time
- added peer
- stopped peer
- dial attempt / dial failure
- consensus step transition
- timeout
- signed proposal
- prevote decision
- precommit decision
- enter commit
- finalize commit
- received proposal
- received block part
- invalid message
- remote signer error
- parser warning

Each event should keep:

- timestamp
- node
- subsystem
- severity
- raw message
- structured attributes
- source file and line number if available

## Log Parsing Requirements

### General parser behavior

- accept both JSON and console logs in the same run
- detect and strip ANSI color codes
- handle out-of-order lines by sorting on parsed timestamps when possible
- preserve original ordering when timestamps are equal or missing
- continue past malformed lines and report parser warnings

### Message matching strategy

Use a layered strategy:

1. structured field extraction for JSON logs
2. exact message matching for well-known TM2/Gnoland log messages
3. regex extraction for console logs
4. raw-line fallback for unclassified evidence

### Initial real message families to support

Consensus state:

- `enterPropose: Our turn to propose`
- `enterPropose: Not our turn to propose`
- `Signed proposal`
- `enterPrevote(`
- `enterPrevote: ProposalBlock is nil`
- `enterPrevote: ProposalBlock is invalid`
- `enterPrevote: ProposalBlock is valid`
- `enterPrevoteWait(`
- `enterPrecommit(`
- `enterPrecommit: No +2/3 prevotes during enterPrecommit`
- `enterPrecommit: +2/3 prevoted for nil`
- `enterPrecommit: +2/3 prevoted proposal block. Locking`
- `enterPrecommitWait(`
- `enterCommit(`
- `Attempt to finalize failed. We don't have the commit block.`
- `Finalizing commit of block`
- `Timed out`

Consensus reactor:

- `Receive`
- `Error decoding message`
- `Peer sent us invalid msg`
- `Ignoring message received during fastSync`
- `Received block part from wrong round`
- `Sending POL`
- `Peer ProposalBlockPartsHeader mismatch, sleeping`

P2P:

- `Added peer`
- `Stopping peer for error`
- `dialing peer`
- `unable to dial peer`
- `unable to add peer`
- `Ignoring inbound connection: already have enough inbound peers`
- `ignoring dial request: already have max outbound peers`

Node startup:

- `RPC+P2P running. Waiting for genesis time to start consensus...`
- `Stopping Node`

## Analysis Model

Each finding should have:

- `id`
- `title`
- `severity`: `info`, `low`, `medium`, `high`, `critical`
- `confidence`: `low`, `medium`, `high`
- `scope`: global or per-node
- `summary`
- `evidence`
- `possible_causes`
- `suggested_actions`

The analyzer should prefer:

- evidence-backed findings
- clear causal hypotheses
- explicit uncertainty when logs are partial

## Checks

The tool should ship with a first-pass ruleset in the categories below.

### 1. Input and genesis checks

- `genesis.json` parses successfully
- `chain_id` is non-empty
- genesis validator set is non-empty
- validator pubkeys and addresses are internally consistent
- validator powers are non-zero
- consensus params are present and valid
- duplicate validator names, addresses, or pubkeys are flagged
- log files appear to belong to one chain window
- if logs expose chain ID or validator identity, they should be reconciled against genesis

Example findings:

- bad genesis validator entry
- duplicate validator identity
- logs appear to mix multiple chains
- validator log belongs to a node absent from genesis validator set

### 2. Startup and chain-liveness checks

- did the node start cleanly
- did it wait for genesis time
- did it ever enter consensus
- did it ever finalize the first block
- what is the highest committed height seen
- is there a long period with no `Finalizing commit of block`
- is the chain halted globally or only on one node

Example findings:

- startup blocked before genesis time
- node never reached first commit
- node committed until height `H` then stalled

### 3. Consensus progression checks

These are the highest-value checks for a first release.

- repeated `enterPrevote` with `ProposalBlock is nil`
- repeated `enterPrecommit` with no `+2/3 prevotes`
- repeated `+2/3 prevoted for nil`
- rounds increasing without `enterCommit`
- `enterCommit` seen but never `Finalizing commit of block`
- repeated `Attempt to finalize failed. We don't have the commit block.`
- proposer signs proposals, but peers never complete block reception
- validator is a proposer but never signs a proposal on its turn
- no forward progress despite timeouts and round churn

Derived diagnoses:

- proposer starvation
- quorum failure
- missing proposal block parts
- commit block unavailable locally
- validator not participating in consensus

### 4. Reactor and block propagation checks

- proposal messages received but block parts remain incomplete
- block parts are received from wrong round repeatedly
- reactor reports invalid or undecodable messages
- vote set majority messages appear without subsequent useful progress
- fast-sync mode causes consensus messages to be ignored unexpectedly
- proposal block parts header mismatches recur

Derived diagnoses:

- proposal propagation failure
- block part mismatch
- malformed or incompatible peer traffic
- node left in an unexpected fast-sync condition

### 5. Peer connectivity and churn checks

- peer count over time by node
- zero-peer windows
- repeated dial failures to persistent peers
- repeated `Stopping peer for error`
- excessive churn relative to observation window
- inbound saturation or outbound saturation preventing recovery
- sentry has peers while validator has none
- all nodes lose peers at the same time

Derived diagnoses:

- validator isolation
- unstable peer connectivity
- persistent peer misconfiguration
- topology asymmetry between sentries and validator
- network partition suspicion

### 6. Validator identity and role checks

- identify which logs belong to genesis validators
- identify non-validator nodes that still participate in relay traffic
- flag a "validator" log whose node reports `This node is not a validator`
- reconcile proposer turns with validator identity when enough evidence exists

Derived diagnoses:

- wrong key configured
- node expected to validate but is not in validator set
- sentry misidentified as validator

### 7. Partial-log confidence checks

Because partial logs are a core requirement, every major conclusion should be conditioned by coverage:

- missing startup window
- missing end-of-incident window
- no logs from other validators
- no logs from the suspected proposer
- node clocks differ materially

Derived outputs:

- `high confidence`
- `medium confidence`
- `low confidence because only one node log covers the halt window`

## Additional Checks Worth Including

These go beyond the initial ideas but are likely useful.

### Remote signer checks

- proposal signing failures
- remote signer client closed unexpectedly
- repeated signing errors around halt time

### Time-skew checks

- detect suspicious clock skew across nodes from overlapping events
- flag ambiguous event ordering caused by skew

### Topology checks

- validator should rarely expose broad peer churn if traffic is supposed to arrive via sentries
- sentries connected to the network but validator isolated from sentries

### Incident segmentation

- split analysis into phases: startup, healthy progress, degraded progress, halt
- make findings per phase, not just globally

### Throughput degradation before halt

- increasing commit latency before stop
- increasing rounds per height before stop
- rising peer churn before stop

## Scoring and Heuristics

The first implementation should use transparent heuristics, not machine learning.

Example scoring inputs:

- number of consecutive timeout events at same height
- number of rounds seen without commit
- ratio of peer removals to peer additions
- zero-peer duration
- number of repeated finalize failures
- whether corroborating evidence exists on multiple nodes

The report should say when a conclusion is a heuristic, for example:

- "Likely cause: validator isolated from peers"
- "Possible cause: proposer failed to propagate block parts"

## Suggested Subcommands

The initial release only needs one main subcommand, but this layout leaves room to grow.

- `doctor inspect`
  analyze genesis and logs, produce findings
- `doctor parse`
  parse logs and emit normalized events for debugging parser behavior
- `doctor rules`
  list implemented checks and severities

## Suggested Flags

- `--genesis <path>`
- `--log <path>`
- `--validator-log <path>`
- `--sentry-log <path>`
- `--node <name>=<path>`
- `--role <name>=<role>`
- `--format text|json`
- `--since <rfc3339>`
- `--until <rfc3339>`
- `--strict`
- `--verbose`
- `--max-findings <n>`

## Report Design Requirements

- findings first, raw event dumps second
- evidence should reference node, timestamp, and message snippet
- missing data should be explicit
- avoid claiming a global halt when only one node log is present
- when multiple causes are plausible, rank them instead of pretending certainty

## Testing Strategy

### Unit tests

- genesis parsing
- JSON log parsing
- console log parsing
- event normalization
- each analysis rule

### Golden tests

Add end-to-end test fixtures with:

- healthy startup and healthy block production
- no peers
- repeated timeouts with nil prevotes
- commit block missing locally
- repeated peer disconnects
- partial logs with low-confidence result

### Fixture format

Prefer test fixtures that bundle:

- `genesis.json`
- one or more log files
- expected JSON findings

This makes rule evolution reviewable.

## Incremental Delivery Plan

### Phase 1

Deliver a useful MVP:

- parse genesis
- parse JSON and console logs
- identify nodes and roles
- detect committed heights
- detect stalls, timeouts, zero-peer windows, peer churn
- detect repeated consensus-step failure patterns
- emit text and JSON reports

### Phase 2

Improve cross-node correlation:

- peer mapping
- sentry versus validator comparisons
- incident phase segmentation
- confidence scoring improvements

### Phase 3

Optional deeper diagnostics:

- remote signer diagnosis
- topology inference
- richer proposer analysis
- optional RPC enrichment if live mode is ever added

## Open Questions

- Should the tool remain purely offline, or later support optional RPC enrichment?
- Should node identity come only from logs, or also from optional metadata files?
- Should console-log support be best-effort only, with JSON logs recommended for reliable analysis?
- Should the tool support compressed logs in the first release?
- Should this live in `contribs/` immediately, or start as a standalone design doc plus fixtures?

## Recommendation

Build `doctor` as an offline-first contrib tool in Go, using the repository's existing command framework and a rule-based analyzer. The MVP should focus on the checks that provide the most operator value with partial logs:

- startup and first-block success
- highest committed height and halt point
- repeated prevote/precommit/timeout patterns
- missing commit block locally
- peer loss and churn
- reactor message failures
- confidence reporting for incomplete evidence

That scope is narrow enough to implement incrementally and broad enough to be useful in real incidents.
