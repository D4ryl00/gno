#!/usr/bin/env bash
set -euo pipefail

if [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
  printf 'error: bash 4+ required (found %s); install with: brew install bash\n' "$BASH_VERSION" >&2
  exit 1
fi

SCENARIO_SELF="${BASH_SOURCE[0]}"
SCENARIO_LIB_DIR="$(cd "$(dirname "${SCENARIO_SELF}")" && pwd)"
REPO_ROOT="$(cd "${SCENARIO_LIB_DIR}/.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-gnoland:local}"
GNOKEY_IMAGE="${GNOKEY_IMAGE:-gnokey:local}"
GNOGENESIS_IMAGE="${GNOGENESIS_IMAGE:-gnogenesis:local}"
GNO_ROOT="${GNO_ROOT:-${REPO_ROOT}/gno}"
WORK_ROOT="${WORK_ROOT:-/tmp/gno-val-tests}"
CHAIN_ID="${CHAIN_ID:-dev}"
TIMEOUT_COMMIT="${TIMEOUT_COMMIT:-1s}"
LOG_LEVEL="${LOG_LEVEL:-info}"
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
declare -a SCENARIO_SENTRIES=()
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

  printf '[%s] scenario dir: %s\n' "$SCENARIO_NAME" "$SCENARIO_DIR"

  SCENARIO_NODES=()
  SCENARIO_VALIDATORS=()
  SCENARIO_SENTRIES=()
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
}

next_rpc_port() {
  printf '%s' "$((26657 + (${#SCENARIO_NODES[@]} * 100)))"
}

register_node() {
  local name="${1:?node name required}"
  local role="${2:?role required}"
  local rpc_port="${3:?rpc port required}"
  local pex="${4:?pex required}"
  local sentry="${5:-}"

  [ -z "${NODE_ROLE[$name]:-}" ] || die "node ${name} already exists"

  SCENARIO_NODES+=("$name")
  NODE_ROLE[$name]="$role"
  NODE_SERVICE[$name]="$name"
  NODE_MONIKER[$name]="$name"
  NODE_RPC_PORT[$name]="$rpc_port"
  NODE_PEX[$name]="$pex"
  NODE_SENTRY[$name]="$sentry"

  case "$role" in
    validator) SCENARIO_VALIDATORS+=("$name") ;;
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
      *)
        die "unknown gen_validator option: $1"
        ;;
    esac
  done

  if [ -z "$rpc_port" ]; then
    rpc_port="$(next_rpc_port)"
  fi

  register_node "$name" validator "$rpc_port" "$pex" "$sentry"
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

  if [ -z "$rpc_port" ]; then
    rpc_port="$(next_rpc_port)"
  fi

  register_node "$name" sentry "$rpc_port" "$pex" ""
}

ensure_image_exists() {
  local image_id
  image_id="$(docker images -q "$IMAGE_NAME" 2>/dev/null)"
  if [ -z "$image_id" ]; then
    die "docker image ${IMAGE_NAME} not found; run \`make build-gnoland-image\` first"
  fi
  image_id="$(docker images -q "$GNOKEY_IMAGE" 2>/dev/null)"
  if [ -z "$image_id" ]; then
    die "docker image ${GNOKEY_IMAGE} not found; run \`make build-gnokey-image\` first"
  fi
  image_id="$(docker images -q "$GNOGENESIS_IMAGE" 2>/dev/null)"
  if [ -z "$image_id" ]; then
    die "docker image ${GNOGENESIS_IMAGE} not found; run \`make build-gnogenesis-image\` first"
  fi
}

compose() {
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
}

