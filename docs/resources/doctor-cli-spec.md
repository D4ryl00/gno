# Doctor CLI Specification

## Status

Draft specification for a Go CLI tool, working name `doctor`.

Note on naming: repository conventions for `contribs/` prefer binaries starting with `gno`, `gnoland`, or `gnokey`. If this tool is added to this repository, the final binary name may need to become `valdoctor` or `gnoland-doctor`. This specification keeps `doctor` as the product name because that is the requested UX.

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
8. A consensus panic (`CONSENSUS FAILURE!!!`) terminated the node.

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

The two formats currently emitted by gnoland nodes are:

**JSON logs** — one JSON object per line (when `log_format = json`):

```
{"level":"info","ts":1774017464.5705216,"msg":"Starting Peer","module":"p2p","peer":"...","impl":"..."}
```

- `level`: `debug`, `info`, `warn`, `error`
- `ts`: Unix epoch seconds as a float64
- `msg`: the log message string
- additional structured key-value fields follow

**Console logs** — a semi-structured format emitted by the zap console logger (when `log_format = console`):

```
2026-03-20T14:37:08.485Z	INFO 	Added peer	{"module": "p2p", "peer": "Peer{...}"}
```

- timestamp: RFC3339 with milliseconds, always UTC
- level word with ANSI color codes: `\x1b[34mINFO \x1b[0m`, `\x1b[35mDEBUG\x1b[0m`, `\x1b[33mWARN \x1b[0m`, `\x1b[31mERROR\x1b[0m`
- fields appended as a trailing JSON object `{"key": "value", ...}`, absent when no fields exist

**Container prefix** — when logs come from Docker or Compose, each line may be prefixed by a container name and pipe symbol before the actual log content:

```
gnoland-1  | {"level":"info","ts":...}
gnoland    | 2026-03-20T14:37:08.485Z	INFO 	...
```

The parser must strip this prefix before classifying the log format.

**Non-log lines** — startup output may include plain-text configuration lines before the first structured entry:

```
Default configuration initialized at gnoland-data/config/config.toml
Updated configuration saved at gnoland-data/config/config.toml
unable to update config field, field "max_num_intbound_peers", is not a valid configuration key, available keys: [...]
```

These must be preserved as raw evidence and classified separately.

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

### Optional metadata files

Node identity should come from logs when possible, but the tool must also support optional metadata files to improve mapping and confidence.

Recommended format for v1:

- TOML is the metadata format
- JSON and YAML are not supported for metadata in v1

Why TOML:

- easy for operators to read and edit
- supports comments
- simpler and less ambiguous than YAML
- already familiar in this repository because Gno and TM2 tooling use TOML-style config in several places

Useful examples:

- node inventory files mapping hostnames, node IDs, and roles
- validator inventory files mapping friendly names to bech32 addresses or pubkeys
- topology files describing validator-to-sentry relationships
- peer alias files mapping peer IDs to human-readable labels

Suggested flag:

- `--metadata <path>` for one or more TOML metadata files
- `--generate-metadata <path>` to write an inferred TOML metadata file during `doctor inspect`

Copyable example:

```toml
version = 1
chain_id = "test5"

[nodes.validator_1]
role = "validator"
files = ["./logs/validator-1.log"]
node_id = "3b1f4d8e9c2a7f1a6d0b4c8e2f9a7c1d3e5f7a9b"
validator_name = "validator-1"
validator_address = "g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5"
validator_pubkey = "gpub1pgfj7ard9eg82cjtv4u4xetrwqer2dntxyfzxz3pq0skzdkmzu0r9h6gny6eg8c9dc303xrrudee6z4he4y7cs5rnjwmyf40yaj"

[nodes.sentry_a]
role = "sentry"
files = ["./logs/sentry-a.log"]
node_id = "91aa0d52ef8a2f6c84921f70839aa8a32e2b2b11"

[nodes.sentry_b]
role = "sentry"
files = ["./logs/sentry-b.log"]
node_id = "6f64b3f7b2f74d9d6db4f5a738b52fc1a3de2ad4"

[topology]
validator_to_sentries = { validator_1 = ["sentry_a", "sentry_b"] }

[peer_aliases]
"c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8090a1b2c" = "public-seed-1"
"d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0" = "public-validator-2"
```

Minimum useful file:

