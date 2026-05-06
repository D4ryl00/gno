#!/usr/bin/env bash
set -euo pipefail

if [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
  printf 'error: bash 4+ required (found %s); install with: brew install bash\n' "$BASH_VERSION" >&2
  exit 1
fi

SCENARIO_SELF="${BASH_SOURCE[0]}"
SCENARIO_LIB_DIR="$(cd "$(dirname "${SCENARIO_SELF}")" && pwd)"
REPO_ROOT="$(cd "${SCENARIO_LIB_DIR}/../../.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-gno-val-scenario-core:local}"
GNOKEY_IMAGE="${GNOKEY_IMAGE:-${IMAGE_NAME}}"
GNOGENESIS_IMAGE="${GNOGENESIS_IMAGE:-gnogenesis:local}"
VALSIGNER_IMAGE="${VALSIGNER_IMAGE:-valsignerd:local}"
GNO_ROOT="${GNO_ROOT:-${REPO_ROOT}}"
WORK_ROOT="${WORK_ROOT:-/tmp/gno-val-tests}"
CHAIN_ID="${CHAIN_ID:-dev}"
TIMEOUT_COMMIT="${TIMEOUT_COMMIT:-1s}"
LOG_LEVEL="${LOG_LEVEL:-info}"
REMOTE_SIGNER_REQUEST_TIMEOUT="${REMOTE_SIGNER_REQUEST_TIMEOUT:-30s}"
GNOKMS_KEYBASE_PASSWORD="${GNOKMS_KEYBASE_PASSWORD:-scenario}"
GNOKMS_KEYBASE_KEY_NAME="${GNOKMS_KEYBASE_KEY_NAME:-validator}"
GNOKMS_LEDGER_BIN="${GNOKMS_LEDGER_BIN:-/tmp/gnokms-ledger}"
GNOKMS_LEDGER_HOST_PORT="${GNOKMS_LEDGER_HOST_PORT:-26660}"
GNOKMS_LEDGER_TIMEOUT="${GNOKMS_LEDGER_TIMEOUT:-60}"
TX_KEY_NAME="${TX_KEY_NAME:-scenario-tx}"
TX_PASSWORD="${TX_PASSWORD:-test123456}"
TX_MNEMONIC="${TX_MNEMONIC:-source bonus chronic canvas draft south burst lottery vacant surface solve popular case indicate oppose farm nothing bullet exhibit title speed wink action roast}"
TX_ADDRESS="${TX_ADDRESS:-g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5}"
TX_BALANCE="${TX_BALANCE:-100000000000ugnot}"
TX_GAS_FEE="${TX_GAS_FEE:-1000000ugnot}"
TX_GAS_WANTED_ADD_PKG="${TX_GAS_WANTED_ADD_PKG:-50000000}"
TX_GAS_WANTED_CALL="${TX_GAS_WANTED_CALL:-3000000}"
TX_GAS_WANTED_RUN="${TX_GAS_WANTED_RUN:-5000000}"
TX_GAS_WANTED_SEND="${TX_GAS_WANTED_SEND:-2000000}"

declare -a SCENARIO_NODES=()
declare -a SCENARIO_VALIDATORS=()
declare -a SCENARIO_GENESIS_VALIDATORS=()
declare -a SCENARIO_SENTRIES=()
declare -a SCENARIO_SIGNERS=()
declare -A NODE_ROLE=()
declare -A NODE_SERVICE=()
declare -A NODE_MONIKER=()
declare -A NODE_RPC_PORT=()
declare -A NODE_PEX=()
declare -A NODE_SENTRY=()
declare -A NODE_ID=()
declare -A NODE_ADDRESS=()
declare -A NODE_PUBKEY=()
declare -A NODE_DATA_DIR=()
declare -A NODE_POWER=()
declare -A NODE_CONTROLLABLE_SIGNER=()
declare -A NODE_SIGNER_SERVICE=()
declare -A NODE_CONTROL_PORT=()
declare -A NODE_GNOKMS_BACKED=()
declare -A NODE_GNOKMS_SERVICE=()
declare -A NODE_LEDGER_BACKED=()
declare -A NODE_LEDGER_HOST_PORT=()
declare -A NODE_LEDGER_GNOKMS_PID=()
declare -A NODE_LEDGER_LOG_FILE=()
declare -A NODE_LOG_PID=()

SCENARIO_NAME=""
PROJECT_NAME=""
SCENARIO_DIR=""
COMPOSE_FILE=""
KEY_HOME=""
NETWORK_NAME=""

log() {
  printf '[%s] %s\n' "${SCENARIO_NAME:-scenario}" "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

dump_ledger_log() {
  local node="$1" logf="$2"
  printf -- '--- last lines of host gnokms-ledger log for %s (%s) ---\n' "$node" "$logf" >&2
  if [ -s "$logf" ]; then
    tail -n 30 "$logf" >&2
  else
    printf '(empty log — gnokms-ledger produced no output before exiting)\n' >&2
  fi
  printf -- '--- end log ---\n' >&2
}

join_by() {
  local delimiter="${1:?delimiter required}"
  shift || true
  local out=""
  local first=1
  local value
  for value in "$@"; do
    if [ "$first" -eq 1 ]; then
      out="$value"
      first=0
    else
      out="${out}${delimiter}${value}"
    fi
  done
  printf '%s' "$out"
}

slugify() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-'
}

require_tools() {
  local missing=()
  local tool
  for tool in docker jq curl; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      missing+=("$tool")
    fi
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    die "missing required tools: $(join_by ', ' "${missing[@]}")"
  fi
}

scenario_init() {
  local name="${1:?scenario name required}"

  SCENARIO_NAME="$name"
  PROJECT_NAME="$(slugify "$name")"
  SCENARIO_DIR="${WORK_ROOT}/${PROJECT_NAME}"
  COMPOSE_FILE="${SCENARIO_DIR}/docker-compose.yml"
  KEY_HOME="${SCENARIO_DIR}/keys"
  NETWORK_NAME="${PROJECT_NAME}_chain"

  log "scenario dir: ${SCENARIO_DIR}"

  SCENARIO_NODES=()
  SCENARIO_VALIDATORS=()
  SCENARIO_GENESIS_VALIDATORS=()
  SCENARIO_SENTRIES=()
  SCENARIO_SIGNERS=()
  NODE_ROLE=()
  NODE_SERVICE=()
  NODE_MONIKER=()
  NODE_RPC_PORT=()
  NODE_PEX=()
  NODE_SENTRY=()
  NODE_ID=()
  NODE_ADDRESS=()
  NODE_PUBKEY=()
  NODE_DATA_DIR=()
  NODE_POWER=()
  NODE_CONTROLLABLE_SIGNER=()
  NODE_SIGNER_SERVICE=()
  NODE_CONTROL_PORT=()
  NODE_GNOKMS_BACKED=()
  NODE_GNOKMS_SERVICE=()
  NODE_LEDGER_BACKED=()
  NODE_LEDGER_HOST_PORT=()
  NODE_LEDGER_GNOKMS_PID=()
  NODE_LEDGER_LOG_FILE=()
  NODE_LOG_PID=()
}

register_node() {
  local name="${1:?node name required}"
  local role="${2:?role required}"
  local rpc_port="${3:-}"
  local pex="${4:?pex required}"
  local sentry="${5:-}"
  local in_genesis="${6:-true}"

  [ -z "${NODE_ROLE[$name]:-}" ] || die "node ${name} already exists"

  SCENARIO_NODES+=("$name")
  NODE_ROLE[$name]="$role"
  NODE_SERVICE[$name]="$name"
  NODE_MONIKER[$name]="$name"
  NODE_RPC_PORT[$name]="$rpc_port"
  NODE_PEX[$name]="$pex"
  NODE_SENTRY[$name]="$sentry"

  case "$role" in
    validator)
      SCENARIO_VALIDATORS+=("$name")
      if [ "$in_genesis" = "true" ]; then
        SCENARIO_GENESIS_VALIDATORS+=("$name")
      fi
      ;;
    sentry) SCENARIO_SENTRIES+=("$name") ;;
    *) die "unsupported node role ${role}" ;;
  esac
}

