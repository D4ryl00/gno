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
- `zsh`
- sibling Gno checkout at `../gno`

## Build The Local Tooling Image

The scripts expect a local Docker image containing both `gnoland` and `gnokey`.

```bash
make build-image
```

By default the image tag is `gno-scenario-all:local`. Override it with `IMAGE=...` if needed.

## Run A Scenario

```bash
make scenario-01
make scenario-04
```

Each run writes generated node data, keys, genesis, and compose output under:

```bash
.work/<scenario-name>/
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

## Package Fixture

`packages/scenario-counter` contains a small realm used by the transaction scenario.

## Adding A New Scenario

The intended flow is:

1. `source` the shared library
2. declare validators / sentries with `gen_validator` and `gen_sentry`
3. call `prepare_network`
4. compose the scenario out of lifecycle and transaction helpers

See any file under `scenarios/` for examples.
