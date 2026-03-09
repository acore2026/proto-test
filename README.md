# mock5g

Transport-pluggable mock gNB/AMF performance harness for SCTP-vs-QUIC benchmarking.

## Features

- Linux kernel SCTP backend (`--transport sctp`)
- QUIC transport backend (`--transport quic`) via `quic-go`
- Modes:
  - `latency` (closed-loop RTT)
  - `throughput` (step-rate sweep)
  - `flood` (custom NAS payload flood)
- CSV output with throughput, drops, and RTT percentiles
  - drop columns include reason breakdown (`drop_timeout`, `drop_send_err`, `drop_decode`, `drop_mismatch`, `drop_other`)

## Build

```bash
go build ./cmd/mock5g
```

## Run

Start AMF server:

```bash
./mock5g amf --transport sctp --listen-ip 127.0.0.1 --listen-port 38412
```

Latency run:

```bash
./mock5g gnb --mode latency --transport sctp --remote-ip 127.0.0.1 --remote-port 38412 --duration 10s --out-csv latency.csv
```

Throughput sweep:

```bash
./mock5g gnb --mode throughput --transport sctp --remote-ip 127.0.0.1 --remote-port 38412 --step-count 5 --step-start-pps 1000 --step-increment 1000 --step-duration 5s --out-csv throughput.csv
```

NAS flood:

```bash
./mock5g gnb --mode flood --transport sctp --remote-ip 127.0.0.1 --remote-port 38412 --nas-template ./nas.hex --nas-hex --pps 50000 --duration 30s --out-csv flood.csv
```

## Notes

- Requires Linux SCTP support in kernel.
- QUIC uses TLS 1.3. If `--cert-file` and `--key-file` are not provided on AMF, a self-signed cert is generated in-memory.
- For quick local testing, QUIC client skips cert verification unless `--ca-file` is provided.
- For fair SCTP vs QUIC latency comparisons, keep `--latency-rate-limit` enabled (default) so `--pps` is enforced in latency mode.

## Quick Test Scripts

Use these scripts to quickly start/stop many runners, including on a remote dedicated server.

Start AMF locally:

```bash
./scripts/start_amf.sh --listen-ip 0.0.0.0 --listen-port 38412
```

Start 20 local runners:

```bash
./scripts/start_runners.sh \
  --count 20 \
  --mode latency \
  --remote-ip 127.0.0.1 \
  --remote-port 38412 \
  --duration 30s \
  --workers 1 \
  --channels 1 \
  --pps 1000
```

Check status and stop:

```bash
./scripts/status_test.sh
./scripts/stop_test.sh
```

Start runners on a remote server over SSH:

```bash
./scripts/remote_start_runners.sh \
  --host user@dedicated-server \
  --remote-dir /opt/mock5g \
  --count 50 \
  --mode throughput \
  --target-ip 10.0.0.10 \
  --target-port 38412 \
  --duration 60s \
  --pps 5000
```

Logs and CSV files are written under `runlogs/` by default.

Protocol comparison helper:

```bash
./scripts/compare_protocols.sh --gnb-count 8 --duration 8s --pps 1000 --mode latency --latency-rate-limit true
```

This runs `1 AMF + N gNB` for SCTP and QUIC with identical settings, then prints an aggregated summary and writes per-runner logs/CSVs under a timestamped `runlogs/compare_*` folder.

## GitHub Automation

- CI workflow: `.github/workflows/ci.yml`
  - Runs `go build ./...` and `go test ./...` on pushes to `main` and pull requests.
- Release workflow: `.github/workflows/release.yml`
  - Triggers when pushing tags matching `v*` (for example `v1.0.0`).
  - Builds `mock5g` binaries for `linux/darwin/windows` and `amd64/arm64`.
  - Uploads ready-to-use archives containing `bin/`, `scripts/`, `config/config.example.yaml`, `README.md`, and `QUICKSTART.txt`.

Create a release tag:

```bash
git tag v1.0.0
git push origin v1.0.0
```