gen_validator() {
  local name="${1:?validator name required}"
  shift || true

  local rpc_port=""
  local sentry=""
  local pex="true"
  local power="1"
  local controllable_signer="false"
  local gnokms_backed="false"
  local ledger_backed="false"
  local in_genesis="true"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --rpc-port)
        rpc_port="${2:?missing rpc port}"
        shift 2
        ;;
      --sentry)
        sentry="${2:?missing sentry name}"
        pex="false"
        shift 2
        ;;
      --pex)
        pex="${2:?missing pex value}"
        shift 2
        ;;
      --power)
        power="${2:?missing power value}"
        shift 2
        ;;
      --controllable-signer)
        controllable_signer="true"
        shift
        ;;
      --gnokms-backed-signer)
        controllable_signer="true"
        gnokms_backed="true"
        shift
        ;;
      --ledger-backed-signer)
        controllable_signer="true"
        ledger_backed="true"
        shift
        ;;
      --not-in-genesis)
        in_genesis="false"
        shift
        ;;
      *)
        die "unknown gen_validator option: $1"
        ;;
    esac
  done

  if [ "$gnokms_backed" = "true" ] && [ "$ledger_backed" = "true" ]; then
    die "validator ${name}: --gnokms-backed-signer and --ledger-backed-signer are mutually exclusive"
  fi

  register_node "$name" validator "$rpc_port" "$pex" "$sentry" "$in_genesis"
  NODE_POWER[$name]="$power"
  NODE_CONTROLLABLE_SIGNER[$name]="$controllable_signer"
  NODE_GNOKMS_BACKED[$name]="$gnokms_backed"
  NODE_LEDGER_BACKED[$name]="$ledger_backed"
  if [ "$controllable_signer" = "true" ]; then
    NODE_SIGNER_SERVICE[$name]="${name}-signer"
    NODE_CONTROL_PORT[$name]=""
    SCENARIO_SIGNERS+=("$name")
  fi
  if [ "$gnokms_backed" = "true" ]; then
    NODE_GNOKMS_SERVICE[$name]="${name}-gnokms"
  fi
}

gen_sentry() {
  local name="${1:?sentry name required}"
  shift || true

  local rpc_port=""
  local pex="false"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --rpc-port)
        rpc_port="${2:?missing rpc port}"
        shift 2
        ;;
      --pex)
        pex="${2:?missing pex value}"
        shift 2
        ;;
      *)
        die "unknown gen_sentry option: $1"
        ;;
    esac
  done

  register_node "$name" sentry "$rpc_port" "$pex" ""
}

ensure_image_exists() {
  local image_id
  image_id="$(docker images -q "$IMAGE_NAME" 2>/dev/null)"
  if [ -z "$image_id" ]; then
    die "docker image ${IMAGE_NAME} not found; run \`make build-images\` first"
  fi
  image_id="$(docker images -q "$GNOKEY_IMAGE" 2>/dev/null)"
  if [ -z "$image_id" ]; then
    die "docker image ${GNOKEY_IMAGE} not found; run \`make build-images\` first"
  fi
  image_id="$(docker images -q "$GNOGENESIS_IMAGE" 2>/dev/null)"
  if [ -z "$image_id" ]; then
    die "docker image ${GNOGENESIS_IMAGE} not found; run \`make build-images\` first"
  fi
  if [ "${#SCENARIO_SIGNERS[@]}" -gt 0 ]; then
    image_id="$(docker images -q "$VALSIGNER_IMAGE" 2>/dev/null)"
    if [ -z "$image_id" ]; then
      die "docker image ${VALSIGNER_IMAGE} not found; run \`make build-images\` first"
    fi
  fi
}

compose() {
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
}

run_in_image() {
  docker run --rm --entrypoint /usr/bin/gnoland "$@"
}

init_node_dirs() {
  local node
  for node in "${SCENARIO_NODES[@]}"; do
    local node_dir="${SCENARIO_DIR}/nodes/${node}"
    NODE_DATA_DIR[$node]="$node_dir"
    mkdir -p "$node_dir"

    run_in_image -v "${node_dir}:/data" "$IMAGE_NAME" secrets init --data-dir /data/secrets >/dev/null
    run_in_image -v "${node_dir}:/data" "$IMAGE_NAME" config init --config-path /data/config/config.toml >/dev/null
  done
}

collect_node_ids() {
  local node
  for node in "${SCENARIO_NODES[@]}"; do
    NODE_ID[$node]="$(run_in_image -v "${NODE_DATA_DIR[$node]}:/data" "$IMAGE_NAME" secrets get node_id.id --data-dir /data/secrets --raw | tr -d '\r\n')"
    NODE_ADDRESS[$node]="$(run_in_image -v "${NODE_DATA_DIR[$node]}:/data" "$IMAGE_NAME" secrets get validator_key.address --data-dir /data/secrets --raw | tr -d '\r\n')"
    NODE_PUBKEY[$node]="$(run_in_image -v "${NODE_DATA_DIR[$node]}:/data" "$IMAGE_NAME" secrets get validator_key.pub_key --data-dir /data/secrets --raw | tr -d '\r\n')"
  done
}

# _gnogenesis runs a gnogenesis command with the scenario genesis and GNO_ROOT mounted.
# Callers must include --genesis-path /work/genesis.json after the subcommand name.
_gnogenesis() {
  docker run --rm \
    --entrypoint /usr/bin/gnogenesis \
    -v "${SCENARIO_DIR}:/work" \
    -v "${GNO_ROOT}:/gnoroot:ro" \
    "$GNOGENESIS_IMAGE" \
    "$@"
}

# _gnokey_deployer runs a gnokey command with the genesis deployer key home mounted.
_gnokey_deployer() {
  docker run -i --rm \
    --entrypoint /usr/bin/gnokey \
    -v "${SCENARIO_DIR}:/work" \
    "$GNOKEY_IMAGE" \
    "$@"
}

