# Gnoland Validator Scenario Harness

This repo generates local Gnoland validator networks in Docker and runs scripted failure / recovery scenarios against them.

It is inspired by `../gno-val-test`, but the setup here is reusable and scenario-driven:

- each validator or sentry runs in its own container
- validators can optionally run with a controllable remote-signer sidecar
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

The scripts expect local Docker images for `gnoland`, `gnokey`, `gnogenesis`, and `valsignerd`.

```bash
make build-images
```

By default the tags are `gnoland:local`, `gnokey:local`, `gnogenesis:local`, and `valsignerd:local`.
Override them with `IMAGE=...`, `GNOKEY_IMAGE=...`, `GNOGENESIS_IMAGE=...`, and `VALSIGNER_IMAGE=...` if needed.

To build images from a GitHub fork, set `GH_USER`. `GH_REPO` defaults to `gno` and `GH_BRANCH` defaults to `master`. Image tags are derived automatically as `<base>:<GH_USER>-<GH_BRANCH>` (slashes in the branch name become dashes), so multiple versions can coexist without overwriting each other.

```bash
make build-images GH_USER=gnolang
# → gnoland:gnolang-master, gnokey:gnolang-master, gnogenesis:gnolang-master, valsignerd:gnolang-master

make build-images GH_USER=gnolang GH_REPO=gno GH_BRANCH=feat/my-branch
# → gnoland:gnolang-feat-my-branch, gnokey:gnolang-feat-my-branch, gnogenesis:gnolang-feat-my-branch, valsignerd:gnolang-feat-my-branch
```

The repository is cloned once to `/tmp/gno-remote-build` and reused across subsequent builds. To force a fresh clone, run `make fetch-remote` with the same variables.

To run a scenario against a previously built fork image, pass the matching tag:

```bash
make scenario-12 GH_USER=gnolang GH_BRANCH=feat/my-branch
```

## Run A Scenario

```bash
make test              # run the CI/basic scenarios
make test-advanced     # run the advanced local-only scenarios
make test-all          # run both tiers
make scenario-01
make scenario-04
```

`make scenario-NN` still works for any individual scenario regardless of folder.

Each run writes generated node data, keys, genesis, and compose output under:

```bash
/tmp/gno-val-tests/<scenario-name>/
```

By default the scenario tears containers down on exit but keeps the generated data. To keep the network running after the script exits:

```bash
KEEP_UP=1 ./advanced/05_sentry_ip_rotation.sh
```

## Scenario Tiers

The scenario scripts now live in two folders:

- `basics/`: the core validator-flow scenarios run by `.github/workflows/ci-val-scenarios.yml`
- `advanced/`: heavier, exploratory, or bug-tracking scenarios intended for local runs

### Basics

- `basics/02_four_validators_restart_staggered.sh`: start 4 validators, stop all after 60s, restart one by one
- `basics/03_four_validators_restart_parallel.sh`: start 4 validators, stop all after 60s, restart all together
- `basics/04_counter_realm_churn.sh`: deploy a sample counter realm, submit transactions, reset one validator, continue submitting txs
- `basics/07_five_validators_reset_one.sh`: start 5 validators, stop/reset/restart 1 — 4/5 remain above the 2/3 threshold so the chain must keep advancing throughout
- `basics/09_five_validators_safe_reset_one.sh`: same as 07 but uses a safe reset (db + wal only, `priv_validator_state` preserved) to avoid double signing
- `basics/10_five_validators_safe_reset_two_below_consensus.sh`: same as 08 but uses a safe reset
- `basics/11_weighted_voting_power_majority.sh`: 4 validators with voting power 10/1/1/1 — val1 alone holds >2/3 of total power, so stopping val2–4 must not halt the chain

### Advanced

