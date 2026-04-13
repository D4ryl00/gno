# Gnoland Validator Scenario Harness

This repo generates local Gnoland validator networks in Docker and runs scripted failure / recovery scenarios against them.

It is inspired by `../gno-val-test`, but the setup here is reusable and scenario-driven:

- each validator or sentry runs in its own container
- the network is generated from a small Bash DSL
- scenarios can stop, restart, and reset nodes
- scenarios can deploy realms and submit transactions with `gnokey`
- sentry-based topologies are supported, including sentry container recreation to force a new container IP while validators keep dialing the same DNS name

## Prerequisites

- `docker`
- `docker compose`
- `jq`
- `curl`
- `bash` (4+)

## Build The Local Tooling Images

The scripts expect local Docker images for `gnoland`, `gnokey`, and `gnogenesis`.

```bash
make build-images
```

By default the tags are `gnoland:local`, `gnokey:local`, and `gnogenesis:local`.
Override them with `IMAGE=...`, `GNOKEY_IMAGE=...`, and `GNOGENESIS_IMAGE=...` if needed.

## Run A Scenario

```bash
make scenario-01
make scenario-04
```

Each run writes generated node data, keys, genesis, and compose output under:

```bash
/tmp/gno-val-tests/<scenario-name>/
```

By default the scenario tears containers down on exit but keeps the generated data. To keep the network running after the script exits:

```bash
KEEP_UP=1 ./scenarios/05_sentry_ip_rotation.sh
```

## Available Scenarios

- `01_five_validators_reset_four.sh`: start 5 validators, run 60s, stop/reset 4, restart them, run 60s again
- `02_four_validators_restart_staggered.sh`: start 4 validators, stop all after 60s, restart one by one
- `03_four_validators_restart_parallel.sh`: start 4 validators, stop all after 60s, restart all together
- `04_counter_realm_churn.sh`: deploy a sample counter realm, submit transactions, reset one validator, continue submitting txs
- `05_sentry_ip_rotation.sh`: run validators behind a sentry, recreate the sentry to force a new container IP, and verify the network keeps progressing
- `06_gas_nondeterminism_check.sh`: restart a subset of validators, estimate addpkg gas on a warm node, and fail if the chain halts after the trigger tx
- `07_five_validators_reset_one.sh`: start 5 validators, stop/reset/restart 1 — 4/5 remain above the 2/3 threshold so the chain must keep advancing throughout
- `08_five_validators_reset_two_below_consensus.sh`: start 5 validators, stop/reset 2 — 3/5 drops below the 2/3 threshold so the chain must halt, then verify it resumes after both validators are restarted
- `09_five_validators_safe_reset_one.sh`: same as 07 but uses a safe reset (db + wal only, `priv_validator_state` preserved) to avoid double signing
- `10_five_validators_safe_reset_two_below_consensus.sh`: same as 08 but uses a safe reset
- `11_weighted_voting_power_majority.sh`: 4 validators with voting power 10/1/1/1 — val1 alone holds >2/3 of total power, so stopping val2–4 must not halt the chain
- `12_duplicate_addr_in_val_proposal.sh`: governance proposal with two entries for the same validator address (VotingPower=0 then VotingPower=5) — expected to end with the validator at power 5, but currently fails due to a bug (**tracking scenario**: should pass once the bug is fixed)

## Reusable Scenario API

Scenarios source `lib/scenario.sh` and use a small set of helpers:

- `scenario_init <name>`
- `gen_validator <name> [--rpc-port <port>] [--sentry <sentry-name>]`
- `gen_sentry <name> [--rpc-port <port>]`
- `prepare_network`
- `start_all_nodes`
- `start_validator <name>`
- `stop_validator <name>`
- `reset_validator <name>`
- `wait_for_seconds <n>`
- `wait_for_blocks <node> <delta> <timeout>`
- `add_pkg <target-node> <pkgdir> <pkgpath>`
- `call_realm <target-node> <pkgpath> <func> [args...]`
- `do_transaction addpkg|call|run|send ...`
- `rotate_sentry_ip <sentry-name>`
- `print_cluster_status`

`wait_for_seconds` is used instead of `wait` to avoid colliding with Bash’s built-in `wait`.

## Adding A New Scenario

Scenario files must follow the naming pattern `NN_<name>.sh` (e.g. `13_my_new_scenario.sh`), where `NN` is a zero-padded two-digit number. The Makefile auto-discovers files matching `scenarios/*.sh` and generates `scenario-NN`, `logs-NN`, and `clean-NN` targets from the numeric prefix. The script must call `scenario_init "scenario-NN"` with the matching number so that the docker-compose project name and work directory align with those targets.

The intended flow is:

1. name the file `NN_<name>.sh` and call `scenario_init "scenario-NN"`
2. `source` the shared library
3. declare validators / sentries with `gen_validator` and `gen_sentry`
4. call `prepare_network`
5. compose the scenario out of lifecycle and transaction helpers

See any file under `scenarios/` for examples.