generate_genesis() {
  [ "${#SCENARIO_GENESIS_VALIDATORS[@]}" -gt 0 ] || die "at least one genesis validator is required"
  [ -d "${GNO_ROOT}/examples" ] || die "GNO_ROOT examples not found at ${GNO_ROOT}/examples; run 'make clone-gno' or set GNO_ROOT"

  local genesis_work="${SCENARIO_DIR}/genesis-work"
  local gnokey_home="${genesis_work}/gnokey-home"
  local deployer_name="GenesisDeployer"
  # Same mnemonic as gen-genesis.sh; address = g1edq4dugw0sgat4zxcw9xardvuydqf6cgleuc8p
  local deployer_mnemonic="anchor hurt name seed oak spread anchor filter lesson shaft wasp home improve text behind toe segment lamp turn marriage female royal twice wealth"

  mkdir -p "$genesis_work" "$gnokey_home"

  log "creating genesis deployer key"
  printf '%s\n\n' "$deployer_mnemonic" | \
    docker run -i --rm \
      --entrypoint /usr/bin/gnokey \
      -v "${gnokey_home}:/keys" \
      "$GNOKEY_IMAGE" \
      add --recover "$deployer_name" --home /keys --insecure-password-stdin >/dev/null

  log "generating empty genesis"
  docker run --rm \
    --entrypoint /usr/bin/gnogenesis \
    -v "${genesis_work}:/work" \
    "$GNOGENESIS_IMAGE" \
    generate \
      --chain-id "$CHAIN_ID" \
      --genesis-time "$(date +%s)" \
      --output-path /work/genesis.json >/dev/null

  # Copy genesis to the scenario work dir where _gnogenesis mounts it
  cp "${genesis_work}/genesis.json" "${SCENARIO_DIR}/genesis.json"

  log "adding packages from GNO_ROOT"
  printf '\n' | \
    docker run -i --rm \
      --entrypoint /usr/bin/gnogenesis \
      -v "${SCENARIO_DIR}:/work" \
      -v "${GNO_ROOT}:/gnoroot:ro" \
      -v "${gnokey_home}:/keys" \
      "$GNOGENESIS_IMAGE" \
      txs add packages /gnoroot/examples \
        --genesis-path /work/genesis.json \
        --gno-home /keys \
        --key-name "$deployer_name" \
        --insecure-password-stdin >/dev/null

  log "generating valset-init MsgRun"
  local valset_file="${genesis_work}/valset-init.gno"
  local valset_entries=""
  local node
  for node in "${SCENARIO_GENESIS_VALIDATORS[@]}"; do
    valset_entries+="$(printf '\t\t\t\t{Address: address("%s"), PubKey: "%s", VotingPower: %s},\n' \
      "${NODE_ADDRESS[$node]}" "${NODE_PUBKEY[$node]}" "${NODE_POWER[$node]:-1}")"
  done
  awk -v entries="$valset_entries" \
    '/\/\/ GEN:VALSET/ { printf "%s", entries; next } { print }' \
    "${SCENARIO_LIB_DIR}/valset-init.gno.tpl" > "$valset_file"

  local setup_tx="${genesis_work}/valset-init-tx.json"
  local setup_tx_jsonl="${genesis_work}/valset-init-tx.jsonl"

  printf '\n' | _gnokey_deployer \
    maketx run \
      --gas-wanted 100000000 \
      --gas-fee 1ugnot \
      --chainid "$CHAIN_ID" \
      --broadcast=false \
      --home /work/genesis-work/gnokey-home \
      --insecure-password-stdin \
      "$deployer_name" \
      /work/genesis-work/valset-init.gno > "$setup_tx"

  printf '\n' | _gnokey_deployer \
    sign \
      --tx-path /work/genesis-work/valset-init-tx.json \
      --chainid "$CHAIN_ID" \
      --account-number 0 \
      --account-sequence 0 \
      --home /work/genesis-work/gnokey-home \
      --insecure-password-stdin \
      "$deployer_name" >/dev/null

  jq -c '{tx: .}' < "$setup_tx" > "$setup_tx_jsonl"

  _gnogenesis txs add sheets --genesis-path /work/genesis.json /work/genesis-work/valset-init-tx.jsonl >/dev/null

  log "adding ${#SCENARIO_GENESIS_VALIDATORS[@]} validators to consensus layer"
  for node in "${SCENARIO_GENESIS_VALIDATORS[@]}"; do
    _gnogenesis validator add \
      --genesis-path /work/genesis.json \
      --name "$node" \
      --address "${NODE_ADDRESS[$node]}" \
      --pub-key "${NODE_PUBKEY[$node]}" \
      --power "${NODE_POWER[$node]:-1}" >/dev/null
  done

  log "adding test1 balance"
  _gnogenesis balances add --genesis-path /work/genesis.json --single "${TX_ADDRESS}=${TX_BALANCE}" >/dev/null

  local genesis_file="${SCENARIO_DIR}/genesis.json"
  for node in "${SCENARIO_NODES[@]}"; do
    cp "$genesis_file" "${NODE_DATA_DIR[$node]}/genesis.json"
  done
}

format_peer_entry() {
  local node="${1:?node required}"
  printf '%s@%s:26656' "${NODE_ID[$node]}" "${NODE_SERVICE[$node]}"
}

persistent_peer_targets() {
  local node="${1:?node required}"
  local role="${NODE_ROLE[$node]}"
  local target
  local -a peers=()

  case "$role" in
    validator)
      if [ -n "${NODE_SENTRY[$node]}" ]; then
        peers+=("${NODE_SENTRY[$node]}")
      else
        for target in "${SCENARIO_VALIDATORS[@]}"; do
          if [ "$target" != "$node" ] && [ -z "${NODE_SENTRY[$target]}" ]; then
            peers+=("$target")
          fi
        done
        for target in "${SCENARIO_SENTRIES[@]}"; do
          peers+=("$target")
        done
      fi
      ;;
    sentry)
      for target in "${SCENARIO_VALIDATORS[@]}"; do
        if [ "$target" = "$node" ]; then
          continue
        fi
        # Only peer with validators that are not hidden behind this sentry.
        # Hidden validators dial the sentry themselves and are listed in
        # private_peer_ids; they must not appear in persistent_peers/seeds.
        if [ -z "${NODE_SENTRY[$target]}" ]; then
          peers+=("$target")
        fi
      done
      for target in "${SCENARIO_SENTRIES[@]}"; do
        if [ "$target" != "$node" ]; then
          peers+=("$target")
        fi
      done
      ;;
    *)
      die "unsupported role ${role}"
      ;;
  esac

  printf '%s\n' "${peers[@]}" | awk '!seen[$0]++ && NF'
}

persistent_peers_for_node() {
  local node="${1:?node required}"
  local -a rendered=()
  local target

  while IFS= read -r target; do
    [ -n "$target" ] || continue
    rendered+=("$(format_peer_entry "$target")")
  done < <(persistent_peer_targets "$node")

  join_by ',' "${rendered[@]}"
}

set_config_value() {
  local node="${1:?node required}"
  local key="${2:?config key required}"
  local value="${3:?config value required}"

  run_in_image -v "${NODE_DATA_DIR[$node]}:/data" "$IMAGE_NAME" \
    config set \
      --config-path /data/config/config.toml \
      "$key" "$value" >/dev/null
}

private_peer_ids_for_sentry() {
  local sentry="${1:?sentry required}"
  local -a ids=()
  local target
  for target in "${SCENARIO_VALIDATORS[@]}"; do
    if [ "${NODE_SENTRY[$target]}" = "$sentry" ]; then
      ids+=("${NODE_ID[$target]}")
    fi
  done
  join_by ',' "${ids[@]}"
}

configure_nodes() {
  local node
  for node in "${SCENARIO_NODES[@]}"; do
    local peers
    peers="$(persistent_peers_for_node "$node")"

    set_config_value "$node" moniker "${NODE_MONIKER[$node]}"
    set_config_value "$node" rpc.laddr "tcp://0.0.0.0:26657"
    set_config_value "$node" p2p.laddr "tcp://0.0.0.0:26656"
    set_config_value "$node" p2p.pex "${NODE_PEX[$node]}"
    if [ -n "$peers" ]; then
      set_config_value "$node" p2p.persistent_peers "$peers"
      set_config_value "$node" p2p.seeds "$peers"
    fi
    set_config_value "$node" consensus.timeout_commit "$TIMEOUT_COMMIT"
    if [ "${NODE_CONTROLLABLE_SIGNER[$node]:-false}" = "true" ]; then
      set_config_value "$node" consensus.priv_validator.remote_signer.server_address "tcp://${NODE_SIGNER_SERVICE[$node]}:26659"
      set_config_value "$node" consensus.priv_validator.remote_signer.request_timeout "$REMOTE_SIGNER_REQUEST_TIMEOUT"
    fi

    if [ "${NODE_ROLE[$node]}" = "sentry" ]; then
      local private_ids
      private_ids="$(private_peer_ids_for_sentry "$node")"
      if [ -n "$private_ids" ]; then
        set_config_value "$node" p2p.private_peer_ids "$private_ids"
      fi
    fi
  done
}

