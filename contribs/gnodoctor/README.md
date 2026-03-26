# gnodoctor

`gnodoctor` is an offline-first incident inspection tool for Gnoland and TM2 logs.

It analyzes:

- a `genesis.json`
- validator logs
- sentry logs
- optional TOML metadata

and produces a diagnosis report that highlights likely causes of stalls, halts, peer issues, and consensus problems.

## Example

```sh
gnodoctor inspect \
  --genesis ./genesis.json \
  --validator-log ./logs/validator.log \
  --sentry-log ./logs/sentry-a.log \
  --format text
```

To bootstrap metadata during inspection:

```sh
gnodoctor inspect \
  --genesis ./genesis.json \
  --log ./logs/* \
  --generate-metadata ./doctor-metadata.toml
```

If `./doctor-metadata.toml` already exists, `gnodoctor` exits with code `2` instead of silently reusing a stale file.