```toml
version = 1

[nodes.validator_1]
role = "validator"
files = ["./logs/validator-1.log"]
```

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
   file loading, log file expansion, metadata loading
3. `internal/parse`
   genesis parsing and log parsing
4. `internal/model`
   normalized events, node identities, consensus timeline, peer graph
5. `internal/analyze`
   rule engine that emits findings

Repository placement:

- implement this in `contribs/` immediately
- use a standalone contrib binary and README from the start

Suggested repository shape:

```text
contribs/valdoctor/
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
- config error (unrecognized key or bad value)
- waiting for genesis time
- WAL started / WAL stopped
- fast-sync started / fast-sync finished (SwitchToConsensus)
- fast-sync block validation error (wrong block id from peer)
- added peer
- stopped peer
- dial attempt / dial failure
- consensus step transition (enterNewRound, enterPropose, enterPrevote, enterPrecommit, enterCommit)
- timeout
- signed proposal
- proposal signing failure
- vote signed and pushed
- vote signing failure
- prevote decision (nil / locked / valid / invalid)
- precommit decision (+2/3 prevotes / no prevotes / nil locked / relocking)
- enter commit
- commit for unknown block
- commit for locked block
- finalize attempt failed (no commit block)
- finalize attempt failed (no +2/3 majority)
- finalizing commit of block
- received proposal
- received complete proposal block
- received block part from wrong height or round
- received unexpected block part
- consensus panic (CONSENSUS FAILURE!!!)
- conflicting vote from self
- apply block error
- vote set update (prevote or precommit bit array)
- validator set update
- invalid message
- remote signer: dial / retry / connected / request succeeded / request failed
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

- strip container prefixes (`<name>  | ` or `<name> | `) before classifying format
- accept both JSON and console logs in the same run
- detect and strip ANSI color codes from console logs
- parse JSON `ts` as a float64 Unix epoch (seconds + fractional seconds)
- parse console timestamps as RFC3339 UTC
- handle out-of-order lines by sorting on parsed timestamps when possible
- preserve original ordering when timestamps are equal or missing
- continue past malformed lines and report parser warnings

### Reliability policy by log format

JSON logs are the reference format for reliable analysis.

Console logs must still be supported for core offline diagnostics, but only on a best-effort basis:

- core findings should still work when console logs are the only input
- field extraction may be incomplete or ambiguous compared to JSON logs
- parser warnings should be surfaced when console parsing loses structure
- confidence should be reduced when conclusions depend on ambiguous console-only evidence

Compressed logs are out of scope for the first release.

### Message matching strategy

Use a layered strategy:

1. structured field extraction for JSON logs
2. exact message matching for well-known TM2/Gnoland log messages
3. regex extraction for console logs
4. raw-line fallback for unclassified evidence

### Message families to support

#### Consensus state

From `tm2/pkg/bft/consensus/state.go`:

- `enterNewRound(H/R). Current: H/R/Step` — track round increments
- `enterNewRound(H/R): Invalid args.` (debug)
- `Resetting Proposal info`
- `Need to set a buffer and log message here for sanity.` — clock skew indicator
- `enterPropose(H/R). Current: H/R/Step`
- `enterPropose(H/R): Invalid args.` (debug)
- `enterPropose: Our turn to propose`
- `enterPropose: Not our turn to propose`
- `enterPropose: Error signing proposal`
- `enterPropose: Cannot propose anything: No commit for the previous block.`
- `Signed proposal`
- `enterPrevote(H/R). Current: H/R/Step`
- `enterPrevote: Block was locked`
- `enterPrevote: ProposalBlock is nil`
- `enterPrevote: ProposalBlock is invalid` (error level)
- `enterPrevote: ProposalBlock is valid`
- `enterPrevoteWait(H/R). Current: H/R/Step`
- `enterPrecommit(H/R). Current: H/R/Step`
- `enterPrecommit: No +2/3 prevotes during enterPrecommit. Precommitting nil.`
- `enterPrecommit: No +2/3 prevotes during enterPrecommit while we're locked. Precommitting nil`
- `enterPrecommit: +2/3 prevoted for nil.`
- `enterPrecommit: +2/3 prevoted for nil. Unlocking`
- `enterPrecommit: +2/3 prevoted locked block. Relocking`
- `enterPrecommit: +2/3 prevoted proposal block. Locking`
- `enterPrecommitWait(H/R). Current: H/R/Step`
- `enterCommit(H/R). Current: H/R/Step`
- `enterCommit(H/R): Invalid args.` (debug)
- `Commit is for locked block. Set ProposalBlock=LockedBlock`
- `Commit is for a block we don't know about. Set ProposalBlock=nil`
- `Attempt to finalize failed. There was no +2/3 majority, or +2/3 was for <nil>.` (error)
- `Attempt to finalize failed. We don't have the commit block.`
- `Finalizing commit of block`
- `Calling finalizeCommit on already stored block`
- `Error on ApplyBlock. Did the application crash? Please restart tendermint` (error)
- `CONSENSUS FAILURE!!!` (error, with `stack` field containing a Go panic stack trace)
- `Found conflicting vote from ourselves. Did you unsafe_reset a validator?` (error)
- `Error signing vote` (error)
- `Unlocking because of POL.`
- `Added to lastPrecommits`
- `Vote ignored and not added`
- `Added to prevote` (debug, with `prevotes` VoteSet bit array string)
- `Added to precommit` (debug, with `precommits` VoteSet bit array string)
- `Received a block part when we're not expecting any`
- `Received complete proposal block`
- `Received block part from wrong height` (debug)
- `Received block part from wrong round` (debug)
- `Received proposal`
- `Signed and pushed vote`
- `Timed out` (from `tm2/pkg/bft/consensus/ticker.go`)