write_compose_file() {
  {
    printf 'name: %s\n\n' "$PROJECT_NAME"
    printf 'services:\n'
    local node
    local signer
    local gnokms_backed
    local ledger_backed
    for signer in "${SCENARIO_SIGNERS[@]}"; do
      gnokms_backed="${NODE_GNOKMS_BACKED[$signer]:-false}"
      ledger_backed="${NODE_LEDGER_BACKED[$signer]:-false}"
      if [ "$gnokms_backed" = "true" ]; then
        # Keybase mounted rw: gnokey opens leveldb with a write lock.
        printf '  %s:\n' "${NODE_GNOKMS_SERVICE[$signer]}"
        printf '    image: "%s"\n' "$GNOGENESIS_IMAGE"
        printf '    entrypoint:\n'
        printf '      - /bin/sh\n'
        printf '    command:\n'
        printf '      - -c\n'
        printf '      - "echo %s | /usr/bin/gnokms gnokey %s --home /keys --listener tcp://0.0.0.0:26659 --insecure-password-stdin --log-level %s"\n' \
          "$GNOKMS_KEYBASE_PASSWORD" "$GNOKMS_KEYBASE_KEY_NAME" "$LOG_LEVEL"
        printf '    volumes:\n'
        printf '      - "%s/gnokms-keys:/keys"\n' "${NODE_DATA_DIR[$signer]}"
        printf '    networks:\n'
        printf '      - chain\n'
        printf '    stop_grace_period: 5s\n'
      fi

      printf '  %s:\n' "${NODE_SIGNER_SERVICE[$signer]}"
      printf '    image: "%s"\n' "$VALSIGNER_IMAGE"
      printf '    command:\n'
      if [ "$ledger_backed" = "true" ]; then
        printf '      - --gnokms-addr\n'
        printf '      - tcp://host.docker.internal:%s\n' "${NODE_LEDGER_HOST_PORT[$signer]}"
      elif [ "$gnokms_backed" = "true" ]; then
        printf '      - --gnokms-addr\n'
        printf '      - tcp://%s:26659\n' "${NODE_GNOKMS_SERVICE[$signer]}"
      else
        printf '      - --key-file\n'
        printf '      - /data/secrets/priv_validator_key.json\n'
      fi
      printf '      - --listen-addr\n'
      printf '      - :8080\n'
      printf '      - --remote-signer-addr\n'
      printf '      - tcp://0.0.0.0:26659\n'
      printf '    volumes:\n'
      printf '      - "%s:/data:ro"\n' "${NODE_DATA_DIR[$signer]}"
      printf '    ports:\n'
      if [ -n "${NODE_CONTROL_PORT[$signer]:-}" ]; then
        printf '      - "%s:8080"\n' "${NODE_CONTROL_PORT[$signer]}"
      else
        printf '      - "::8080"\n'
      fi
      printf '    networks:\n'
      printf '      - chain\n'
      printf '    stop_grace_period: 5s\n'
      if [ "$ledger_backed" = "true" ]; then
        # Linux Docker needs this mapping; macOS Docker Desktop ignores it.
        printf '    extra_hosts:\n'
        printf '      - "host.docker.internal:host-gateway"\n'
      elif [ "$gnokms_backed" = "true" ]; then
        printf '    depends_on:\n'
        printf '      - %s\n' "${NODE_GNOKMS_SERVICE[$signer]}"
      fi
    done

    for node in "${SCENARIO_NODES[@]}"; do
      printf '  %s:\n' "${NODE_SERVICE[$node]}"
      printf '    image: "%s"\n' "$IMAGE_NAME"
      printf '    entrypoint:\n'
      printf '      - /usr/bin/gnoland\n'
      printf '    command:\n'
      printf '      - start\n'
      printf '      - -skip-genesis-sig-verification\n'
      printf '      - -data-dir\n'
      printf '      - /data\n'
      printf '      - -genesis\n'
      printf '      - /data/genesis.json\n'
      printf '      - -chainid\n'
      printf '      - %s\n' "$CHAIN_ID"
      printf '      - -gnoroot-dir\n'
      printf '      - /gnoroot\n'
      printf '      - -log-level\n'
      printf '      - %s\n' "$LOG_LEVEL"
      printf '    volumes:\n'
      printf '      - "%s:/data"\n' "${NODE_DATA_DIR[$node]}"
      printf '    ports:\n'
      if [ -n "${NODE_RPC_PORT[$node]:-}" ]; then
        printf '      - "%s:26657"\n' "${NODE_RPC_PORT[$node]}"
      else
        printf '      - "::26657"\n'
      fi
      printf '    networks:\n'
      printf '      - chain\n'
      printf '    stop_grace_period: 5s\n'
    done
    printf '\nnetworks:\n'
    printf '  chain: {}\n'
  } > "$COMPOSE_FILE"
}

discover_ledger_pubkeys() {
  local ledger_count=0
  local node
  for node in "${SCENARIO_VALIDATORS[@]+"${SCENARIO_VALIDATORS[@]}"}"; do
    [ "${NODE_LEDGER_BACKED[$node]:-false}" = "true" ] || continue
    ledger_count=$((ledger_count + 1))
    [ "$ledger_count" -le 1 ] || die "only one --ledger-backed-signer validator is supported (one physical Ledger)"
    [ -x "$GNOKMS_LEDGER_BIN" ] || die "gnokms-ledger binary not found at ${GNOKMS_LEDGER_BIN}; build it with 'make build-gnokms-ledger'"

    printf '\n'
    printf '╭───────────────────────────────────────────────────────────╮\n'
    printf '│ Ledger setup for validator %-32s │\n' "$node"
    printf '├───────────────────────────────────────────────────────────┤\n'
    printf '│  1. Plug the Ledger device into this host.                │\n'
    printf '│  2. Unlock it.                                            │\n'
    printf '│  3. Open the Tendermint validator app on the device.      │\n'
    printf '│  4. Quit Ledger Live (it grabs the device exclusively).   │\n'
    printf '╰───────────────────────────────────────────────────────────╯\n'
    read -r -p "Press Enter when ready... " _

    local port="$GNOKMS_LEDGER_HOST_PORT"
    local logf="${SCENARIO_DIR}/logs/${node}-host-gnokms.log"
    mkdir -p "$(dirname "$logf")"

    log "starting host gnokms-ledger for ${node} (port=${port}, log=${logf})"
    : > "$logf"
    "$GNOKMS_LEDGER_BIN" ledger \
      --listener "tcp://0.0.0.0:${port}" \
      --log-level "$LOG_LEVEL" \
      --log-format console \
      >"$logf" 2>&1 &
    local pid="$!"
    NODE_LEDGER_GNOKMS_PID[$node]="$pid"
    NODE_LEDGER_HOST_PORT[$node]="$port"
    NODE_LEDGER_LOG_FILE[$node]="$logf"
    disown "$pid" 2>/dev/null || true

    # Mirror gnokms output to the terminal in real time so the user sees any
    # error printed at exit. The tail follows the same file we're polling.
    tail -n +1 -F "$logf" >&2 2>/dev/null &
    local tail_pid="$!"
    disown "$tail_pid" 2>/dev/null || true

    local i pubkey="" address="" exit_reason=""
    for i in $(seq 1 "$GNOKMS_LEDGER_TIMEOUT"); do
      if ! kill -0 "$pid" 2>/dev/null; then
        exit_reason="exited"
        break
      fi
      # awk returns 0 on no match (unlike grep), so this stays compatible
      # with `set -o pipefail` even when the log doesn't yet contain pubkey.
      pubkey="$(awk '/^[[:space:]]+pub_key:[[:space:]]+gpub/ {print $2; exit}' "$logf" 2>/dev/null)"
      address="$(awk '/^[[:space:]]+address:[[:space:]]+g1/ {print $2; exit}' "$logf" 2>/dev/null)"
      if [ -n "$pubkey" ] && [ -n "$address" ]; then
        break
      fi
      sleep 1
    done

    # Stop the mirror tail; flush any final output it had pending.
    kill "$tail_pid" 2>/dev/null || true
    wait "$tail_pid" 2>/dev/null || true

    if [ "$exit_reason" = "exited" ]; then
      dump_ledger_log "$node" "$logf"
      die "host gnokms-ledger exited before publishing a pubkey; see ${logf}"
    fi
    if [ -z "$pubkey" ] || [ -z "$address" ]; then
      dump_ledger_log "$node" "$logf"
      die "did not see ledger pubkey within ${GNOKMS_LEDGER_TIMEOUT}s; see ${logf}"
    fi

    NODE_PUBKEY[$node]="$pubkey"
    NODE_ADDRESS[$node]="$address"
    log "ledger pubkey for ${node}: ${address}"
  done
}

