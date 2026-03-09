package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mock5g/pkg/app"
	"mock5g/pkg/config"
	"mock5g/pkg/transport"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "amf":
		err = runAMF(ctx, args)
	case "gnb":
		err = runGNB(ctx, args)
	default:
		usage()
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runAMF(ctx context.Context, args []string) error {
	cfg := config.Default()
	if p := configPathFromArgs(args); p != "" {
		loaded, err := config.Load(p)
		if err != nil {
			return err
		}
		cfg = loaded
	}

	fs := flag.NewFlagSet("amf", flag.ExitOnError)
	_ = fs.String("config", "", "path to YAML config")
	transportType := fs.String("transport", string(cfg.Transport.Type), "transport type: sctp|quic")
	listenIP := fs.String("listen-ip", cfg.Transport.LocalIP, "listen IP")
	listenPort := fs.Int("listen-port", cfg.Transport.LocalPort, "listen port")
	channels := fs.Int("channels", cfg.Transport.ChannelCount, "logical channel count")
	nodelay := fs.Bool("nodelay", cfg.Transport.NoDelay, "enable nodelay")
	heartbeatMS := fs.Int("heartbeat-ms", cfg.Transport.HeartbeatMS, "heartbeat interval ms")
	_ = fs.Parse(args)

	cfg.Transport.Type = transport.Type(*transportType)
	cfg.Transport.LocalIP = *listenIP
	cfg.Transport.LocalPort = *listenPort
	cfg.Transport.ChannelCount = *channels
	cfg.Transport.NoDelay = *nodelay
	cfg.Transport.HeartbeatMS = *heartbeatMS

	server := app.AMFServer{TransportCfg: cfg.Transport}
	return server.Run(ctx)
}

func runGNB(ctx context.Context, args []string) error {
	cfg := config.Default()
	if p := configPathFromArgs(args); p != "" {
		loaded, err := config.Load(p)
		if err != nil {
			return err
		}
		cfg = loaded
	}

	fs := flag.NewFlagSet("gnb", flag.ExitOnError)
	_ = fs.String("config", "", "path to YAML config")
	mode := fs.String("mode", cfg.Run.Mode, "run mode: latency|throughput|flood")
	transportType := fs.String("transport", string(cfg.Transport.Type), "transport type: sctp|quic")
	remoteIP := fs.String("remote-ip", cfg.Transport.RemoteIP, "remote AMF IP")
	remotePort := fs.Int("remote-port", cfg.Transport.RemotePort, "remote AMF port")
	localIP := fs.String("local-ip", cfg.Transport.LocalIP, "local bind IP")
	localPort := fs.Int("local-port", 0, "local bind port (optional)")
	workers := fs.Int("workers", cfg.Run.Workers, "number of workers")
	channels := fs.Int("channels", cfg.Transport.ChannelCount, "logical channel count")
	pps := fs.Int("pps", cfg.Run.PPS, "target pps")
	duration := fs.Duration("duration", cfg.Run.Duration, "run duration")
	stepCount := fs.Int("step-count", cfg.Run.StepCount, "throughput step count")
	stepStart := fs.Int("step-start-pps", cfg.Run.StepStartPPS, "throughput start pps")
	stepInc := fs.Int("step-increment", cfg.Run.StepIncrement, "throughput increment pps")
	stepDuration := fs.Duration("step-duration", cfg.Run.StepDuration, "throughput duration per step")
	outCSV := fs.String("out-csv", cfg.Run.OutputCSV, "output csv path")
	nasTemplate := fs.String("nas-template", "", "path to NAS payload template")
	nasHex := fs.Bool("nas-hex", false, "template is hex text")
	recvTimeout := fs.Duration("recv-timeout", cfg.Run.RecvTimeout, "response timeout")
	latencyRateLimit := fs.Bool("latency-rate-limit", cfg.Run.LatencyRateLimit, "enforce --pps in latency mode")
	nodelay := fs.Bool("nodelay", cfg.Transport.NoDelay, "enable nodelay")
	heartbeatMS := fs.Int("heartbeat-ms", cfg.Transport.HeartbeatMS, "heartbeat interval ms")
	alpn := fs.String("alpn", cfg.Transport.ALPN, "reserved for QUIC")
	certFile := fs.String("cert-file", cfg.Transport.CertFile, "reserved for QUIC")
	keyFile := fs.String("key-file", cfg.Transport.KeyFile, "reserved for QUIC")
	caFile := fs.String("ca-file", cfg.Transport.CAFile, "reserved for QUIC")
	_ = fs.Parse(args)

	cfg.Transport.Type = transport.Type(*transportType)
	cfg.Transport.RemoteIP = *remoteIP
	cfg.Transport.RemotePort = *remotePort
	cfg.Transport.LocalIP = *localIP
	cfg.Transport.LocalPort = *localPort
	cfg.Transport.ChannelCount = *channels
	cfg.Transport.NoDelay = *nodelay
	cfg.Transport.HeartbeatMS = *heartbeatMS
	cfg.Transport.ALPN = *alpn
	cfg.Transport.CertFile = *certFile
	cfg.Transport.KeyFile = *keyFile
	cfg.Transport.CAFile = *caFile

	cfg.Run.Mode = *mode
	cfg.Run.Workers = *workers
	cfg.Run.PPS = *pps
	cfg.Run.Duration = *duration
	cfg.Run.StepCount = *stepCount
	cfg.Run.StepStartPPS = *stepStart
	cfg.Run.StepIncrement = *stepInc
	cfg.Run.StepDuration = *stepDuration
	cfg.Run.OutputCSV = *outCSV
	cfg.Run.NASPath = *nasTemplate
	cfg.Run.NASHex = *nasHex
	cfg.Run.RecvTimeout = *recvTimeout
	cfg.Run.LatencyRateLimit = *latencyRateLimit
	cfg.Run.ChannelCount = *channels

	if cfg.Transport.ConnectTimeout == 0 {
		cfg.Transport.ConnectTimeout = 5 * time.Second
	}

	client := app.GNBClient{TransportCfg: cfg.Transport, RunCfg: cfg.Run}
	return client.Run(ctx)
}

func usage() {
	fmt.Println(`mock5g - transport-pluggable gNB/AMF performance harness

Usage:
  mock5g amf [flags]
  mock5g gnb [flags]

Examples:
  mock5g amf --transport sctp --listen-ip 127.0.0.1 --listen-port 38412
  mock5g gnb --mode latency --transport sctp --remote-ip 127.0.0.1 --remote-port 38412 --duration 10s --out-csv summary.csv
  mock5g gnb --mode flood --nas-template ./nas.hex --nas-hex --pps 50000 --duration 30s`)
}

func configPathFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			return args[i+1]
		}
		if len(args[i]) > 9 && args[i][:9] == "--config=" {
			return args[i][9:]
		}
	}
	return ""
}