- `advanced/01_five_validators_reset_four.sh`: start 5 validators, run 60s, stop/reset 4, restart them, run 60s again
- `advanced/05_sentry_ip_rotation.sh`: run validators behind a sentry, recreate the sentry to force a new container IP, and verify the network keeps progressing
- `advanced/06_gas_nondeterminism_check.sh`: restart a subset of validators, estimate addpkg gas on a warm node, and fail if the chain halts after the trigger tx
- `advanced/08_five_validators_reset_two_below_consensus.sh`: start 5 validators, stop/reset 2 — 3/5 drops below the 2/3 threshold so the chain must halt, then verify it resumes after both validators are restarted
- `advanced/12_duplicate_addr_in_val_proposal.sh`: governance proposal with two entries for the same validator address (VotingPower=0 then VotingPower=5) — expected to end with the validator at power 5, but currently fails due to a bug
- `advanced/13_duplicate_addr_across_proposals.sh`: two valid validator proposals in the same block target the same address — expected to converge on the last change, but currently crashes in EndBlocker due to duplicate aggregate changes
- `advanced/14_five_validators_drop_proposals_with_signers.sh`: 5 validators with controllable signer sidecars — drop proposal signatures on all validators live, assert the chain halts at a fixed height, clear the rules, assert consensus resumes without restarting nodes
- `advanced/15_four_validators_drop_prevotes_thresholds.sh`: 4 validators with controllable signer sidecars — drop prevotes on 1 validator and assert the chain keeps advancing, then drop prevotes on 3/4 validators and assert the chain halts
- `advanced/16_four_validators_precommit_delays_thresholds.sh`: 4 validators with controllable signer sidecars — progressively delay precommits below and above `timeout_commit`, assert the chain still advances while a quorum can eventually form, then push two validators past the observation window and assert block production stalls

## Reusable Scenario API

Scenarios source `lib/scenario.sh` and use a small set of helpers:

- `scenario_init <name>`
- `gen_validator <name> [--rpc-port <port>] [--sentry <sentry-name>] [--controllable-signer]`
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
- `signer_state <validator>`
- `signer_drop <validator> proposal|prevote|precommit [height] [round]`
- `signer_delay <validator> proposal|prevote|precommit <duration> [height] [round]`
- `signer_clear <validator> [phase]`
- `rotate_sentry_ip <sentry-name>`
- `print_cluster_status`

`wait_for_seconds` is used instead of `wait` to avoid colliding with Bash’s built-in `wait`.

## Controllable Signers

Pass `--controllable-signer` to `gen_validator` to launch a `valsignerd` sidecar for that validator. The validator itself still runs stock `gnoland`; only the signing path is redirected through the sidecar via the existing remote-signer configuration.

Each controllable validator gets:

- a sidecar service named `<validator>-signer`
- an HTTP control API on host port `<validator-rpc-port + 1>`
- a remote signer endpoint inside the compose network at `tcp://<validator>-signer:26659`

`prepare_network` writes an inventory file at:

```bash
/tmp/gno-val-tests/<scenario-name>/inventory.json
```

That file lists validator RPC URLs and signer control URLs for use by an external cockpit.

The sidecar currently supports live rules for:

- drop proposal signatures
- drop prevote signatures
- drop precommit signatures
- delay proposal / prevote / precommit signatures
- optional height / round scoping

This approach does not modify vote contents or proposal contents. It controls whether a validator signs, and when.

Example live control commands against a running scenario:

```bash
# Inspect current signer state.
curl -fsS http://127.0.0.1:26658/state | jq

# Drop all precommits from val1.
curl -fsS -X PUT http://127.0.0.1:26658/rules/precommit \
  -H 'Content-Type: application/json' \
  -d '{"action":"drop"}'

# Delay only round 0 prevotes at height 25 by 8 seconds.
curl -fsS -X PUT http://127.0.0.1:26658/rules/prevote \
  -H 'Content-Type: application/json' \
  -d '{"action":"delay","delay":"8s","height":25,"round":0}'

# Clear just the precommit rule.
curl -fsS -X DELETE http://127.0.0.1:26658/rules/precommit

# Clear all rules on that signer.
curl -fsS -X POST http://127.0.0.1:26658/reset
```

## Adding A New Scenario

Scenario files must follow the naming pattern `NN_<name>.sh` (e.g. `13_my_new_scenario.sh`), where `NN` is a zero-padded two-digit number. Place the file under `basics/` if it is suitable for GitHub Actions CI, or under `advanced/` if it is intended for local-only or exploratory runs. The Makefile auto-discovers files matching `basics/*.sh` and `advanced/*.sh`, and generates `scenario-NN`, `logs-NN`, and `clean-NN` targets from the numeric prefix. The script must call `scenario_init "scenario-NN"` with the matching number so that the docker-compose project name and work directory align with those targets.

The intended flow is:

1. place the file in `basics/` or `advanced/`, and call `scenario_init "scenario-NN"`
2. `source` the shared library
3. declare validators / sentries with `gen_validator` and `gen_sentry`
4. call `prepare_network`
5. compose the scenario out of lifecycle and transaction helpers

See any file under `basics/` or `advanced/` for examples.