stop_host_gnokms_ledger() {
  local node
  for node in "${SCENARIO_VALIDATORS[@]+"${SCENARIO_VALIDATORS[@]}"}"; do
    local pid="${NODE_LEDGER_GNOKMS_PID[$node]:-}"
    [ -n "$pid" ] || continue
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    NODE_LEDGER_GNOKMS_PID[$node]=""
  done
}

populate_gnokms_keybases() {
  local signer
  for signer in "${SCENARIO_SIGNERS[@]+"${SCENARIO_SIGNERS[@]}"}"; do
    [ "${NODE_GNOKMS_BACKED[$signer]:-false}" = "true" ] || continue
    docker run --rm \
      --entrypoint /usr/bin/valkeyimport \
      -v "${NODE_DATA_DIR[$signer]}:/data" \
      "$VALSIGNER_IMAGE" \
      --priv-validator-key /data/secrets/priv_validator_key.json \
      --keybase-dir /data/gnokms-keys \
      --key-name "$GNOKMS_KEYBASE_KEY_NAME" \
      --password "$GNOKMS_KEYBASE_PASSWORD" >/dev/null
  done
}

create_tx_key() {
  mkdir -p "$KEY_HOME"
  if find "$KEY_HOME" -mindepth 1 -print -quit | grep -q .; then
    return
  fi

  printf '%s\n%s\n%s\n' "$TX_MNEMONIC" "$TX_PASSWORD" "$TX_PASSWORD" | \
    docker run -i --rm --entrypoint /usr/bin/gnokey -v "${KEY_HOME}:/keys" "$GNOKEY_IMAGE" \
      add "$TX_KEY_NAME" --home /keys --recover --quiet --insecure-password-stdin >/dev/null
}

wipe_scenario_dir() {
  [ -d "$SCENARIO_DIR" ] || return 0
  if rm -rf "$SCENARIO_DIR" 2>/dev/null; then
    return 0
  fi
  # Files inside SCENARIO_DIR may be owned by root because previous runs
  # created them inside docker containers. Wipe them as root via a one-shot
  # container so we can start fresh without sudo.
  log "wiping ${SCENARIO_DIR} via container (root-owned files left by previous run)"
  docker run --rm --entrypoint sh \
    -v "${WORK_ROOT}:/work" \
    "$IMAGE_NAME" \
    -c "rm -rf /work/${PROJECT_NAME}" \
    || die "failed to wipe ${SCENARIO_DIR}; remove it manually"
}

prepare_network() {
  require_tools
  ensure_image_exists

  [ "${#SCENARIO_NODES[@]}" -gt 0 ] || die "no nodes declared"

  wipe_scenario_dir
  mkdir -p "$SCENARIO_DIR"

  init_node_dirs
  collect_node_ids
  discover_ledger_pubkeys
  generate_genesis
  configure_nodes
  populate_gnokms_keybases
  write_compose_file
  create_tx_key

  log "prepared network in ${SCENARIO_DIR}"
}

node_rpc_url() {
  local node="${1:?node required}"
  printf 'http://127.0.0.1:%s' "${NODE_RPC_PORT[$node]}"
}

node_control_url() {
  local node="${1:?node required}"
  [ "${NODE_CONTROLLABLE_SIGNER[$node]:-false}" = "true" ] || die "validator ${node} does not have a controllable signer"
  printf 'http://127.0.0.1:%s' "${NODE_CONTROL_PORT[$node]}"
}

write_inventory() {
  local inventory="${SCENARIO_DIR}/inventory.json"
  local validators_json="[]"
  local node

  for node in "${SCENARIO_VALIDATORS[@]}"; do
    local control_url="null"
    if [ "${NODE_CONTROLLABLE_SIGNER[$node]:-false}" = "true" ]; then
      control_url="\"$(node_control_url "$node")\""
    fi

    validators_json="$(
      jq -cn \
        --argjson current "$validators_json" \
        --arg name "$node" \
        --arg rpc "$(node_rpc_url "$node")" \
        --arg service "${NODE_SERVICE[$node]}" \
        --arg signer_service "${NODE_SIGNER_SERVICE[$node]:-}" \
        --arg address "${NODE_ADDRESS[$node]}" \
        --arg pubkey "${NODE_PUBKEY[$node]}" \
        --argjson controllable "$( [ "${NODE_CONTROLLABLE_SIGNER[$node]:-false}" = "true" ] && printf 'true' || printf 'false' )" \
        --argjson control_url "$control_url" \
        '$current + [{
          name: $name,
          rpc_url: $rpc,
          control_url: $control_url,
          service: $service,
          signer_service: $signer_service,
          controllable_signer: $controllable,
          address: $address,
          pub_key: $pubkey
        }]' \
    )"
  done

  jq -n \
    --arg scenario "$SCENARIO_NAME" \
    --arg work_dir "$SCENARIO_DIR" \
    --arg compose_file "$COMPOSE_FILE" \
    --argjson validators "$validators_json" \
    '{
      scenario: $scenario,
      work_dir: $work_dir,
      compose_file: $compose_file,
      validators: $validators
    }' > "$inventory"

  log "wrote inventory: ${inventory}"
}

wait_for_rpc() {
  local node="${1:?node required}"
  local timeout="${2:-120}"
  local i
  for i in $(seq 1 "$timeout"); do
    if curl -fsS "$(node_rpc_url "$node")/status" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  die "rpc for ${node} did not come up within ${timeout}s"
}

wait_for_control() {
  local node="${1:?node required}"
  local timeout="${2:-120}"
  local i
  for i in $(seq 1 "$timeout"); do
    if curl -fsS "$(node_control_url "$node")/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  die "control api for ${node} did not come up within ${timeout}s"
}

