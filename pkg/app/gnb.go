package app

import (
	"context"
	"fmt"

	"mock5g/pkg/config"
	"mock5g/pkg/load"
	"mock5g/pkg/metrics"
	"mock5g/pkg/nas"
	"mock5g/pkg/transport"
)

type GNBClient struct {
	TransportCfg transport.Config
	RunCfg       config.RunConfig
}

func (g GNBClient) Run(ctx context.Context) error {
	payload := []byte("ping")
	if g.RunCfg.NASPath != "" {
		p, err := nas.LoadTemplate(g.RunCfg.NASPath, g.RunCfg.NASHex)
		if err != nil {
			return err
		}
		payload = p
	}

	r := load.Runner{TransportCfg: g.TransportCfg, RunCfg: g.RunCfg}

	var rows []metrics.Snapshot
	var err error
	switch g.RunCfg.Mode {
	case "latency":
		rows, err = r.RunLatency(ctx, payload)
	case "throughput":
		rows, err = r.RunThroughputSweep(ctx, payload)
	case "flood":
		rows, err = r.RunFlood(ctx, payload)
	default:
		return fmt.Errorf("unknown mode %q", g.RunCfg.Mode)
	}
	if err != nil {
		return err
	}

	if g.RunCfg.OutputCSV != "" {
		if err := metrics.WriteCSV(g.RunCfg.OutputCSV, rows); err != nil {
			return err
		}
		fmt.Printf("wrote csv: %s\n", g.RunCfg.OutputCSV)
	}
	if len(rows) > 0 {
		fmt.Println("final summary:")
		metrics.PrintSummary(rows[len(rows)-1])
	}
	return nil
}
