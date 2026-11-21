// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// postCommitWakeFakeDB answers the exact wake statement. When block is set it
// waits for the statement context to end and returns its error, standing in
// for a wake stuck behind another transaction's row locks. It records the
// context state the statement saw.
type postCommitWakeFakeDB struct {
	block    bool
	reopened int64

	mu          sync.Mutex
	sawDeadline bool
	sawErr      error
	statements  int
}

func (d *postCommitWakeFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return sharedIntentResult{}, nil
}

func (d *postCommitWakeFakeDB) QueryContext(ctx context.Context, _ string, _ ...any) (db.Rows, error) {
	d.mu.Lock()
	_, d.sawDeadline = ctx.Deadline()
	d.sawErr = ctx.Err()
	d.statements++
	d.mu.Unlock()
	if d.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &waitingWakeCountRows{reopened: d.reopened, cleared: d.reopened}, nil
}

// enrolledBackwardEvidenceRow is one fan-in readiness row of the enrolled
// phase.
func enrolledBackwardEvidenceRow() []gpphase.PhaseState {
	return []gpphase.PhaseState{ownGenerationPhaseRow("scope-pc", "gen-pc",
		gpphase.KeyspaceCrossRepoEvidence, gpphase.PhaseBackwardEvidenceCommitted)}
}

// TestPostCommitExactPhaseWakeTimesOutAndMeters pins the #11104 bound: a
// post-commit re-wake stuck behind locks ends at its timeout, returns
// nothing to the committed caller, and counts outcome=error on the
// post-commit wake site.
func TestPostCommitExactPhaseWakeTimesOutAndMeters(t *testing.T) {
	t.Parallel()

	fake := &postCommitWakeFakeDB{block: true}
	instruments, reader := newEnqueueInstruments(t)
	start := time.Now()
	wakeExactPhaseWaitsAfterCommitWithin(context.Background(), fake, instruments,
		enrolledBackwardEvidenceRow(), 50*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("post-commit wake took %v, want it bounded by its timeout", elapsed)
	}
	if !fake.sawDeadline {
		t.Fatal("post-commit wake statement ran without a deadline")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_reducer_waiting_wake_total",
		map[string]string{"wake_site": "phase_exact_post_commit", "outcome": "error"})
	if PostCommitExactPhaseWakeTimeout <= 0 || PostCommitExactPhaseWakeTimeout >= DefaultWaitingAuditInterval {
		t.Fatalf("PostCommitExactPhaseWakeTimeout = %v, want positive and below the %v audit interval",
			PostCommitExactPhaseWakeTimeout, DefaultWaitingAuditInterval)
	}
}

// TestPostCommitExactPhaseWakeSurvivesCallerCancel pins the detachment: a
// partition that committed still re-wakes when the pass context was
// canceled (a sibling partition failed), under its own deadline.
func TestPostCommitExactPhaseWakeSurvivesCallerCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &postCommitWakeFakeDB{reopened: 1}
	instruments, reader := newEnqueueInstruments(t)
	WakeExactPhaseWaitsAfterCommit(ctx, fake, instruments, enrolledBackwardEvidenceRow())
	if fake.statements != 1 || fake.sawErr != nil || !fake.sawDeadline {
		t.Fatalf("statements=%d ctxErr=%v deadline=%v, want one live, bounded statement",
			fake.statements, fake.sawErr, fake.sawDeadline)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_reducer_waiting_wake_total",
		map[string]string{"wake_site": "phase_exact_post_commit", "outcome": "reopened"})
}

// TestExactPhaseWakeLogsInfoOnlyWhenItReopens pins the log level: a wake that
// reopened a waiter logs INFO, a no-op wake logs DEBUG. Not parallel: it
// swaps the default logger, and parallel tests resume only after the
// sequential ones finish.
func TestExactPhaseWakeLogsInfoOnlyWhenItReopens(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, reopened := range []int64{0, 1} {
		fake := &postCommitWakeFakeDB{reopened: reopened}
		if err := wakeExactPhaseWaits(context.Background(), fake, nil, "phase_exact_in_tx",
			enrolledBackwardEvidenceRow(), time.Now()); err != nil {
			t.Fatalf("wakeExactPhaseWaits() error = %v", err)
		}
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, "reducer exact phase wake") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("wake lines = %q, want 2", lines)
	}
	if !strings.Contains(lines[0], "level=DEBUG") || !strings.Contains(lines[0], "reopened=0") {
		t.Fatalf("no-op wake line = %q, want DEBUG reopened=0", lines[0])
	}
	if !strings.Contains(lines[1], "level=INFO") || !strings.Contains(lines[1], "reopened=1") {
		t.Fatalf("reopening wake line = %q, want INFO reopened=1", lines[1])
	}
}