#### Consensus reactor

From `tm2/pkg/bft/consensus/reactor.go`:

- `ConsensusReactor` (with `fastSync` field — true when starting in fast-sync mode)
- `SwitchToConsensus` — fast-sync finished, switching to live consensus
- `Receive` (debug, with `src`, `chId`, `msg` fields)
- `Error decoding message`
- `Peer sent us invalid msg`
- `Ignoring message received during fastSync`
- `Sending block part`
- `Sending proposal`
- `Sending POL`
- `Peer ProposalBlockPartsHeader mismatch, sleeping`
- `Stopping gossipDataRoutine for peer`
- `Stopping gossipVotesRoutine for peer`
- `Stopping queryMaj23Routine for peer`

The `Receive` debug messages on channel `35` carry `VoteSetBits` (`VSB`) messages:
`[VSB H/RR/type hash BA{N:bitstring}]`. Tracking these reveals which validators a peer
believes have voted, and whether the same stale bit array repeats — a sign of stuck consensus.

#### Blockchain reactor (fast-sync)

From `tm2/pkg/bft/blockchain/reactor.go`:

- `Starting BlockchainReactor` / `Starting BlockPool` — fast-sync begin
- `Stopping BlockPool` — fast-sync end
- `Time to switch to consensus reactor!` — fast-sync completed, about to call SwitchToConsensus
- `Fast Sync Rate` (debug, with current height and peers) — periodic fast-sync progress
- `Error in validation` (error, with `err` field) — block received from a peer failed commit validation; immediately followed by `Stopping peer for error` with `BlockchainReactor validation error: invalid commit -- wrong block id: want X got Y`
- `Peer asking for a block we don't have` — a peer requested a height this node does not hold

When multiple peers are stopped for the same `wrong block id` in fast-sync, it indicates a possible chain fork or that this node has divergent state relative to the majority of the network.

#### P2P

From `tm2/pkg/p2p/switch.go` and `tm2/pkg/p2p/discovery/discovery.go`:

- `Added peer`
- `Stopping peer for error`
- `dialing peer`
- `unable to dial peer`
- `unable to add peer`
- `Ignoring inbound connection: already have enough inbound peers`
- `Ignoring inbound connection: error while adding peer`
- `ignoring dial request: already have max outbound peers` (with `have` and `max` fields)
- `ignoring dial request for existing peer`
- `no peers to share in discovery request`
- `error encountered during peer connection accept`
- `Error starting peer`

#### Node startup

From `tm2/pkg/bft/node/node.go`:

- `RPC+P2P running. Waiting for genesis time to start consensus...`
- `Starting Node`
- `Stopping Node`
- `Genesis time is in the future. Starting RPC+P2P early`

#### State

From `tm2/pkg/bft/state/execution.go`:

- `Updates to validators` — validator set changed; used to reconcile identity across log sections

#### Remote signer

