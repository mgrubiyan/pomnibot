package perftest

import (
	"fmt"
	"testing"
	"time"
)

func TestOperationThresholds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		latency time.Duration
		err     error
		pass    bool
	}{
		{"boundary", 500 * time.Millisecond, nil, true},
		{"slow operation", 501 * time.Millisecond, nil, false},
		{"unexpected failure", time.Millisecond, fmt.Errorf("timeout"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRecorder()
			for i := 0; i < 100; i++ {
				r.add("fast", time.Millisecond, nil)
			}
			r.add("rare", tc.latency, tc.err)
			m := r.summary(time.Second)
			if m.passes() != tc.pass {
				t.Fatalf("per-operation thresholds: %+v", m)
			}
			if m.Overall.Count != 101 || m.Overall.RPS != 101 {
				t.Fatalf("incorrect counts: %+v", m.Overall)
			}
		})
	}
}
