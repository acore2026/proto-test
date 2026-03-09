#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_PATH="$ROOT_DIR/bin/mock5g"
LOG_DIR="$ROOT_DIR/runlogs"
COUNT=1
TRANSPORT="sctp"
MODE="latency"
REMOTE_IP="127.0.0.1"
REMOTE_PORT="38412"
DURATION="10s"
WORKERS=1
CHANNELS=1
PPS=1000
STEP_COUNT=5
STEP_START_PPS=1000
STEP_INCREMENT=1000
STEP_DURATION="5s"
NAS_TEMPLATE=""
NAS_HEX="false"
STAGGER_MS=100

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin) BIN_PATH="$2"; shift 2 ;;
    --log-dir) LOG_DIR="$2"; shift 2 ;;
    --count) COUNT="$2"; shift 2 ;;
    --transport) TRANSPORT="$2"; shift 2 ;;
    --mode) MODE="$2"; shift 2 ;;
    --remote-ip) REMOTE_IP="$2"; shift 2 ;;
    --remote-port) REMOTE_PORT="$2"; shift 2 ;;
    --duration) DURATION="$2"; shift 2 ;;
    --workers) WORKERS="$2"; shift 2 ;;
    --channels) CHANNELS="$2"; shift 2 ;;
    --pps) PPS="$2"; shift 2 ;;
    --step-count) STEP_COUNT="$2"; shift 2 ;;
    --step-start-pps) STEP_START_PPS="$2"; shift 2 ;;
    --step-increment) STEP_INCREMENT="$2"; shift 2 ;;
    --step-duration) STEP_DURATION="$2"; shift 2 ;;
    --nas-template) NAS_TEMPLATE="$2"; shift 2 ;;
    --nas-hex) NAS_HEX="true"; shift 1 ;;
    --stagger-ms) STAGGER_MS="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

if [[ "$COUNT" -lt 1 ]]; then
  echo "--count must be >= 1"
  exit 1
fi

mkdir -p "$LOG_DIR" "$(dirname "$BIN_PATH")"

if [[ ! -x "$BIN_PATH" ]]; then
  echo "Building mock5g binary at $BIN_PATH"
  (cd "$ROOT_DIR" && go build -o "$BIN_PATH" ./cmd/mock5g)
fi

RUNNER_PIDS_FILE="$LOG_DIR/runners.pid"
: > "$RUNNER_PIDS_FILE"

for ((i=1; i<=COUNT; i++)); do
  runner_log="$LOG_DIR/runner_${i}.log"
  runner_csv="$LOG_DIR/runner_${i}.csv"

  args=(
    gnb
    --mode "$MODE"
    --transport "$TRANSPORT"
    --remote-ip "$REMOTE_IP"
    --remote-port "$REMOTE_PORT"
    --duration "$DURATION"
    --workers "$WORKERS"
    --channels "$CHANNELS"
    --pps "$PPS"
    --step-count "$STEP_COUNT"
    --step-start-pps "$STEP_START_PPS"
    --step-increment "$STEP_INCREMENT"
    --step-duration "$STEP_DURATION"
    --out-csv "$runner_csv"
  )

  if [[ -n "$NAS_TEMPLATE" ]]; then
    args+=(--nas-template "$NAS_TEMPLATE")
  fi
  if [[ "$NAS_HEX" == "true" ]]; then
    args+=(--nas-hex)
  fi

  nohup "$BIN_PATH" "${args[@]}" >"$runner_log" 2>&1 &
  pid=$!
  echo "$pid" >> "$RUNNER_PIDS_FILE"
  echo "Runner $i started: pid=$pid log=$runner_log csv=$runner_csv"

  if [[ "$STAGGER_MS" -gt 0 ]]; then
    sleep "0.$(printf "%03d" "$STAGGER_MS")"
  fi
done

echo "All runners started. PID file: $RUNNER_PIDS_FILE"