From `tm2/pkg/bft/privval/signer/remote/client/`:

- `Failed to dial` (warn, with `protocol`, `address`, `error`)
- `Retrying to connect` (info, with `try`, `maxRetry`)
- `Dial succeeded` (debug)
- `Connected to server` (info, with `protocol`, `address`)
- `Already connected to server` (debug)
- `Sign request succeeded` (debug)
- `Sign request failed` (error, with `error`)

#### Configuration errors

Lines that appear before the first structured log entry and indicate misconfiguration:

- `unable to update config field, field "<name>", is not a valid configuration key, available keys: [...]`
- `Default configuration initialized at ...`
- `Updated configuration saved at ...`

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
- were there config key errors before startup (unrecognized config fields)
- did the node start in fast-sync mode (`fastSync: true`) and did it complete (`SwitchToConsensus`)
- did it wait for genesis time
- did it ever enter consensus
- did it ever finalize the first block
- what is the highest committed height seen
- is there a long period with no `Finalizing commit of block`
- is the chain halted globally or only on one node

Example findings:

- unrecognized config key at startup
- startup blocked before genesis time
- node never switched from fast-sync to consensus
- WAL stopped immediately after consensus panic (crash confirmed)
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
- repeated `Attempt to finalize failed. There was no +2/3 majority` (error level)
- `Commit is for a block we don't know about` — node entered commit phase with a block it never received
- proposer signs proposals but peers never complete block reception (`Received complete proposal block` missing)
- validator is a proposer but never signs a proposal on its turn
- no forward progress despite timeouts and round churn
- `CONSENSUS FAILURE!!!` present — a panic occurred; extract and display the stack trace
- `Error on ApplyBlock` — application-level failure
- `Found conflicting vote from ourselves` — possible double-signing or unsafe reset
- `enterPropose: Error signing proposal`
- `Error signing vote`
- `enterPropose: Cannot propose anything: No commit for the previous block.`
- rounds stuck at a high value (e.g., > 5) for multiple consecutive heights
- VoteSet bit arrays (`Added to prevote` / `Added to precommit`) that never reach +2/3

Derived diagnoses:

- consensus panic / crash
- proposer starvation
- quorum failure
- missing proposal block parts
- commit block unavailable locally
- validator not participating in consensus
- possible double-signing attempt

### 4. Reactor and block propagation checks

- proposal messages received but `Received complete proposal block` never appears
- block parts received from wrong height or round repeatedly
- reactor reports invalid or undecodable messages
- `VoteSetBits` messages repeating with the same stale bit array at the same height — stuck quorum
- fast-sync mode causes consensus messages to be ignored unexpectedly
- proposal block parts header mismatches recur
- gossip routines stopping unexpectedly
- during fast-sync: `Error in validation` followed by `Stopping peer for error` with `BlockchainReactor validation error: invalid commit -- wrong block id`; if multiple peers are ejected for the same mismatch, suspect chain fork or divergent local state

Derived diagnoses:

- proposal propagation failure
- block part mismatch
- malformed or incompatible peer traffic
- node stuck in fast-sync
- possible chain fork or corrupted local state during fast-sync

### 5. Peer connectivity and churn checks

- peer count over time by node
- zero-peer windows
- repeated dial failures to persistent peers
- repeated `Stopping peer for error`
- excessive churn relative to observation window
- `ignoring dial request: already have max outbound peers` with `max=1` — single-sentry topology: if the sentry disconnects, the validator has no fallback
- `no peers to share in discovery request` — the node believes it has no peers to advertise; combined with dial failures this indicates full network isolation
- inbound saturation or outbound saturation preventing recovery
- sentry has peers while validator has none
- all nodes lose peers at the same time

Derived diagnoses:

- validator isolation
- unstable peer connectivity
- persistent peer misconfiguration
- single-sentry single-point-of-failure topology
- network partition suspicion

### 6. Validator identity and role checks

- identify which logs belong to genesis validators
- identify non-validator nodes that still participate in relay traffic
- flag a "validator" log whose node reports `This node is not a validator`
- flag a node that reports `This node is a validator` during consensus but was not identified as one initially (identity confirmed during `Updates to validators`)
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

- remote signer dial failure before startup completes — normal if KMS starts late
- remote signer repeatedly failing during active consensus — signing stall
- high retry count before first successful connection
- `Sign request failed` during a height where the node was the proposer

