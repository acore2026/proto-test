#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_PATH="$ROOT_DIR/bin/mock5g"
LOG_DIR="$ROOT_DIR/runlogs/compare_$(date +%Y%m%d_%H%M%S)"
GO_BIN="$(command -v go || true)"
if [[ -z "$GO_BIN" && -x /usr/local/go/bin/go ]]; then
  GO_BIN="/usr/local/go/bin/go"
fi

GNB_COUNT=8
DURATION="8s"
PPS=1000
WORKERS=1
CHANNELS=1
MODE="latency"
LATENCY_RATE_LIMIT="true"
BASE_PORT=38600

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin) BIN_PATH="$2"; shift 2 ;;
    --log-dir) LOG_DIR="$2"; shift 2 ;;
    --gnb-count) GNB_COUNT="$2"; shift 2 ;;
    --duration) DURATION="$2"; shift 2 ;;
    --pps) PPS="$2"; shift 2 ;;
    --workers) WORKERS="$2"; shift 2 ;;
    --channels) CHANNELS="$2"; shift 2 ;;
    --mode) MODE="$2"; shift 2 ;;
    --latency-rate-limit) LATENCY_RATE_LIMIT="$2"; shift 2 ;;
    --base-port) BASE_PORT="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

mkdir -p "$LOG_DIR" "$(dirname "$BIN_PATH")"

if [[ ! -x "$BIN_PATH" ]]; then
  if [[ -z "$GO_BIN" ]]; then
    echo "go binary not found and $BIN_PATH does not exist"
    exit 1
  fi
  echo "Building mock5g binary at $BIN_PATH"
  (cd "$ROOT_DIR" && "$GO_BIN" build -o "$BIN_PATH" ./cmd/mock5g)
fi

run_protocol() {
  local proto="$1"
  local port="$2"
  local amf_log="$LOG_DIR/${proto}_amf.log"
  local amf_pid=""

  "$BIN_PATH" amf --transport "$proto" --listen-ip 127.0.0.1 --listen-port "$port" > "$amf_log" 2>&1 &
  amf_pid=$!
  sleep 1

  local pids=()
  local i
  for ((i=1; i<=GNB_COUNT; i++)); do
    local runner_csv="$LOG_DIR/${proto}_runner_${i}.csv"
    local runner_log="$LOG_DIR/${proto}_runner_${i}.log"

    "$BIN_PATH" gnb \
      --mode "$MODE" \
      --transport "$proto" \
      --remote-ip 127.0.0.1 \
      --remote-port "$port" \
      --duration "$DURATION" \
      --workers "$WORKERS" \
      --channels "$CHANNELS" \
      --pps "$PPS" \
      --latency-rate-limit "$LATENCY_RATE_LIMIT" \
      --out-csv "$runner_csv" > "$runner_log" 2>&1 &
    pids+=("$!")
  done

  local rc=0
  for pid in "${pids[@]}"; do
    wait "$pid" || rc=1
  done

  kill "$amf_pid" >/dev/null 2>&1 || true
  wait "$amf_pid" >/dev/null 2>&1 || true

  summarize "$proto" "$rc"
}