_capture_node_logs() {
  local node="${1:?node required}"
  # Kill any existing log-follower for this service so there is always exactly
  # one writer per log file (prevents stale followers after container restarts).
  if [ -n "${NODE_LOG_PID[$node]:-}" ]; then
    kill "${NODE_LOG_PID[$node]}" 2>/dev/null || true
  fi
  mkdir -p "${SCENARIO_DIR}/logs"
  # Inline docker compose instead of the compose() wrapper: bash functions are
  # unreliable inside background jobs in non-interactive shells.
  # Pipe through awk to force per-line flushing: docker compose uses full
  # buffering when stdout is not a TTY (i.e. any non-interactive invocation),
  # so without fflush() nothing reaches the log file until the buffer fills.
  # Guard disown with || true — without job control (non-interactive bash)
  # disown can return non-zero which would trigger set -e.
  # Redirect awk stderr to /dev/null so it does not inherit the parent shell's
  # stderr fd — when invoked via runBashScript the parent stderr is a Go pipe,
  # and leaving it open in the background process would cause CombinedOutput()
  # to block indefinitely waiting for the write end to close.
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" logs -f "$node" 2>&1 | \
    awk '{ print; fflush() }' >> "${SCENARIO_DIR}/logs/${node}.log" 2>/dev/null &
  local pid="$!"
  NODE_LOG_PID[$node]="$pid"
  disown "$pid" 2>/dev/null || true
}

_resolve_rpc_port() {
  local node="${1:?node required}"
  local host_port
  host_port="$(compose port "${NODE_SERVICE[$node]}" 26657 2>/dev/null | grep -oE '[0-9]+$')"
  [ -n "$host_port" ] || die "could not resolve host RPC port for ${node}"
  NODE_RPC_PORT[$node]="$host_port"
}

_resolve_control_port() {
  local node="${1:?node required}"
  [ "${NODE_CONTROLLABLE_SIGNER[$node]:-false}" = "true" ] || return 0
  local host_port
  host_port="$(compose port "${NODE_SIGNER_SERVICE[$node]}" 8080 2>/dev/null | grep -oE '[0-9]+$')"
  [ -n "$host_port" ] || die "could not resolve host control port for ${node}"
  NODE_CONTROL_PORT[$node]="$host_port"
}

start_node() {
  local node="${1:?node required}"
  compose up -d "$node" >/dev/null
  _resolve_rpc_port "$node"
  wait_for_rpc "$node" 120
  _capture_node_logs "$node"
  log "started ${node}"
}

start_validator() {
  start_node "$1"
}

start_sentry() {
  start_node "$1"
}

start_all_nodes() {
  [ "${#SCENARIO_NODES[@]}" -gt 0 ] || die "no nodes to start"

  local node

  if [ "${#SCENARIO_SIGNERS[@]}" -gt 0 ]; then
    local signer_service
    local gnokms_service
    for node in "${SCENARIO_SIGNERS[@]}"; do
      if [ "${NODE_GNOKMS_BACKED[$node]:-false}" = "true" ]; then
        gnokms_service="${NODE_GNOKMS_SERVICE[$node]}"
        compose up -d "$gnokms_service" >/dev/null
        _capture_node_logs "$gnokms_service"
        log "started ${gnokms_service}"
      fi
      if [ "${NODE_LEDGER_BACKED[$node]:-false}" = "true" ]; then
        local ledger_pid="${NODE_LEDGER_GNOKMS_PID[$node]:-}"
        [ -n "$ledger_pid" ] && kill -0 "$ledger_pid" 2>/dev/null \
          || die "host gnokms-ledger for ${node} is not running; see ${NODE_LEDGER_LOG_FILE[$node]:-<no log>}"
      fi
      signer_service="${NODE_SIGNER_SERVICE[$node]}"
      compose up -d "$signer_service" >/dev/null
      _resolve_control_port "$node"
      wait_for_control "$node" 120
      _capture_node_logs "$signer_service"
      log "started ${signer_service}"
    done
  fi

  # Start sentries first and wait for them before launching validators so
  # that the P2P gateway is ready when validators try to dial out.
  if [ "${#SCENARIO_SENTRIES[@]}" -gt 0 ]; then
    compose up -d "${SCENARIO_SENTRIES[@]}" >/dev/null
    for node in "${SCENARIO_SENTRIES[@]}"; do
      _resolve_rpc_port "$node"
      wait_for_rpc "$node" 120
      _capture_node_logs "$node"
    done
  fi

  if [ "${#SCENARIO_VALIDATORS[@]}" -gt 0 ]; then
    compose up -d "${SCENARIO_VALIDATORS[@]}" >/dev/null
    for node in "${SCENARIO_VALIDATORS[@]}"; do
      _resolve_rpc_port "$node"
      wait_for_rpc "$node" 120
      _capture_node_logs "$node"
    done
  fi

  write_compose_file
  write_inventory
  log "started ${#SCENARIO_NODES[@]} node(s)"
}

stop_node() {
  local node="${1:?node required}"
  compose stop "$node" >/dev/null
  log "stopped ${node}"
}

stop_validator() {
  stop_node "$1"
}

stop_sentry() {
  stop_node "$1"
}

reset_node() {
  local node="${1:?node required}"
  stop_node "$node" || true
  # All files under the node data dir are owned by root (created inside the
  # container), so perform the reset from inside a container to avoid host
  # permission errors.
  docker run --rm --entrypoint sh \
    -v "${NODE_DATA_DIR[$node]}:/data" \
    -v "${SCENARIO_DIR}/genesis.json:/genesis.json:ro" \
    "$IMAGE_NAME" \
    -c 'rm -rf /data/db /data/wal && printf '"'"'{"height":"0","round":"0","step":0}\n'"'"' > /data/secrets/priv_validator_state.json && cp /genesis.json /data/genesis.json'
  log "reset ${node}"
}

reset_validator() {
  reset_node "$1"
}

safe_reset_node() {
  local node="${1:?node required}"
  stop_node "$node" || true
  # Remove only db and wal; preserve priv_validator_state.json so the node
  # cannot sign a block at a height/round/step it already committed (no double
  # signing). genesis.json is left untouched as well.
  docker run --rm --entrypoint sh \
    -v "${NODE_DATA_DIR[$node]}:/data" "$IMAGE_NAME" \
    -c 'rm -rf /data/db /data/wal'
  log "safe-reset ${node}"
}

safe_reset_validator() {
  safe_reset_node "$1"
}

wait_for_seconds() {
  local seconds="${1:?seconds required}"
  log "waiting ${seconds}s"
  sleep "$seconds"
}

node_height() {
  local node="${1:?node required}"
  curl -fsS "$(node_rpc_url "$node")/status" | jq -r '.result.sync_info.latest_block_height // "0"'
}

wait_for_height() {
  local node="${1:?node required}"
  local target="${2:?target height required}"
  local timeout="${3:-120}"
  local i
  for i in $(seq 1 "$timeout"); do
    local height
    height="$(node_height "$node" 2>/dev/null || printf '0')"
    if [ "$height" -ge "$target" ] 2>/dev/null; then
      log "${node} reached height ${height}"
      return 0
    fi
    sleep 1
  done
  die "${node} did not reach height ${target} within ${timeout}s"
}

wait_for_blocks() {
  local node="${1:?node required}"
  local delta="${2:?delta required}"
  local timeout="${3:-120}"
  local current
  current="$(node_height "$node")"
  wait_for_height "$node" "$((current + delta))" "$timeout"
}

signer_state() {
  local node="${1:?node required}"
  curl -fsS "$(node_control_url "$node")/state"
}

_signer_rule_request() {
  local node="${1:?node required}"
  local phase="${2:?phase required}"
  local action="${3:?action required}"
  local height="${4:-}"
  local round="${5:-}"
  local delay="${6:-}"

  local -a jq_args=(
    -n
    --arg action "$action"
    --arg height "$height"
    --arg round "$round"
    --arg delay "$delay"
  )

  jq "${jq_args[@]}" '
    {
      action: $action
    }
    + (if $height != "" then {height: ($height | tonumber)} else {} end)
    + (if $round != "" then {round: ($round | tonumber)} else {} end)
    + (if $delay != "" then {delay: $delay} else {} end)
  '
}