### Time-skew checks

- detect suspicious clock skew across nodes from overlapping events
- flag ambiguous event ordering caused by skew
- `Need to set a buffer and log message here for sanity.` is emitted when `StartTime` is in the future relative to `now`, indicating clock skew between nodes

### Topology checks

- validator should rarely expose broad peer churn if traffic is supposed to arrive via sentries
- sentries connected to the network but validator isolated from sentries
- `max_outbound_peers=1` combined with `ignoring dial request: already have max outbound peers` means the validator relies on exactly one outbound connection; flag this as a resilience risk

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
- `--metadata <path>`
- `--generate-metadata <path>`
- `--node <name>=<path>`
- `--role <name>=<role>`
- `--format text|json`
- `--since <rfc3339>`
- `--until <rfc3339>`
- `--strict`
- `--verbose`
- `--max-findings <n>`

### Flag semantics

#### `--genesis <path>`

- required for `doctor inspect`
- path to the `genesis.json` used as the reference chain definition
- exactly one file
- parse failure is a hard input error with exit code `2`

#### `--log <path>`

- add a generic log source
- role is initially `unknown`
- may be repeated
- accepts file paths only in v1; compressed logs are not supported
- shell globbing is left to the shell, not implemented by the CLI itself

Use this when the operator has logs but does not want to classify them up front.

#### `--validator-log <path>`

- add a log source and assign role `validator`
- may be repeated
- same parsing rules as `--log`

This is a convenience flag equivalent to:

```sh
doctor inspect --log ./validator.log --role <inferred-or-bound-node>=validator
```

but without requiring the node name to be known ahead of time.

#### `--sentry-log <path>`

- add a log source and assign role `sentry`
- may be repeated
- same parsing rules as `--log`

This is mainly used to help topology-aware checks and to prevent sentry logs from being misinterpreted as validator logs.

#### `--metadata <path>`

- load an optional TOML metadata file
- may be repeated
- later metadata files override earlier ones on key conflicts
- metadata supplements logs; it does not silently replace contradictory log evidence

Supported uses:

- assign friendly node names
- assign or override node roles
- map validator pubkeys or addresses to friendly names
- describe validator-to-sentry topology
- map peer IDs to aliases

If metadata contradicts strong evidence from logs, `doctor` should emit a parser or validation warning. Under `--strict`, that becomes an input error.

#### `--generate-metadata <path>`

- write a TOML metadata file during `doctor inspect`
- exactly one output path
- intended to help the operator bootstrap a metadata file from partial information already visible in the supplied logs and genesis
- generation happens as part of inspection, not as a separate mode

Expected behavior:

- inspect logs and genesis as usual
- infer whatever identity information is available with reasonable confidence
- write a TOML file containing named nodes, roles, file bindings, detected node IDs, validator addresses or pubkeys when available, and empty sections where manual completion is expected
- keep user-supplied metadata untouched; this flag writes a new file, it does not rewrite existing metadata in place unless the target path is explicitly reused

Suggested use:

```sh
doctor inspect \
  --genesis ./genesis.json \
  --validator-log ./logs/validator.log \
  --sentry-log ./logs/sentry-a.log \
  --generate-metadata ./doctor-metadata.toml
```

Generated output should be safe to copy, edit, and feed back into a later run:

```sh
doctor inspect \
  --genesis ./genesis.json \
  --metadata ./doctor-metadata.toml \
  --log ./logs/*
```

Rules:

- if the output file already exists, require an explicit overwrite flag in a later revision or fail with an input error in v1
- generation should never downgrade analysis quality if writing the file fails; the inspection report should still be produced, with an added warning about metadata export failure

#### `--node <name>=<path>`

- bind a friendly node name to a specific log path
- may be repeated
- intended for cases where file names are not descriptive enough

Example:

```sh
doctor inspect \
  --genesis ./genesis.json \
  --node validator-1=./logs/node-a.log \
  --role validator-1=validator
```

If the same file is provided both with `--node` and `--log`, the tool should deduplicate the source and keep the explicit node name.

#### `--role <name>=<role>`

- assign a role to a named node
- allowed values in v1: `validator`, `sentry`, `seed`, `unknown`
- may be repeated

This only applies to nodes named through:

- `--node`
- metadata files
- identities inferred from logs and promoted into named nodes