summarize() {
  local proto="$1"
  local rc="$2"
  local total_ach=0 total_tx=0 total_rx=0 total_drop=0
  local sum_p50=0 sum_p95=0 sum_p99=0
  local sum_timeout=0 sum_senderr=0 sum_decode=0 sum_mismatch=0 sum_other=0
  local n=0

  local f
  shopt -s nullglob
  for f in "$LOG_DIR/${proto}"_runner_*.csv; do
    local row
    row=$(awk -F, 'NR>1{last=$0} END{print last}' "$f")
    [[ -n "$row" ]] || continue

    local ach tx rx drop timeout senderr decode mismatch other p50 p95 p99
    ach=$(echo "$row" | awk -F, '{print $5}')
    tx=$(echo "$row" | awk -F, '{print $6}')
    rx=$(echo "$row" | awk -F, '{print $7}')
    drop=$(echo "$row" | awk -F, '{print $8}')
    timeout=$(echo "$row" | awk -F, '{print $9}')
    senderr=$(echo "$row" | awk -F, '{print $10}')
    decode=$(echo "$row" | awk -F, '{print $11}')
    mismatch=$(echo "$row" | awk -F, '{print $12}')
    other=$(echo "$row" | awk -F, '{print $13}')
    p50=$(echo "$row" | awk -F, '{print $14}')
    p95=$(echo "$row" | awk -F, '{print $15}')
    p99=$(echo "$row" | awk -F, '{print $16}')

    total_ach=$((total_ach + ach))
    total_tx=$((total_tx + tx))
    total_rx=$((total_rx + rx))
    total_drop=$((total_drop + drop))
    sum_timeout=$((sum_timeout + timeout))
    sum_senderr=$((sum_senderr + senderr))
    sum_decode=$((sum_decode + decode))
    sum_mismatch=$((sum_mismatch + mismatch))
    sum_other=$((sum_other + other))
    sum_p50=$((sum_p50 + p50))
    sum_p95=$((sum_p95 + p95))
    sum_p99=$((sum_p99 + p99))
    n=$((n + 1))
  done

  if [[ "$n" -eq 0 ]]; then
    echo "$proto rc=$rc runners=0"
    return
  fi

  local avg_p50 avg_p95 avg_p99
  avg_p50=$((sum_p50 / n))
  avg_p95=$((sum_p95 / n))
  avg_p99=$((sum_p99 / n))

  echo "$proto rc=$rc runners=$n total_achieved_pps=$total_ach total_tx=$total_tx total_rx=$total_rx total_drop=$total_drop drop_timeout=$sum_timeout drop_send_err=$sum_senderr drop_decode=$sum_decode drop_mismatch=$sum_mismatch drop_other=$sum_other avg_p50_us=$avg_p50 avg_p95_us=$avg_p95 avg_p99_us=$avg_p99" \
    | tee "$LOG_DIR/${proto}_summary.txt"
}

echo "Running comparison: mode=$MODE gnb_count=$GNB_COUNT duration=$DURATION pps=$PPS workers=$WORKERS channels=$CHANNELS latency_rate_limit=$LATENCY_RATE_LIMIT"
run_protocol sctp "$BASE_PORT"
run_protocol quic "$((BASE_PORT + 1))"

echo
printf "%-8s %-8s %-12s %-10s %-10s %-10s %-11s %-11s %-11s\n" "proto" "runners" "ach_pps" "drop" "drop_to" "drop_se" "avg_p50_us" "avg_p95_us" "avg_p99_us"
for proto in sctp quic; do
  summary_file="$LOG_DIR/${proto}_summary.txt"
  runners=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^runners=/){split($i,a,"="); print a[2]}}' "$summary_file")
  ach=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^total_achieved_pps=/){split($i,a,"="); print a[2]}}' "$summary_file")
  drop=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^total_drop=/){split($i,a,"="); print a[2]}}' "$summary_file")
  dto=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^drop_timeout=/){split($i,a,"="); print a[2]}}' "$summary_file")
  dse=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^drop_send_err=/){split($i,a,"="); print a[2]}}' "$summary_file")
  p50=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^avg_p50_us=/){split($i,a,"="); print a[2]}}' "$summary_file")
  p95=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^avg_p95_us=/){split($i,a,"="); print a[2]}}' "$summary_file")
  p99=$(awk '{for(i=1;i<=NF;i++) if($i ~ /^avg_p99_us=/){split($i,a,"="); print a[2]}}' "$summary_file")
  printf "%-8s %-8s %-12s %-10s %-10s %-10s %-11s %-11s %-11s\n" "$proto" "$runners" "$ach" "$drop" "$dto" "$dse" "$p50" "$p95" "$p99"
done

echo "Detailed logs/CSVs: $LOG_DIR"
