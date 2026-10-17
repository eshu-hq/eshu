// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRetentionProbeRoundContextIsFreshAndBounded(t *testing.T) {
	first, cancelFirst := newRetentionProbeRoundContext()
	cancelFirst()
	second, cancelSecond := newRetentionProbeRoundContext()
	defer cancelSecond()
	if err := second.Err(); err != nil {
		t.Fatalf("next round inherited cancellation: %v", err)
	}
	deadline, ok := second.Deadline()
	remaining := time.Until(deadline)
	if !ok || remaining <= 0 || remaining > 3*time.Minute {
		t.Fatalf("next round deadline = %v, bounded 3m deadline required", deadline)
	}
	if first.Err() != context.Canceled {
		t.Fatalf("prior round cancellation = %v, want canceled", first.Err())
	}
	cancelSecond()
	if second.Err() != context.Canceled {
		t.Fatalf("current round cancellation = %v, want canceled", second.Err())
	}
}

func TestRetentionPhaseTraceRecordsOriginalBudgetAndFailure(t *testing.T) {
	ctx, cancel := newRetentionProbeRoundContext()
	defer cancel()
	var records []string
	trace := retentionPhaseTrace{
		ctx:          ctx,
		round:        2,
		roundStarted: time.Now(),
		logf: func(format string, args ...any) {
			records = append(records, fmt.Sprintf(format, args...))
		},
	}
	started := time.Now()
	trace.mark("seed_first", "start", started, "running")
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer trace.finish("seed_first", true)
		runtime.Goexit()
	}()
	<-done
	if len(records) != 2 {
		t.Fatalf("phase records = %d, want start and failed round end", len(records))
	}
	for _, record := range records {
		for _, field := range []string{
			"round=2", "utc=", "round_elapsed=", "original_ctx_deadline_utc=",
			"original_ctx_remaining=", "ctx_err=<nil>",
		} {
			if !strings.Contains(record, field) {
				t.Errorf("record %q missing %q", record, field)
			}
		}
	}
	if !strings.Contains(records[0], "phase=seed_first boundary=start outcome=running") {
		t.Errorf("start record = %q", records[0])
	}
	if !strings.Contains(records[1], "phase=round boundary=end outcome=failed_in_seed_first") {
		t.Errorf("failure record = %q", records[1])
	}
	if ctx.Err() != nil {
		t.Fatalf("tracing changed original context: %v", ctx.Err())
	}
}

func TestRetentionPhaseTraceUsesCapturedPruneEndTime(t *testing.T) {
	ctx, cancel := newRetentionProbeRoundContext()
	defer cancel()
	var record string
	trace := retentionPhaseTrace{
		ctx: ctx, round: 1, roundStarted: time.Now(),
		logf: func(format string, args ...any) { record = fmt.Sprintf(format, args...) },
	}
	captured := time.Now().UTC()
	trace.markAt("prune_first", "end", captured.Add(-time.Millisecond), "ok", captured)
	if !strings.Contains(record, "utc="+captured.Format(time.RFC3339Nano)) {
		t.Fatalf("record %q did not use captured prune end", record)
	}
}