If `--role` references an unknown node name, that is a validation error.

#### `--format text|json`

- select output renderer
- default: `text`

Behavior:

- `text`: concise operator-facing incident report
- `json`: stable machine-readable report for CI and tooling

#### `--since <rfc3339>`

- lower bound of the analysis window
- events strictly older than this timestamp are excluded from rule evaluation
- parsing still occurs before filtering so input quality warnings can still be reported

This is useful when supplied logs cover many hours but the incident window is known.

#### `--until <rfc3339>`

- upper bound of the analysis window
- events newer than this timestamp are excluded from rule evaluation
- same filtering rules as `--since`

If `--until` is earlier than `--since`, that is a validation error.

#### `--strict`

- make input hygiene and evidence quality enforceable
- default: `false`

When enabled, the command should still attempt analysis, but it must exit non-zero if any of the following remain unresolved:

- unsupported log format for any supplied file
- unparseable timestamps on lines needed for ordering or correlation
- unresolved node identity when a rule depends on node attribution
- contradictory metadata versus log-derived identity
- parser warnings above a configured threshold
- invalid flag combinations or references to unknown named nodes

Recommended exit behavior under `--strict`:

- exit `2` for input-quality failures
- exit `1` for successful analysis that found critical operational issues

In other words, `--strict` is for CI, automation, and disciplined incident workflows where ambiguous input should fail fast instead of producing a soft warning.

#### `--verbose`

- include low-severity findings, parser warnings, and supporting evidence that is omitted from the default text report
- default: `false`

`--verbose` affects presentation, not the analyzer itself.

#### `--max-findings <n>`

- cap the number of findings rendered in the `text` report
- default: `20`
- must be greater than `0`

This is a presentation limit, not an analysis limit:

- text output shows the top `n` findings by severity and confidence
- JSON output should still contain the full findings set unless a separate pagination flag is added later

## Report Design Requirements

- findings first, raw event dumps second
- evidence should reference node, timestamp, and message snippet
- missing data should be explicit
- avoid claiming a global halt when only one node log is present
- when multiple causes are plausible, rank them instead of pretending certainty

## Testing Strategy

### Unit tests

- genesis parsing
- JSON log parsing (with float64 Unix timestamps)
- console log parsing (ANSI codes, trailing JSON fields)
- container prefix stripping
- event normalization
- each analysis rule

### Golden tests

Add end-to-end test fixtures with:

- healthy startup and healthy block production
- no peers
- repeated timeouts with nil prevotes
- commit block missing locally (`Commit is for a block we don't know about`)
- repeated peer disconnects
- consensus panic (`CONSENSUS FAILURE!!!`)
- partial logs with low-confidence result
- fast-sync never completing
- fast-sync block validation errors (wrong block id from multiple peers)

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
- parse JSON and console logs, including container prefix stripping
- identify nodes and roles
- detect committed heights
- detect stalls, timeouts, zero-peer windows, peer churn
- detect repeated consensus-step failure patterns
- detect `CONSENSUS FAILURE!!!` panics
- detect remote signer failure patterns
- emit text and JSON reports

### Phase 2

Improve cross-node correlation:

- peer mapping
- sentry versus validator comparisons
- incident phase segmentation
- confidence scoring improvements
- VoteSet quorum tracking from bit arrays
- metadata-file based node and topology enrichment

### Phase 3

Optional deeper diagnostics:

- remote signer diagnosis
- topology inference
- richer proposer analysis
- optional RPC enrichment against a live or replayed endpoint

## Resolved Decisions

- remain offline-first in the initial design, with optional RPC enrichment added later
- support node identity from logs and from optional metadata files
- support console logs on a best-effort basis, with JSON logs recommended for reliable analysis
- do not support compressed logs in the first release
- implement the tool in `contribs/` immediately

## Recommendation

Build `doctor` as an offline-first contrib tool in Go, using the repository's existing command framework and a rule-based analyzer. The MVP should focus on the checks that provide the most operator value with partial logs:

- startup and first-block success
- highest committed height and halt point
- repeated prevote/precommit/timeout patterns
- missing commit block locally
- peer loss and churn
- reactor message failures
- consensus panics
- confidence reporting for incomplete evidence

That scope is narrow enough to implement incrementally and broad enough to be useful in real incidents.