run_in_image() {
  docker run --rm "$@"
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
# Uses --entrypoint gnogenesis because gnocontribs image ENTRYPOINT is /bin/sh -c.
_gnogenesis() {
  docker run --rm \
    --entrypoint gnogenesis \
    -v "${SCENARIO_DIR}:/work" \
    -v "${GNO_ROOT}:/gnoroot:ro" \
    "$GNOGENESIS_IMAGE" \
    "$@"
}

# _gnokey_deployer runs a gnokey command with the genesis deployer key home mounted.
_gnokey_deployer() {
  docker run -i --rm \
    -v "${SCENARIO_DIR}:/work" \
    "$GNOKEY_IMAGE" \
    "$@"
}

generate_genesis() {
  [ "${#SCENARIO_VALIDATORS[@]}" -gt 0 ] || die "at least one validator is required"
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
      -v "${gnokey_home}:/keys" \
      "$GNOKEY_IMAGE" \
      add --recover "$deployer_name" --home /keys --insecure-password-stdin >/dev/null

  log "generating empty genesis"
  docker run --rm \
    --entrypoint gnogenesis \
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
      --entrypoint gnogenesis \
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
  for node in "${SCENARIO_VALIDATORS[@]}"; do
    valset_entries+="$(printf '\t\t\t\t{Address: address("%s"), PubKey: "%s", VotingPower: 10},\n' \
      "${NODE_ADDRESS[$node]}" "${NODE_PUBKEY[$node]}")"
  done
  awk -v entries="$valset_entries" \
    '/\/\/ GEN:VALSET/ { printf "%s", entries; next } { print }' \
    "${SCENARIO_LIB_DIR}/valset-init.gno.tpl" > "$valset_file"

  local setup_tx="${genesis_work}/valset-init-tx.json"
  local setup_tx_jsonl="${genesis_work}/valset-init-tx.jsonl"

  _gnokey_deployer \
    maketx run \
      --gas-wanted 100000000 \
      --gas-fee 1ugnot \
      --chainid "$CHAIN_ID" \
      --home /work/genesis-work/gnokey-home \
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

  log "adding ${#SCENARIO_VALIDATORS[@]} validators to consensus layer"
  for node in "${SCENARIO_VALIDATORS[@]}"; do
    _gnogenesis validator add \
      --genesis-path /work/genesis.json \
      --name "$node" \
      --address "${NODE_ADDRESS[$node]}" \
      --pub-key "${NODE_PUBKEY[$node]}" \
      --power 10 >/dev/null
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
        if [ -z "${NODE_SENTRY[$target]}" ] || [ "${NODE_SENTRY[$target]}" = "$node" ]; then
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

configure_nodes() {
  local node
  for node in "${SCENARIO_NODES[@]}"; do
    local peers
    peers="$(persistent_peers_for_node "$node")"

    set_config_value "$node" moniker "${NODE_MONIKER[$node]}"
    set_config_value "$node" rpc.laddr "tcp://0.0.0.0:26657"
    set_config_value "$node" p2p.laddr "tcp://0.0.0.0:26656"
    set_config_value "$node" p2p.pex "${NODE_PEX[$node]}"
    set_config_value "$node" p2p.persistent_peers "$peers"
    set_config_value "$node" p2p.seeds "$peers"
    set_config_value "$node" consensus.timeout_commit "$TIMEOUT_COMMIT"
  done
}

write_compose_file() {
  {
    printf 'name: %s\n\n' "$PROJECT_NAME"
    printf 'services:\n'
    local node
    for node in "${SCENARIO_NODES[@]}"; do
      printf '  %s:\n' "${NODE_SERVICE[$node]}"
      printf '    image: "%s"\n' "$IMAGE_NAME"
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
      printf '      - "%s:26657"\n' "${NODE_RPC_PORT[$node]}"
      printf '    networks:\n'
      printf '      - chain\n'
      printf '    stop_grace_period: 5s\n'
    done
    printf '\nnetworks:\n'
    printf '  chain: {}\n'
  } > "$COMPOSE_FILE"
}

create_tx_key() {
  mkdir -p "$KEY_HOME"
  if find "$KEY_HOME" -mindepth 1 -print -quit | grep -q .; then
    return
  fi

  printf '%s\n%s\n%s\n' "$TX_MNEMONIC" "$TX_PASSWORD" "$TX_PASSWORD" | \
    docker run -i --rm -v "${KEY_HOME}:/keys" "$GNOKEY_IMAGE" \
      add "$TX_KEY_NAME" --home /keys --recover --quiet --insecure-password-stdin >/dev/null
}

prepare_network() {
  require_tools
  ensure_image_exists

  [ "${#SCENARIO_NODES[@]}" -gt 0 ] || die "no nodes declared"

  rm -rf "$SCENARIO_DIR"
  mkdir -p "$SCENARIO_DIR"

  init_node_dirs
  collect_node_ids
  generate_genesis
  configure_nodes
  write_compose_file
  create_tx_key

  log "prepared network in ${SCENARIO_DIR}"
}

node_rpc_url() {
  local node="${1:?node required}"
  printf 'http://127.0.0.1:%s' "${NODE_RPC_PORT[$node]}"
}

wait_for_rpc() {
  local node="${1:?node required}"
  local timeout="${2:-60}"
  local i
  for i in $(seq 1 "$timeout"); do
    if curl -fsS "$(node_rpc_url "$node")/status" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  die "rpc for ${node} did not come up within ${timeout}s"
}

_capture_node_logs() {
  local node="${1:?node required}"
  mkdir -p "${SCENARIO_DIR}/logs"
  compose logs -f "$node" >> "${SCENARIO_DIR}/logs/${node}.log" 2>&1 &
}

start_node() {
  local node="${1:?node required}"
  compose up -d "$node" >/dev/null
  wait_for_rpc "$node" 90
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
  local -a ordered=()
  local node
  for node in "${SCENARIO_SENTRIES[@]}"; do
    ordered+=("$node")
  done
  for node in "${SCENARIO_VALIDATORS[@]}"; do
    ordered+=("$node")
  done

  [ "${#ordered[@]}" -gt 0 ] || die "no nodes to start"
  compose up -d "${ordered[@]}" >/dev/null

  for node in "${ordered[@]}"; do
    wait_for_rpc "$node" 90
    _capture_node_logs "$node"
  done

  log "started ${#ordered[@]} node(s)"
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
  rm -rf "${NODE_DATA_DIR[$node]}/db" "${NODE_DATA_DIR[$node]}/wal"
  printf '{"height":"0","round":"0","step":0}\n' > "${NODE_DATA_DIR[$node]}/secrets/priv_validator_state.json"
  cp "${SCENARIO_DIR}/genesis.json" "${NODE_DATA_DIR[$node]}/genesis.json"
  log "reset ${node}"
}

reset_validator() {
  reset_node "$1"
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

  local abs_script
  local script_dir
  local script_name
  abs_script="$(cd "$(dirname "$script_path")" && pwd)/$(basename "$script_path")"
  script_dir="$(dirname "$abs_script")"
  script_name="$(basename "$abs_script")"

  gnokey_tx_with_password \
    -v "${script_dir}:/script:ro" \
    maketx run \
      --gas-fee "$TX_GAS_FEE" \
      --gas-wanted "$gas_wanted" \
      --broadcast=true \
      --chainid "$CHAIN_ID" \
      --remote "${NODE_SERVICE[$target_node]}:26657" \
      --home /keys \
      --insecure-password-stdin \
      "$TX_KEY_NAME" \
      "/script/${script_name}"
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

  docker run --rm --network "$(docker_network_name)" "$GNOKEY_IMAGE" \
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

  docker run -d --rm --entrypoint sh --name "$bumper" --network "$(docker_network_name)" "$IMAGE_NAME" -c 'sleep 300' >/dev/null
  compose up -d "$sentry" >/dev/null
  wait_for_rpc "$sentry" 90
  new_ip="$(node_ip "$sentry" || true)"

  if [ -n "$old_ip" ] && [ "$old_ip" = "$new_ip" ]; then
    compose stop "$sentry" >/dev/null
    compose rm -f "$sentry" >/dev/null
    docker run -d --rm --name "$bumper2" --network "$(docker_network_name)" "$IMAGE_NAME" sh -c 'sleep 300' >/dev/null
    compose up -d "$sentry" >/dev/null
    wait_for_rpc "$sentry" 90
    new_ip="$(node_ip "$sentry" || true)"
  fi

  docker rm -f "$bumper" "$bumper2" >/dev/null 2>&1 || true
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
  if [ -f "$COMPOSE_FILE" ]; then
    compose down --remove-orphans >/dev/null 2>&1 || true
  fi
}
