# valdoctor

Offline incident inspection for Gnoland and TM2 validators. Give it a `genesis.json`
and your logs; it tells you what went wrong.

## Install

```sh
make install
```

## Usage

```sh
valdoctor inspect \
  --genesis ./genesis.json \
  --validator-log ./logs/validator.log \
  --sentry-log ./logs/sentry-a.log
```

If you don't know the role of each file, use `--log` for everything:

```sh
valdoctor inspect --genesis ./genesis.json --log ./logs/*
```

For scripting or incident pipelines:

```sh
valdoctor inspect --genesis ./genesis.json --log ./logs/* --format json
```

## Sample output

```
=== valdoctor report ===

chain: test5  validators: 7  logs: 3 file(s)  window: 14:35:00Z → 14:55:46Z

Health summary
- validator-a  peer count max=6 current=0  stalled 10m20s
- sentry-a     peer count max=4 current=4
- validator-b  remote signer unstable: failures=3 reconnects=2

Consensus state (end of window)
- validator-a [validator] height=19497 round=0
  prevotes: 4/7  precommits: 4/7
- sentry-a    [sentry]    height=19498 round=3
  round escalation: max_round=3 at h19498

Findings

[critical] consensus-panic-validator-b: Consensus panic on validator-b
  A CONSENSUS FAILURE!!! panic was logged. The node process terminated.
  evidence: [validator-b] CONSENSUS FAILURE!!! err=runtime error: ...
  possible cause: node joined consensus mid-round via fast-sync without the proposal block
  suggested: restart the node after resolving the underlying issue

[high] stall-after-last-commit-validator-a: validator-a: no commits for 10m20s after height 19497
  Last commit at h19497; no further commits observed for 10m20s. Average block time was 2s.
  evidence: [validator-b] consensus panic at h19498
  possible cause: quorum loss caused by other validators also failing at h19498:
    1 crashed (validator-b) — if their combined voting power exceeds 1/3, consensus cannot proceed
  suggested: investigate why 1 other validator(s) also failed at h19498

[medium] round-escalation-sentry-a: sentry-a reached round 3 at height 19498
  Consensus at height 19498 required at least 3 round(s) before committing or stalling.
  suggested: examine logs from all validators around height 19498
```

## Recommendations

**Use JSON logs.** Pass `--log-format json` to `gnoland start`.
Console logs are supported but field extraction is best-effort.

**Enable debug-level logging.** Pass `--log-level debug` to `gnoland start` to get VoteSet quorum
tracking (how many prevotes and precommits were received per round) and richer
consensus state in the report. Without it, quorum analysis falls back to
coarser event counts.

**Provide logs from all validators when possible.** Many findings are downgraded
to lower confidence when only one node's logs are available. Cross-node correlation
(stall root cause, quorum loss attribution) requires at least two nodes.

**Use a metadata file for recurring incidents.** The first time, let `valdoctor`
generate one from your logs:

```sh
valdoctor inspect \
  --genesis ./genesis.json \
  --log ./logs/* \
  --generate-metadata ./valdoctor-meta.toml
```

Edit it to add topology (which sentries serve which validators) and re-use it:

```sh
valdoctor inspect \
  --genesis ./genesis.json \
  --metadata ./valdoctor-meta.toml \
  --log ./logs/*
```

This unlocks topology-aware findings (e.g. "validator-a lost its connection to
sentry-b while sentry-b remained reachable").

**Narrow the window when logs are large.** Use `--since` and `--until` to focus
on the incident:

```sh
valdoctor inspect \
  --genesis ./genesis.json \
  --log ./logs/* \
  --since 2026-03-20T14:00:00Z \
  --until 2026-03-20T15:00:00Z
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | No critical issue |
| 1 | At least one critical finding |
| 2 | Input error |
| 3 | Too few classifiable events to draw conclusions |

## Config

```sh
valdoctor config init            # write default config
valdoctor config set format json # persist a flag default
```

See `docs/resources/doctor-cli-spec.md` for the full specification.
