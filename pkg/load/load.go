package load

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"mock5g/pkg/config"
	"mock5g/pkg/metrics"
	"mock5g/pkg/transport"
	"mock5g/pkg/transport/backend"
	"mock5g/pkg/wire"
)

type Runner struct {
	TransportCfg transport.Config
	RunCfg       config.RunConfig
}

func (r Runner) RunLatency(ctx context.Context, payload []byte) ([]metrics.Snapshot, error) {
	collector := &metrics.Collector{}
	start := time.Now()

	workers := r.RunCfg.Workers
	if workers <= 0 {
		workers = 1
	}

	ctx, cancel := context.WithTimeout(ctx, r.RunCfg.Duration)
	defer cancel()

	var seq atomic.Uint64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			sess, ch, err := r.open(ctx, uint16(workerID%max(1, r.TransportCfg.ChannelCount)))
			if err != nil {
				return
			}
			defer sess.Close()
			var ticker *time.Ticker
			if r.RunCfg.LatencyRateLimit && r.RunCfg.PPS > 0 {
				ppsPerWorker := max(1, r.RunCfg.PPS/workers)
				interval := time.Second / time.Duration(ppsPerWorker)
				if interval <= 0 {
					interval = time.Microsecond
				}
				ticker = time.NewTicker(interval)
				defer ticker.Stop()
			}

			for {
				if ticker != nil {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
					}
				} else {
					select {
					case <-ctx.Done():
						return
					default:
					}
				}
				next := seq.Add(1)
				frm := wire.Frame{MsgType: wire.MsgReq, SeqID: next, SendTSNS: time.Now().UnixNano(), ChannelID: ch.ID(), Payload: payload}
				buf, _ := frm.Encode()
				if err := ch.Send(ctx, buf); err != nil {
					collector.AddDropSendErr(1)
					continue
				}
				collector.AddTx(1)

				rctx, rcancel := context.WithTimeout(ctx, r.recvTimeout())
				raw, err := ch.Recv(rctx)
				rcancel()
				if err != nil {
					if isTimeoutError(err) {
						collector.AddDropTimeout(1)
					} else {
						collector.AddDrop(1)
					}
					continue
				}
				resp, err := wire.Decode(raw)
				if err != nil {
					collector.AddDropDecode(1)
					continue
				}
				if resp.MsgType != wire.MsgResp || resp.SeqID != next {
					collector.AddDropMismatch(1)
					continue
				}
				collector.AddRx(1)
				collector.AddLatency(time.Since(time.Unix(0, resp.SendTSNS)))
			}
		}(i)
	}

	intervals := r.collectSnapshots(ctx, collector, "latency", r.RunCfg.PPS, workers)
	wg.Wait()
	final := collector.Snapshot("latency", string(r.TransportCfg.Type), r.RunCfg.PPS, workers, time.Since(start))
	intervals = append(intervals, final)
	return intervals, nil
}

func (r Runner) RunFlood(ctx context.Context, payload []byte) ([]metrics.Snapshot, error) {
	collector := &metrics.Collector{}
	workers := r.RunCfg.Workers
	if workers <= 0 {
		workers = 1
	}

	ctx, cancel := context.WithTimeout(ctx, r.RunCfg.Duration)
	defer cancel()

	ppsPerWorker := max(1, r.RunCfg.PPS/workers)
	interval := time.Second / time.Duration(ppsPerWorker)
	if interval <= 0 {
		interval = time.Microsecond
	}

	var seq atomic.Uint64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			sess, ch, err := r.open(ctx, uint16(workerID%max(1, r.TransportCfg.ChannelCount)))
			if err != nil {
				return
			}
			defer sess.Close()

			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					id := seq.Add(1)
					frm := wire.Frame{MsgType: wire.MsgFlood, SeqID: id, SendTSNS: time.Now().UnixNano(), ChannelID: ch.ID(), Payload: payload}
					buf, _ := frm.Encode()
					if err := ch.Send(ctx, buf); err != nil {
						collector.AddDropSendErr(1)
						continue
					}
					collector.AddTx(1)
				}
			}
		}(i)
	}

	intervals := r.collectSnapshots(ctx, collector, "flood", r.RunCfg.PPS, workers)
	wg.Wait()
	intervals = append(intervals, collector.Snapshot("flood", string(r.TransportCfg.Type), r.RunCfg.PPS, workers, r.RunCfg.Duration))
	return intervals, nil
}

func (r Runner) RunThroughputSweep(ctx context.Context, payload []byte) ([]metrics.Snapshot, error) {
	steps := r.RunCfg.StepCount
	if steps <= 0 {
		steps = 1
	}
	startPPS := r.RunCfg.StepStartPPS
	if startPPS <= 0 {
		startPPS = max(1, r.RunCfg.BasePPS)
	}
	inc := r.RunCfg.StepIncrement
	if inc <= 0 {
		inc = max(1, r.RunCfg.StepPPS)
	}
	if r.RunCfg.StepDuration <= 0 {
		return nil, errors.New("step_duration must be > 0")
	}

	all := make([]metrics.Snapshot, 0, steps)
	for i := 0; i < steps; i++ {
		pps := startPPS + i*inc
		r2 := r
		r2.RunCfg.PPS = pps
		r2.RunCfg.Duration = r.RunCfg.StepDuration
		rows, err := r2.RunLatency(ctx, payload)
		if err != nil {
			return nil, fmt.Errorf("step %d pps=%d: %w", i+1, pps, err)
		}
		if len(rows) > 0 {
			all = append(all, rows[len(rows)-1])
		}
	}
	return all, nil
}

func (r Runner) open(ctx context.Context, channelID uint16) (transport.Session, transport.Channel, error) {
	sess, err := backend.Dial(ctx, r.TransportCfg)
	if err != nil {
		return nil, nil, err
	}
	ch, err := sess.OpenChannel(ctx, channelID)
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	return sess, ch, nil
}

func (r Runner) collectSnapshots(ctx context.Context, collector *metrics.Collector, mode string, pps, channels int) []metrics.Snapshot {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	rows := make([]metrics.Snapshot, 0, 16)
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return rows
		case <-ticker.C:
			s := collector.Snapshot(mode, string(r.TransportCfg.Type), pps, channels, time.Since(start))
			metrics.PrintSummary(s)
			rows = append(rows, s)
		}
	}
}

func (r Runner) recvTimeout() time.Duration {
	if r.RunCfg.RecvTimeout > 0 {
		return r.RunCfg.RecvTimeout
	}
	return time.Second
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
