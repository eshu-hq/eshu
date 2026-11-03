// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"testing"
	"time"
)

func TestRecorderKeepsSlowestCalls(t *testing.T) {
	r := newRecorder()
	for i := 1; i <= 30; i++ {
		r.observe("claim_issued", time.Duration(i)*time.Millisecond, nil)
	}
	r.observe("complete", 20*time.Second, nil)
	slow := r.slowCalls()
	if len(slow) != maxSlowCalls {
		t.Fatalf("kept %d slow calls, want %d", len(slow), maxSlowCalls)
	}
	if slow[0].Op != "complete" || slow[0].DurationMS != 20000 {
		t.Fatalf("slowest = %+v, want the 20 s complete first", slow[0])
	}
	if last := slow[len(slow)-1]; last.DurationMS != 22 {
		t.Fatalf("tenth slowest = %v ms, want 22", last.DurationMS)
	}
	if s := summarize([]time.Duration{3, 1, 2}); s.Count != 3 || s.MaxMS != ms(3) {
		t.Fatalf("summary = %+v", s)
	}
}
