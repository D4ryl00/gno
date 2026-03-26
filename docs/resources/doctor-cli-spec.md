# valdoctor — Offline Incident Inspection CLI

`valdoctor` is an offline-first incident inspection tool for Gnoland and TM2 logs.
It reads a `genesis.json`, one or more log files, and optional TOML metadata, then
produces a structured diagnosis report that identifies the most likely causes of
stalls, halts, peer issues, and consensus failures.

---

## Invocation

```sh
valdoctor inspect \
  --genesis ./genesis.json \
  --validator-log ./logs/validator.log \
  --sentry-log ./logs/sentry-a.log
```

### Log input flags

| Flag | Description |
|------|-------------|
| `--log <path>` | Generic log file; role inferred automatically. Repeatable. |
| `--validator-log <path>` | Log file explicitly tagged as a validator. Repeatable. |
| `--sentry-log <path>` | Log file explicitly tagged as a sentry. Repeatable. |
| `--node <name>=<path>` | Bind a friendly name to a log path. Repeatable. |
| `--role <name>=<role>` | Assign a role (`validator`, `sentry`, `seed`) to a named node. Repeatable. |

### Other flags

| Flag | Default | Description |
|------|---------|-------------|
| `--genesis <path>` | _(required)_ | Path to `genesis.json`. |
| `--metadata <path>` | — | TOML metadata file; repeatable. |
| `--generate-metadata <path>` | — | Write inferred metadata to a TOML file during inspection. Errors if the file already exists (exit code 2). |
| `--config <path>` | `$XDG_CONFIG_HOME/valdoctor/config.toml` | Path to a TOML config file. |
| `--format text\|json` | `text` | Output format. |
| `--since <RFC3339>` | — | Lower bound of the analysis window. |
| `--until <RFC3339>` | — | Upper bound of the analysis window. |
| `--verbose` / `-v` | false | Show low-severity findings and event details in the health summary. |
| `--show-unclassified` | false | Print unclassified log lines at the end of the report. |
| `--max-findings <n>` | 20 | Maximum findings rendered in text output. |
| `--max-health <n>` | 5 (0 in verbose) | Maximum nodes shown in the health summary. |

### Config management

```sh
valdoctor config init          # write default config to $XDG_CONFIG_HOME/valdoctor/config.toml
valdoctor config get [key]     # print one key or all config values as JSON
valdoctor config set <key> <value>
```

---

## Metadata file (TOML)

An optional TOML file enriches the analysis with topology and peer information
that cannot be inferred from logs alone.

```toml
version    = 1
chain_id   = "test5"

[nodes.validator-a]
role               = "validator"
files              = ["./logs/validator-a.log"]
node_id            = "abc123..."
validator_address  = "g1..."

[nodes.sentry-a]
role  = "sentry"
files = ["./logs/sentry-a.log"]

[topology]
[topology.validator_to_sentries]
validator-a = ["sentry-a", "sentry-b"]

[peer_aliases]
"abc123def456..." = "sentry-a"
```

---

## Log formats supported

- **JSON** (`{"level":"info","ts":...,"msg":"...","key":"value",...}`) — full field extraction
- **Console** (`2006-01-02T15:04:05Z INFO  message key=value`) — timestamp + level + KV pairs
- **Raw** (anything else) — message text only, no timestamp or fields

---

## Report structure

### Text output

```
=== valdoctor report ===

--- input ---
genesis: ...  chain_id: ...  validators: N
logs: N file(s) covering K node(s)
window: <start> → <end>

--- health summary ---
<node>  h<last_commit>  peers: <current>/<max>  [!N behind]  [fast-sync]
  prevotes: R/T [+2/3]  precommits: R/T [+2/3]
  round escalation: max_round=R at hH
  proposals signed: N
  remote signer unstable: failures=N reconnects=M

--- findings ---
[critical] <title>
  <summary>
  evidence: [<node>] <message>
  possible cause: ...
  suggested: ...
```

Findings are sorted by severity (critical → high → medium → low → info).
Colors are applied automatically when stdout is a TTY; suppressed by `NO_COLOR`.

### JSON output

Emits a single JSON object (`Report`) with:
- `input` — genesis, chain ID, validator count, log file count, time window
- `nodes` — per-node summaries (see below)
- `findings` — sorted list of `Finding` objects
- `warnings` — parser warnings and unsupported log patterns
- `metadata_generated_path` — set when `--generate-metadata` was used
- `confidence_too_low` — true when analysis is based on a single node's logs
- `critical_issues_detected` — true when any finding has severity `critical`

