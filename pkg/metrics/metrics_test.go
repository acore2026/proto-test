package metrics

import (
	"testing"
	"time"
)

func TestSnapshotPercentiles(t *testing.T) {
	c := &Collector{}
	c.AddTx(10)
	c.AddRx(8)
	c.AddDropTimeout(2)
	c.AddLatency(100 * time.Microsecond)
	c.AddLatency(200 * time.Microsecond)
	c.AddLatency(300 * time.Microsecond)

	s := c.Snapshot("latency", "sctp", 1000, 1, time.Second)
	if s.P50US == 0 || s.P95US == 0 || s.P99US == 0 {
		t.Fatalf("unexpected percentiles: %+v", s)
	}
	if s.Drop != 2 || s.DropTimeout != 2 {
		t.Fatalf("expected timeout drop accounting, got: %+v", s)
	}
}
