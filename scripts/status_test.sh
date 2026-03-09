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

print_file_status() {
  local name="$1"
  local file="$2"
  if [[ ! -f "$file" ]]; then
    echo "$name: no pid file"
    return
  fi
  local alive=0
  while IFS= read -r pid; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      echo "$name: running pid=$pid"
      alive=1
    fi
  done < "$file"
  if [[ "$alive" -eq 0 ]]; then
    echo "$name: not running (stale pid file)"
  fi
}

print_file_status "amf" "$LOG_DIR/amf.pid"
print_file_status "runners" "$LOG_DIR/runners.pid"
