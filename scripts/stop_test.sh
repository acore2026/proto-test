#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="$ROOT_DIR/runlogs"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --log-dir) LOG_DIR="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

kill_from_file() {
  local file="$1"
  if [[ ! -f "$file" ]]; then
    return 0
  fi
  while IFS= read -r pid; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      echo "Stopped pid=$pid"
    fi
  done < "$file"
  rm -f "$file"
}

kill_from_file "$LOG_DIR/runners.pid"
kill_from_file "$LOG_DIR/amf.pid"

echo "Stop complete"
