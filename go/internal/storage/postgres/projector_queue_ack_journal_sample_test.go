// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeClock is a settable clock for a logSampler.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// gateSkipLines decodes the Ack gate-skip log lines in logs.
func gateSkipLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if msg, _ := entry["msg"].(string); strings.HasPrefix(msg, "changed-since ack journal skipped") {
			lines = append(lines, entry)
		}
	}
	return lines
}

// TestProjectorAckGateSkipLogIsSampled is the PR #8210 review fix: the
// counter eshu_dp_changed_since_ack_journal_total{outcome=gate_skipped}
// counts every gate skip, while the INFO line is a sampled audit line. The
// first skip logs, the rest of the interval is suppressed, and the first skip
// of the next interval logs again with the suppressed count.
func TestProjectorAckGateSkipLogIsSampled(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	clock := &fakeClock{t: time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)}
	sampler := newLogSampler(ackSkipLogInterval, clock.now)
	ack := func() {
		t.Helper()
		q := ackJournalQueue(&ackStatementDB{journalRow: []any{int64(1), int64(0), false}}, true)
		q.Instruments = instruments
		q.ackSkipLog = sampler
		if err := q.Ack(context.Background(), ackWorkForLockTimeoutTest(), runtime.Result{}); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	}

	const burst = 5
	for range burst {
		ack()
		clock.advance(time.Second)
	}
	if got := ajJournalCounts(t, reader)[ackJournalOutcomeGateSkipped]; got != burst {
		t.Fatalf("gate_skipped count = %d, want every skip counted (%d)", got, burst)
	}
	lines := gateSkipLines(t, &logs)
	if len(lines) != 1 {
		t.Fatalf("gate-skip log lines in one interval = %d, want 1:\n%s", len(lines), logs.String())
	}
	if lines[0]["scope_id"] != "scope-1" || lines[0]["generation_id"] != "generation-1" ||
		lines[0]["outcome"] != ackJournalOutcomeGateSkipped || lines[0]["gate_skipped_since_last_log"] != float64(0) {
		t.Fatalf("first gate-skip line = %v, want scope, generation, outcome and 0 suppressed", lines[0])
	}

	clock.advance(ackSkipLogInterval)
	ack()
	if got := ajJournalCounts(t, reader)[ackJournalOutcomeGateSkipped]; got != burst+1 {
		t.Fatalf("gate_skipped count = %d, want %d", got, burst+1)
	}
	lines = gateSkipLines(t, &logs)
	if len(lines) != 2 {
		t.Fatalf("gate-skip log lines after the interval = %d, want 2:\n%s", len(lines), logs.String())
	}
	if lines[1]["gate_skipped_since_last_log"] != float64(burst-1) {
		t.Fatalf("second gate-skip line = %v, want gate_skipped_since_last_log %d", lines[1], burst-1)
	}
}

// TestLogSamplerAdmitsOnePerIntervalUnderConcurrency proves the sampler is
// safe for the many goroutines that Ack on: concurrent events inside one
// interval admit exactly one line, and the next admitted line carries every
// other event as suppressed. Run with -race.
func TestLogSamplerAdmitsOnePerIntervalUnderConcurrency(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{t: time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)}
	sampler := newLogSampler(time.Minute, clock.now)
	const workers = 64
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := sampler.admit(); ok {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if admitted != 1 {
		t.Fatalf("admitted %d of %d concurrent events in one interval, want 1", admitted, workers)
	}
	clock.advance(time.Minute)
	ok, suppressed := sampler.admit()
	if !ok || suppressed != workers-1 {
		t.Fatalf("next interval admit = (%t, %d), want (true, %d)", ok, suppressed, workers-1)
	}
}
