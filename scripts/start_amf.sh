#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_PATH="$ROOT_DIR/bin/mock5g"
LOG_DIR="$ROOT_DIR/runlogs"
TRANSPORT="sctp"
LISTEN_IP="127.0.0.1"
LISTEN_PORT="38412"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin) BIN_PATH="$2"; shift 2 ;;
    --log-dir) LOG_DIR="$2"; shift 2 ;;
    --transport) TRANSPORT="$2"; shift 2 ;;
    --listen-ip) LISTEN_IP="$2"; shift 2 ;;
    --listen-port) LISTEN_PORT="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

mkdir -p "$LOG_DIR" "$(dirname "$BIN_PATH")"

if [[ ! -x "$BIN_PATH" ]]; then
  echo "Building mock5g binary at $BIN_PATH"
  (cd "$ROOT_DIR" && go build -o "$BIN_PATH" ./cmd/mock5g)
fi

AMF_PID_FILE="$LOG_DIR/amf.pid"
if [[ -f "$AMF_PID_FILE" ]] && kill -0 "$(cat "$AMF_PID_FILE")" 2>/dev/null; then
  echo "AMF already running with PID $(cat "$AMF_PID_FILE")"
  exit 0
fi

AMF_LOG="$LOG_DIR/amf.log"
nohup "$BIN_PATH" amf \
  --transport "$TRANSPORT" \
  --listen-ip "$LISTEN_IP" \
  --listen-port "$LISTEN_PORT" \
  >"$AMF_LOG" 2>&1 &

echo $! > "$AMF_PID_FILE"
echo "AMF started: pid=$(cat "$AMF_PID_FILE"), log=$AMF_LOG"
