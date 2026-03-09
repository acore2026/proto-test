#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_PATH="$ROOT_DIR/bin/mock5g"
LOG_DIR="$ROOT_DIR/runlogs/scale_compare_$(date +%Y%m%d_%H%M%S)"
GO_BIN="$(command -v go || true)"
if [[ -z "$GO_BIN" && -x /usr/local/go/bin/go ]]; then
  GO_BIN="/usr/local/go/bin/go"
fi

COUNTS="4,16,64,256"
DURATION="10s"
PPS=50
WORKERS=1
CHANNELS=1
MODE="latency"
LATENCY_RATE_LIMIT="true"
BASE_PORT=42000
STAGGER_MS=10

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin) BIN_PATH="$2"; shift 2 ;;
    --log-dir) LOG_DIR="$2"; shift 2 ;;
    --counts) COUNTS="$2"; shift 2 ;;
    --duration) DURATION="$2"; shift 2 ;;
    --pps) PPS="$2"; shift 2 ;;
    --workers) WORKERS="$2"; shift 2 ;;
    --channels) CHANNELS="$2"; shift 2 ;;
    --mode) MODE="$2"; shift 2 ;;
    --latency-rate-limit) LATENCY_RATE_LIMIT="$2"; shift 2 ;;
    --base-port) BASE_PORT="$2"; shift 2 ;;
    --stagger-ms) STAGGER_MS="$2"; shift 2 ;;
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

summary_csv="$LOG_DIR/summary.csv"
echo "scenario,gnb_count,protocol,tls,runners,mode,duration,pps,workers,channels,ach_pps,tx,rx,drop,drop_timeout,drop_send_err,drop_decode,drop_mismatch,drop_other,avg_p50_us,avg_p95_us,avg_p99_us,rc" > "$summary_csv"

run_case() {
  local scenario="$1"
  local gnb_count="$2"
  local proto="$3"
  local tls_mode="$4"
  local port="$5"

  local scenario_dir="$LOG_DIR/$scenario"
  mkdir -p "$scenario_dir"
  local amf_log="$scenario_dir/amf.log"

  local tls_args=()
  if [[ "$tls_mode" == "true" ]]; then
    tls_args+=(--tls)
  fi

  "$BIN_PATH" amf --transport "$proto" "${tls_args[@]}" --listen-ip 127.0.0.1 --listen-port "$port" > "$amf_log" 2>&1 &
  local amf_pid=$!
  sleep 1

  local -a gnb_pids=()
  local i
  for ((i=1; i<=gnb_count; i++)); do
    local runner_log="$scenario_dir/runner_${i}.log"
    local runner_csv="$scenario_dir/runner_${i}.csv"
    "$BIN_PATH" gnb \
      --mode "$MODE" \
      --transport "$proto" \
      "${tls_args[@]}" \
      --remote-ip 127.0.0.1 \
      --remote-port "$port" \
      --duration "$DURATION" \
      --workers "$WORKERS" \
      --channels "$CHANNELS" \
      --pps "$PPS" \
      --latency-rate-limit="$LATENCY_RATE_LIMIT" \
      --out-csv "$runner_csv" > "$runner_log" 2>&1 &
    gnb_pids+=("$!")
    if [[ "$STAGGER_MS" -gt 0 ]]; then
      sleep "0.$(printf "%03d" "$STAGGER_MS")"
    fi
  done

  local rc=0
  local pid
  for pid in "${gnb_pids[@]}"; do
    wait "$pid" || rc=1
  done

  kill "$amf_pid" >/dev/null 2>&1 || true
  wait "$amf_pid" >/dev/null 2>&1 || true

  local total_ach=0 total_tx=0 total_rx=0 total_drop=0
  local total_to=0 total_se=0 total_de=0 total_mm=0 total_ot=0
  local sum_p50=0 sum_p95=0 sum_p99=0 n=0
  local f row
  for f in "$scenario_dir"/runner_*.csv; do
    row=$(awk -F, 'NR>1{last=$0} END{print last}' "$f")
    [[ -n "$row" ]] || continue
    ach=$(echo "$row" | awk -F, '{print $5}')
    tx=$(echo "$row" | awk -F, '{print $6}')
    rx=$(echo "$row" | awk -F, '{print $7}')
    dr=$(echo "$row" | awk -F, '{print $8}')
    d_to=$(echo "$row" | awk -F, '{print $9}')
    d_se=$(echo "$row" | awk -F, '{print $10}')
    d_de=$(echo "$row" | awk -F, '{print $11}')
    d_mm=$(echo "$row" | awk -F, '{print $12}')
    d_ot=$(echo "$row" | awk -F, '{print $13}')
    p50=$(echo "$row" | awk -F, '{print $14}')
    p95=$(echo "$row" | awk -F, '{print $15}')
    p99=$(echo "$row" | awk -F, '{print $16}')

    total_ach=$((total_ach + ach))
    total_tx=$((total_tx + tx))
    total_rx=$((total_rx + rx))
    total_drop=$((total_drop + dr))
    total_to=$((total_to + d_to))
    total_se=$((total_se + d_se))
    total_de=$((total_de + d_de))
    total_mm=$((total_mm + d_mm))
    total_ot=$((total_ot + d_ot))
    sum_p50=$((sum_p50 + p50))
    sum_p95=$((sum_p95 + p95))
    sum_p99=$((sum_p99 + p99))
    n=$((n + 1))
  done

  local avg_p50=0 avg_p95=0 avg_p99=0
  if [[ "$n" -gt 0 ]]; then
    avg_p50=$((sum_p50 / n))
    avg_p95=$((sum_p95 / n))
    avg_p99=$((sum_p99 / n))
  fi

  echo "$scenario,$gnb_count,$proto,$tls_mode,$n,$MODE,$DURATION,$PPS,$WORKERS,$CHANNELS,$total_ach,$total_tx,$total_rx,$total_drop,$total_to,$total_se,$total_de,$total_mm,$total_ot,$avg_p50,$avg_p95,$avg_p99,$rc" >> "$summary_csv"
  echo "$scenario rc=$rc runners=$n ach_pps=$total_ach drop=$total_drop p50=$avg_p50 p95=$avg_p95 p99=$avg_p99"
}

IFS=',' read -r -a count_arr <<< "$COUNTS"
idx=0
for c in "${count_arr[@]}"; do
  c="$(echo "$c" | xargs)"
  [[ -n "$c" ]] || continue

  run_case "gnb_${c}_sctp_kernel_tls" "$c" "sctp-kernel" "true" "$((BASE_PORT + idx))"
  idx=$((idx + 1))

  run_case "gnb_${c}_sctp_kernel" "$c" "sctp-kernel" "false" "$((BASE_PORT + idx))"
  idx=$((idx + 1))

  run_case "gnb_${c}_quic" "$c" "quic" "false" "$((BASE_PORT + idx))"
  idx=$((idx + 1))
done

echo
printf "%-24s %-6s %-12s %-6s %-8s %-8s %-8s %-8s %-8s %-8s\n" "scenario" "gnb" "protocol" "tls" "ach_pps" "drop" "p50" "p95" "p99" "rc"
awk -F, 'NR>1 {printf "%-24s %-6s %-12s %-6s %-8s %-8s %-8s %-8s %-8s %-8s\n", $1, $2, $3, $4, $11, $14, $20, $21, $22, $23}' "$summary_csv"

echo "Summary CSV: $summary_csv"
echo "Logs dir: $LOG_DIR"