---

## Node summary fields

Each node entry in the report includes:

| Field | Description |
|-------|-------------|
| `name`, `role`, `files` | Identity |
| `start`, `end` | Log window timestamps |
| `event_count` | Total classified events |
| `highest_commit`, `commit_count` | Last committed height and total commits observed |
| `timeout_count` | Consensus timeout events |
| `max_peers`, `current_peers` | Peak and final peer counts |
| `last_height`, `last_round`, `last_step` | Last known consensus state |
| `last_commit_time`, `avg_block_time_ns`, `stall_duration_ns` | Timing metrics |
| `vote_state_height`, `prevotes_*`, `precommits_*` | VoteSet quorum state from debug logs |
| `joined_via_fast_sync`, `fast_sync_switch_height` | Fast-sync transition |
| `proposal_signed_count` | Proposals signed by this node |
| `max_round_seen`, `max_round_height` | Highest round reached at any single height |
| `signer_failure_count`, `signer_connect_count` | Remote signer stability |
| `dial_failure_count` | Outbound P2P dial failures |
| `has_debug_logs` | Whether any `debug`-level log lines were observed |

---

## Classified events

| Kind | Trigger phrase |
|------|---------------|
| `added_peer` | "Added peer" |
| `stopping_peer` | "Stopping peer" |
| `dial_failure` | "unable to dial peer" |
| `max_outbound_peers` | "Maximum number of outbound peers reached" |
| `no_peers_to_share` | "No addresses to send" |
| `timeout` | "Timed out" |
| `switch_to_consensus` | "Switched to consensus" / "SwitchToConsensus" |
| `prevote_proposal_nil` | "Prevote step" with nil proposal |
| `precommit_no_maj23` | "Precommit step" without +2/3 |
| `finalize_no_maj23` | "Finalize commit" without +2/3 |
| `commit_block_missing` | "Commit is for a block we don't have" |
| `finalize_commit` | "Finalizing commit" |
| `consensus_failure` | "CONSENSUS FAILURE!!!" |
| `conflicting_vote` | "Found conflicting vote" |
| `apply_block_error` | "Error on applying block" |
| `node_not_validator` | "This node is not a validator" |
| `signed_proposal` | "Signed proposal" |
| `remote_signer_failure` | "RemoteSigner" failure |
| `remote_signer_connected` | "RemoteSigner" connected |
| `received_complete_proposal_block` | "Received complete proposal block" |
| `fastsync_block_validation_error` | "Block validation failure" (fast-sync) |
| `added_prevote` | "Added to prevote" |
| `added_precommit` | "Added to precommit" |
| `commit_unknown_block` | "Commit is for an unknown block" |
| `commit_locked_block` | "Commit is for locked block" |
| `unexpected_block_part` | "Received a block part when we're not expecting any" |
| `add_vote_error` | "Error attempting to add vote" |
| `config_error` | Configuration-level warnings |
| `parser_warning` | Parser could not classify or parse the line |

VoteSet quorum state (`prevotes_*`, `precommits_*`) is extracted from debug-level
`VoteSet{H:… +2/3:… BA{N:…}}` strings attached to `added_prevote`/`added_precommit` events.

---

## Findings catalogue

### Global / cross-node

| ID | Severity | Trigger |
|----|----------|---------|
| `genesis-no-validators` | critical | Genesis has no validators |
| `validator-height-divergence` | high | Validators disagree on committed height by > 1 |
| `forward-progress` | info | All nodes are making commits; no problems detected |
| `parser-warnings` | low | Parser warnings were emitted |
| `no-proposal-signed-at-h<H>` | high | At apparent stall height, no node signed a proposal |
| `proposal-not-propagated-h<H>` | high | A proposal was signed but no peer received the block |
| `clock-skew-<A>-<B>` | medium | Two nodes committed the same height ≥ 5 s apart |
| `validator-isolated-despite-sentry` | high | Validator has 0 peers while sentries are connected (topology-aware) |
| `validator-isolated-from-sentry-<V>-<S>` | high | Specific validator–sentry pair both have 0 peers |

### Per-node

