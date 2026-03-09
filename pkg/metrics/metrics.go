package metrics

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

type Snapshot struct {
	Timestamp   time.Time
	Mode        string
	Transport   string
	TargetPPS   int
	AchievedPPS int
	Tx          uint64
	Rx          uint64
	Drop        uint64
	P50US       int64
	P95US       int64
	P99US       int64
	Channels    int
}

type Collector struct {
	mu        sync.Mutex
	tx        uint64
	rx        uint64
	drop      uint64
	latencyUS []int64
}

func (c *Collector) AddTx(n uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tx += n
}

func (c *Collector) AddRx(n uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rx += n
}

func (c *Collector) AddDrop(n uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drop += n
}

func (c *Collector) AddLatency(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latencyUS = append(c.latencyUS, d.Microseconds())
}

func (c *Collector) Snapshot(mode, transport string, targetPPS, channels int, since time.Duration) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	p50, p95, p99 := percentiles(c.latencyUS)
	achieved := 0
	if since > 0 {
		achieved = int(float64(c.rx) / since.Seconds())
	}
	drop := c.drop
	if c.tx > c.rx {
		drop += c.tx - c.rx
	}
	return Snapshot{
		Timestamp:   time.Now(),
		Mode:        mode,
		Transport:   transport,
		TargetPPS:   targetPPS,
		AchievedPPS: achieved,
		Tx:          c.tx,
		Rx:          c.rx,
		Drop:        drop,
		P50US:       p50,
		P95US:       p95,
		P99US:       p99,
		Channels:    channels,
	}
}

func percentiles(vals []int64) (int64, int64, int64) {
	if len(vals) == 0 {
		return 0, 0, 0
	}
	cp := append([]int64(nil), vals...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := func(p float64) int {
		i := int(float64(len(cp)-1) * p)
		if i < 0 {
			i = 0
		}
		if i >= len(cp) {
			i = len(cp) - 1
		}
		return i
	}
	return cp[idx(0.50)], cp[idx(0.95)], cp[idx(0.99)]
}

func WriteCSV(path string, rows []Snapshot) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	head := []string{"timestamp", "mode", "transport", "target_pps", "achieved_pps", "tx", "rx", "drop", "p50_us", "p95_us", "p99_us", "channels"}
	if err := w.Write(head); err != nil {
		return err
	}
	for _, s := range rows {
		rec := []string{
			s.Timestamp.Format(time.RFC3339Nano),
			s.Mode,
			s.Transport,
			strconv.Itoa(s.TargetPPS),
			strconv.Itoa(s.AchievedPPS),
			strconv.FormatUint(s.Tx, 10),
			strconv.FormatUint(s.Rx, 10),
			strconv.FormatUint(s.Drop, 10),
			strconv.FormatInt(s.P50US, 10),
			strconv.FormatInt(s.P95US, 10),
			strconv.FormatInt(s.P99US, 10),
			strconv.Itoa(s.Channels),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return nil
}

func PrintSummary(s Snapshot) {
	fmt.Printf("[%s] mode=%s transport=%s target_pps=%d achieved_pps=%d tx=%d rx=%d drop=%d p50=%dus p95=%dus p99=%dus channels=%d\n",
		s.Timestamp.Format(time.RFC3339), s.Mode, s.Transport, s.TargetPPS, s.AchievedPPS, s.Tx, s.Rx, s.Drop, s.P50US, s.P95US, s.P99US, s.Channels)
}