signer_drop() {
  local node="${1:?validator required}"
  local phase="${2:?phase required}"
  local height="${3:-}"
  local round="${4:-}"
  local payload
  payload="$(_signer_rule_request "$node" "$phase" drop "$height" "$round" "")"
  curl -fsS -X PUT -H 'Content-Type: application/json' --data "$payload" \
    "$(node_control_url "$node")/rules/${phase}" >/dev/null
  log "configured signer drop on ${node} phase=${phase} height=${height:-*} round=${round:-*}"
}

signer_delay() {
  local node="${1:?validator required}"
  local phase="${2:?phase required}"
  local delay="${3:?delay required}"
  local height="${4:-}"
  local round="${5:-}"
  local payload
  payload="$(_signer_rule_request "$node" "$phase" delay "$height" "$round" "$delay")"
  curl -fsS -X PUT -H 'Content-Type: application/json' --data "$payload" \
    "$(node_control_url "$node")/rules/${phase}" >/dev/null
  log "configured signer delay on ${node} phase=${phase} delay=${delay} height=${height:-*} round=${round:-*}"
}

print_signer_metrics() {
  local node="${1:?validator required}"
  [ "${NODE_CONTROLLABLE_SIGNER[$node]:-false}" = "true" ] || die "validator ${node} does not have a controllable signer"

  local backend_label="local"
  if [ "${NODE_LEDGER_BACKED[$node]:-false}" = "true" ]; then
    backend_label="gnokms+ledger"
  elif [ "${NODE_GNOKMS_BACKED[$node]:-false}" = "true" ]; then
    backend_label="gnokms+gnokey"
  fi

  local state
  state="$(signer_state "$node")"

  printf '\n=== signer metrics: %s (backend=%s) ===\n' "$node" "$backend_label"
  printf '%-10s %8s %12s %12s %12s\n' phase count avg_ms min_ms max_ms
  printf '%s' "$state" | jq -r '
    ["proposal","prevote","precommit"][] as $phase |
    .stats[$phase] as $s |
    if ($s.sign_count // 0) > 0 then
      [$phase,
       ($s.sign_count | tostring),
       (($s.total_ns / $s.sign_count / 1000000) | tostring),
       (($s.min_ns / 1000000) | tostring),
       (($s.max_ns / 1000000) | tostring)]
    else
      [$phase, "0", "-", "-", "-"]
    end | @tsv
  ' | awk -F '\t' '{
    if ($2 == "0") printf "%-10s %8s %12s %12s %12s\n", $1, $2, $3, $4, $5
    else printf "%-10s %8s %12.3f %12.3f %12.3f\n", $1, $2, $3, $4, $5
  }'
}

print_all_signer_metrics() {
  local node
  for node in "${SCENARIO_SIGNERS[@]+"${SCENARIO_SIGNERS[@]}"}"; do
    print_signer_metrics "$node"
  done
}

signer_clear() {
  local node="${1:?validator required}"
  local phase="${2:-}"

  if [ -n "$phase" ]; then
    curl -fsS -X DELETE "$(node_control_url "$node")/rules/${phase}" >/dev/null
    log "cleared signer rule on ${node} phase=${phase}"
    return 0
  fi

  curl -fsS -X POST "$(node_control_url "$node")/reset" >/dev/null
  log "cleared signer rules on ${node}"
}

# chain_advances succeeds if the chain produces at least <delta> new blocks on
# <node> within <timeout> seconds. Use this when the caller needs to inspect the
# result before deciding how to fail.
chain_advances() {
  local node="${1:?node required}"
  local timeout="${2:-30}"
  local delta="${3:-2}"
  local before
  before="$(node_height "$node")"
  local target="$((before + delta))"
  local i h
  for i in $(seq 1 "$timeout"); do
    h="$(node_height "$node" 2>/dev/null || printf '0')"
    if [ "$h" -ge "$target" ] 2>/dev/null; then
      log "chain advancing: ${node} reached h=${h} (was ${before})"
      return 0
    fi
    sleep 1
  done
  return 1
}

# assert_chain_halted fails if the chain keeps producing blocks on <node>
# within <timeout> seconds. Use this to verify that a deliberate halt occurred.
assert_chain_halted() {
  local node="${1:?node required}"
  local timeout="${2:-30}"
  local delta="${3:-2}"

  if chain_advances "$node" "$timeout" "$delta"; then
    die "expected chain to halt on ${node}, but it kept advancing"
  fi
  log "chain halted as expected on ${node}"
}

# assert_chain_advances fails if the chain does not produce at least <delta> new
# blocks on <node> within <timeout> seconds. Use this to detect a chain halt.
assert_chain_advances() {
  local node="${1:?node required}"
  local timeout="${2:-30}"
  local delta="${3:-2}"

  if chain_advances "$node" "$timeout" "$delta"; then
    return 0
  fi

  local before
  before="$(node_height "$node" 2>/dev/null || printf '0')"
  local target="$((before + delta))"
  die "chain halted: ${node} height stuck at h=${before} after ${timeout}s (expected >=${target})"
}

docker_network_name() {
  printf '%s' "$NETWORK_NAME"
}

gnokey_tx_with_password() {
  # Consume leading -v <bind> docker volume flags before the gnokey subcommand.
  local -a extra_docker_args=()
  while [[ $# -gt 0 && "$1" == "-v" ]]; do
    extra_docker_args+=("-v" "$2")
    shift 2
  done
  printf '%s\n' "$TX_PASSWORD" | \
    docker run -i --rm \
      --entrypoint /usr/bin/gnokey \
      --network "$(docker_network_name)" \
      -v "${KEY_HOME}:/keys" \
      "${extra_docker_args[@]}" \
      "$GNOKEY_IMAGE" \
      "$@"
}

add_pkg() {
  local target_node="${1:?target node required}"
  local pkgdir="${2:?package dir required}"
  local pkgpath="${3:?package path required}"
  local gas_wanted="${4:-$TX_GAS_WANTED_ADD_PKG}"
  local simulate_mode="${5:-}"

  local abs_pkgdir
  abs_pkgdir="$(cd "$pkgdir" && pwd)"

  local -a cmd=(
    maketx addpkg
    --pkgdir /pkg
    --pkgpath "$pkgpath"
    --gas-fee "$TX_GAS_FEE"
    --gas-wanted "$gas_wanted"
    --broadcast=true
    --chainid "$CHAIN_ID"
    --remote "${NODE_SERVICE[$target_node]}:26657"
    --home /keys
    --insecure-password-stdin
  )

  if [ -n "$simulate_mode" ]; then
    cmd+=(--simulate "$simulate_mode")
  fi

  cmd+=("$TX_KEY_NAME")

  gnokey_tx_with_password \
    -v "${abs_pkgdir}:/pkg:ro" \
    "${cmd[@]}"
}

estimate_add_pkg_gas() {
  local target_node="${1:?target node required}"
  local pkgdir="${2:?package dir required}"
  local pkgpath="${3:?package path required}"
  local probe_gas_wanted="${4:-$TX_GAS_WANTED_ADD_PKG}"

  local output
  output="$(add_pkg "$target_node" "$pkgdir" "$pkgpath" "$probe_gas_wanted" only)"
  printf '%s\n' "$output" >&2

  local gas_used
  gas_used="$(printf '%s\n' "$output" | awk '/GAS USED:/ {print $3; exit}')"
  [ -n "$gas_used" ] || die "failed to parse simulated gas usage for addpkg on ${target_node}"

  printf '%s\n' "$gas_used"
}

call_realm() {
  local target_node="${1:?target node required}"
  local pkgpath="${2:?package path required}"
  local func_name="${3:?function name required}"
  shift 3 || true

  local -a cmd=(
    maketx call
    --pkgpath "$pkgpath"
    --func "$func_name"
    --gas-fee "$TX_GAS_FEE"
    --gas-wanted "$TX_GAS_WANTED_CALL"
    --broadcast=true
    --chainid "$CHAIN_ID"
    --remote "${NODE_SERVICE[$target_node]}:26657"
    --home /keys
    --insecure-password-stdin
  )

  local arg
  for arg in "$@"; do
    cmd+=(--args "$arg")
  done
  cmd+=("$TX_KEY_NAME")

  gnokey_tx_with_password "${cmd[@]}"
}

run_script() {
  local target_node="${1:?target node required}"
  local script_path="${2:?script path required}"
  local gas_wanted="${3:-$TX_GAS_WANTED_RUN}"
  local simulate_mode="${4:-}"

  local abs_script
  local script_dir
  local script_name
  abs_script="$(cd "$(dirname "$script_path")" && pwd)/$(basename "$script_path")"
  script_dir="$(dirname "$abs_script")"
  script_name="$(basename "$abs_script")"

  local -a cmd=(
    maketx run
      --gas-fee "$TX_GAS_FEE"
      --gas-wanted "$gas_wanted"
      --broadcast=true
      --chainid "$CHAIN_ID"
      --remote "${NODE_SERVICE[$target_node]}:26657"
      --home /keys
      --insecure-password-stdin
  )

  if [ -n "$simulate_mode" ]; then
    cmd+=(--simulate "$simulate_mode")
  fi

  cmd+=("$TX_KEY_NAME" "/script/${script_name}")

  gnokey_tx_with_password \
    -v "${script_dir}:/script:ro" \
    "${cmd[@]}"
}

estimate_run_gas() {
  local target_node="${1:?target node required}"
  local script_path="${2:?script path required}"
  local probe_gas_wanted="${3:-$TX_GAS_WANTED_RUN}"

  local output
  output="$(run_script "$target_node" "$script_path" "$probe_gas_wanted" only)"
  printf '%s\n' "$output" >&2

  local gas_used
  gas_used="$(printf '%s\n' "$output" | awk '/GAS USED:/ {print $3; exit}')"
  [ -n "$gas_used" ] || die "failed to parse simulated gas usage for run on ${target_node}"

  printf '%s\n' "$gas_used"
}

send_coins() {
  local target_node="${1:?target node required}"
  local to_addr="${2:?destination address required}"
  local amount="${3:?amount required}"

  gnokey_tx_with_password \
    maketx send \
      --to "$to_addr" \
      --send "$amount" \
      --gas-fee "$TX_GAS_FEE" \
      --gas-wanted "$TX_GAS_WANTED_SEND" \
      --broadcast=true \
      --chainid "$CHAIN_ID" \
      --remote "${NODE_SERVICE[$target_node]}:26657" \
      --home /keys \
      --insecure-password-stdin \
      "$TX_KEY_NAME"
}

do_transaction() {
  local kind="${1:?transaction kind required}"
  shift || true

  case "$kind" in
    addpkg) add_pkg "$@" ;;
    call) call_realm "$@" ;;
    run) run_script "$@" ;;
    send) send_coins "$@" ;;
    *) die "unsupported transaction kind ${kind}" ;;
  esac
}

