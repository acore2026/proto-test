#!/usr/bin/env bash
set -euo pipefail

HOST=""
REMOTE_DIR=""
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
LOG_DIR="runlogs"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) HOST="$2"; shift 2 ;;
    --remote-dir) REMOTE_DIR="$2"; shift 2 ;;
    --count) COUNT="$2"; shift 2 ;;
    --transport) TRANSPORT="$2"; shift 2 ;;
    --mode) MODE="$2"; shift 2 ;;
    --target-ip) REMOTE_IP="$2"; shift 2 ;;
    --target-port) REMOTE_PORT="$2"; shift 2 ;;
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
    --log-dir) LOG_DIR="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

if [[ -z "$HOST" || -z "$REMOTE_DIR" ]]; then
  echo "Usage: $0 --host user@server --remote-dir /path/to/mock5g [options]"
  exit 1
fi

cmd=(
  "cd '$REMOTE_DIR'"
  "./scripts/start_runners.sh"
  "--count '$COUNT'"
  "--transport '$TRANSPORT'"
  "--mode '$MODE'"
  "--remote-ip '$REMOTE_IP'"
  "--remote-port '$REMOTE_PORT'"
  "--duration '$DURATION'"
  "--workers '$WORKERS'"
  "--channels '$CHANNELS'"
  "--pps '$PPS'"
  "--step-count '$STEP_COUNT'"
  "--step-start-pps '$STEP_START_PPS'"
  "--step-increment '$STEP_INCREMENT'"
  "--step-duration '$STEP_DURATION'"
  "--log-dir '$LOG_DIR'"
)

if [[ -n "$NAS_TEMPLATE" ]]; then
  cmd+=("--nas-template '$NAS_TEMPLATE'")
fi
if [[ "$NAS_HEX" == "true" ]]; then
  cmd+=("--nas-hex")
fi

ssh "$HOST" "$(printf "%s " "${cmd[@]}")"
