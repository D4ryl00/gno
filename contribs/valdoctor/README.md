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

## Recommendations

**Use JSON logs.** Set `log_format = json` in `config.toml` on your nodes.
Console logs are supported but field extraction is best-effort.

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