query_render() {
  local target_node="${1:?target node required}"
  local expr="${2:?render expression required}"

  docker run --rm --entrypoint /usr/bin/gnokey --network "$(docker_network_name)" "$GNOKEY_IMAGE" \
    query vm/qrender --data "$expr" --remote "${NODE_SERVICE[$target_node]}:26657"
}

container_id_for_node() {
  compose ps -q "$1"
}

node_ip() {
  local node="${1:?node required}"
  local container_id
  container_id="$(container_id_for_node "$node")"
  [ -n "$container_id" ] || return 1
  docker inspect "$container_id" | jq -r --arg network "$(docker_network_name)" '.[0].NetworkSettings.Networks[$network].IPAddress // empty'
}

rotate_sentry_ip() {
  local sentry="${1:?sentry name required}"
  # Optional second argument: name of a shell function to call while the sentry
  # is fully stopped (after removal, before bumpers and restart). Use it to run
  # assertions that require the sentry to be down.
  local while_down="${2:-}"
  [ "${NODE_ROLE[$sentry]:-}" = "sentry" ] || die "${sentry} is not a sentry"

  local old_ip
  local new_ip
  local bumper
  local bumper2

  old_ip="$(node_ip "$sentry" || true)"
  bumper="${PROJECT_NAME}-${sentry}-bump-1"
  bumper2="${PROJECT_NAME}-${sentry}-bump-2"

  compose stop "$sentry" >/dev/null
  compose rm -f "$sentry" >/dev/null
  docker rm -f "$bumper" "$bumper2" >/dev/null 2>&1 || true

  if [ -n "$while_down" ]; then
    "$while_down"
  fi

  docker run -d --rm --entrypoint sh --name "$bumper" --network "$(docker_network_name)" "$IMAGE_NAME" -c 'sleep 300' >/dev/null
  compose up -d "$sentry" >/dev/null
  _resolve_rpc_port "$sentry"
  wait_for_rpc "$sentry" 120
  new_ip="$(node_ip "$sentry" || true)"

  if [ -n "$old_ip" ] && [ "$old_ip" = "$new_ip" ]; then
    compose stop "$sentry" >/dev/null
    compose rm -f "$sentry" >/dev/null
    docker run -d --rm --entrypoint sh --name "$bumper2" --network "$(docker_network_name)" "$IMAGE_NAME" -c 'sleep 300' >/dev/null
    compose up -d "$sentry" >/dev/null
    _resolve_rpc_port "$sentry"
    wait_for_rpc "$sentry" 120
    new_ip="$(node_ip "$sentry" || true)"
  fi

  docker rm -f "$bumper" "$bumper2" >/dev/null 2>&1 || true
  [ -n "$new_ip" ] || die "failed to resolve a new IP for sentry ${sentry}"
  if [ -n "$old_ip" ] && [ "$old_ip" = "$new_ip" ]; then
    die "sentry ${sentry} kept IP ${new_ip} after recreation; rotation scenario was not exercised"
  fi
  log "sentry ${sentry} IP ${old_ip:-unknown} -> ${new_ip:-unknown}"
}

print_cluster_status() {
  local node
  for node in "${SCENARIO_NODES[@]}"; do
    if curl -fsS "$(node_rpc_url "$node")/status" >/dev/null 2>&1; then
      printf '%-16s role=%-10s height=%s rpc=%s\n' \
        "$node" \
        "${NODE_ROLE[$node]}" \
        "$(node_height "$node")" \
        "$(node_rpc_url "$node")"
    else
      printf '%-16s role=%-10s state=stopped rpc=%s\n' \
        "$node" \
        "${NODE_ROLE[$node]}" \
        "$(node_rpc_url "$node")"
    fi
  done
}

scenario_finish() {
  local sentry
  for sentry in "${SCENARIO_SENTRIES[@]+"${SCENARIO_SENTRIES[@]}"}"; do
    docker rm -f "${PROJECT_NAME}-${sentry}-bump-1" "${PROJECT_NAME}-${sentry}-bump-2" >/dev/null 2>&1 || true
  done
  if [ "${KEEP_UP:-0}" = "1" ]; then
    log "leaving network running because KEEP_UP=1"
    return 0
  fi
  stop_host_gnokms_ledger
  if [ -f "$COMPOSE_FILE" ]; then
    compose down --remove-orphans >/dev/null 2>&1 || true
  fi
}