| ID | Severity | Trigger |
|----|----------|---------|
| `consensus-panic-<node>` | critical | `CONSENSUS FAILURE!!!` |
| `apply-block-error-<node>` | critical | Block application error |
| `conflicting-vote-<node>` | critical | Double-sign / conflicting vote |
| `validator-address-mismatch-<node>` | critical | "invalid validator address" in vote error |
| `remote-signer-failure-<node>` | high / critical | Remote signer failures; escalates to critical when no proposals were signed |
| `stall-after-last-commit-<node>` | high | No commits for > max(30 s, 5 × avg block time) after last commit |
| `finalize-no-maj23-<node>` | high | Repeated +2/3 quorum failures at finalization |
| `no-maj23-<node>` | high | Repeated precommit +2/3 failures |
| `missing-commit-block-<node>` | high | Node repeatedly cannot find the commit block |
| `proposal-block-nil-<node>` | high | Node repeatedly prevotes nil due to absent proposal |
| `validator-no-first-commit-<node>` | high | Validator never committed a single block |
| `fastsync-block-error-<node>` | high | Block validation error during fast-sync |
| `node-not-validator-<node>` | medium | Node reports it is not a validator |
| `peer-starvation-<node>` | medium | Peer count stayed at 0 throughout the log window |
| `max-outbound-peers-low-<node>` | medium | `max_outbound_peers` ≤ 2 and the cap was hit |
| `validator-isolated-from-sentry-<V>-<S>` | high | Validator–sentry pair both isolated |
| `round-escalation-<node>` | medium | Consensus reached round ≥ 3 at a single height |
| `dial-failures-<node>` | medium | ≥ 5 outbound dial failures |
| `config-error-<node>` | low | Configuration-level warning in logs |

### Confidence and scope

- Findings have confidence `low`, `medium`, or `high`.
- Global findings (scope `"global"`) are downgraded from `high` to `medium` when
  only one node contributed events (single-node coverage).
- The stall finding uses `low` confidence when only the stalled node has events
  covering the stall window.

---

## Stall analysis (cross-node correlation)

When a node's last commit is followed by > threshold silence, `valdoctor` inspects
the stall window (events after the last commit, by timestamp or line-number order):

1. **Peer isolation** — checks `current_peers == 0 && max_peers > 0`.
2. **Node crash** — checks for `consensus_failure` or `apply_block_error` events in the window; height is inferred from the last consensus event before the panic if the panic line itself carries no height.
3. **Quorum failure** — counts `finalize_no_maj23` + `precommit_no_maj23` events.
4. **No proposal** — counts `prevote_proposal_nil` events.
5. **Missing commit block** — counts `commit_block_missing` events.
6. **Remote signer unavailable** — `signer_failure_count > 0 && proposal_signed_count == 0`.
7. **Other validators also failed** — checks all other nodes (validators and unknown-role) whose logs cover the stall window; if they also didn't commit the next height, they are listed as corroborating evidence with their reason (crashed / stalled).

When no specific cause is found:
- If `has_debug_logs` is true: says "cause not determinable even with debug-level logs".
- Otherwise: suggests enabling debug-level logging.

---

## Proposer analysis

At the apparent stall height (the height with the most `prevote_proposal_nil` events,
minimum 2 occurrences), `valdoctor` checks:

- **No proposal signed anywhere** → finding `no-proposal-signed-at-h<H>`: the proposer was absent, could not connect to its remote signer, or is not included in the provided logs.
- **Proposal signed but not received** → finding `proposal-not-propagated-h<H>`: a node signed the proposal but no peer received the complete block parts.

---

## Remote signer diagnosis

`valdoctor` tracks `signer_failure_count` and `signer_connect_count` per node.

- **Cycling** (`failures ≥ 2 && reconnects ≥ 1`): the KMS connection was unstable.
- **Never signed** (`proposal_signed_count == 0 && role == validator`): severity escalates to `critical`.
- **Health summary** shows `remote signer unstable: failures=N reconnects=M` when failures are present.

---

## Phase summary

| Phase | Features |
|-------|----------|
| 1 | Log parsing (JSON, console, raw); event classification; per-node health summary; core findings (panic, quorum, peers, fast-sync, conflicting vote, apply-block error) |
| 2 | Cross-node correlation; peer mapping; sentry vs validator comparison; incident phase segmentation; confidence scoring; VoteSet quorum tracking from bit arrays; metadata-based topology enrichment |
| 3 | Remote signer reconnection-loop detection; round escalation detection; repeated dial-failure detection; proposer absence / propagation-failure analysis; clock-skew detection across nodes |
