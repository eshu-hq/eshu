// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ackRefusalDB scripts Ack's rows-affected results and records how the Ack
// transaction ended, so the superseded-generation branch can be checked
// without a database. The live proof is projector_queue_ack_superseded_live_test.go.
type ackRefusalDB struct {
	recordingExecQueryer
	commits, rollbacks int
}

func (d *ackRefusalDB) Begin(context.Context) (db.Transaction, error) {
	d.beginCalls++
	return ackRefusalTx{parent: d}, nil
}

type ackRefusalTx struct{ parent *ackRefusalDB }

func (tx ackRefusalTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.parent.ExecContext(ctx, query, args...)
}

func (tx ackRefusalTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return tx.parent.QueryContext(ctx, query, args...)
}

func (tx ackRefusalTx) Commit() error   { tx.parent.commits++; return nil }
func (tx ackRefusalTx) Rollback() error { tx.parent.rollbacks++; return nil }

func ackRefusalResults(activated, marked int64) []sql.Result {
	// set_config, scope repoint, work ack, obsolete supersede, active
	// supersede, activate, then the activation obligation insert when
	// activated (the superseded mark when refused), then the producer
	// activation obligation insert when activated (#7635). Refused Acks
	// roll back before either obligation, so the last two entries feed
	// the activated path only.
	return []sql.Result{
		projectorRowsAffectedResult{rowsAffected: 1},
		projectorRowsAffectedResult{rowsAffected: 1},
		projectorRowsAffectedResult{rowsAffected: 1},
		projectorRowsAffectedResult{rowsAffected: 0},
		projectorRowsAffectedResult{rowsAffected: 1},
		projectorRowsAffectedResult{rowsAffected: activated},
		projectorRowsAffectedResult{rowsAffected: marked},
		projectorRowsAffectedResult{rowsAffected: 1},
	}
}

func ackRefusalWork() projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-1"},
		Generation:   scope.ScopeGeneration{GenerationID: "generation-old"},
		AttemptCount: 2,
	}
}

func TestProjectorAckSupersededGenerationRollsBackAndMarksWork(t *testing.T) {
	t.Parallel()

	fake := &ackRefusalDB{}
	fake.results = ackRefusalResults(0, 1)
	queue := NewProjectorQueue(fake, "projector-1", time.Minute)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	err := queue.Ack(context.Background(), ackRefusalWork(), runtime.Result{})
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack() error = %v, want ErrWorkSuperseded", err)
	}
	if got := supersededFailureClass(err); got != projectorAckGenerationSupersededClass {
		t.Fatalf("superseded failure class = %q, want %q (review F3)", got, projectorAckGenerationSupersededClass)
	}
	if fake.commits != 0 || fake.rollbacks == 0 {
		t.Fatalf("commits=%d rollbacks=%d, want the Ack transaction rolled back, never committed",
			fake.commits, fake.rollbacks)
	}
	if got, want := len(fake.execs), 7; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if activate := fake.execs[5].query; !strings.Contains(activate, "AND status <> 'superseded'") {
		t.Fatalf("activate statement lacks the superseded predicate:\n%s", activate)
	}
	mark := fake.execs[6]
	for _, want := range []string{
		"SET status = 'superseded'",
		"failure_class = '" + projectorAckGenerationSupersededClass + "'",
		"work.lease_owner = $4",
		"work.attempt_count = $5",
		"work.status IN ('claimed', 'running')",
		"generation.status = 'superseded'",
	} {
		if !strings.Contains(mark.query, want) {
			t.Fatalf("superseded mark statement missing %q:\n%s", want, mark.query)
		}
	}
	if mark.args[3] != "projector-1" || mark.args[4] != 2 {
		t.Fatalf("superseded mark args = %v, want lease owner and attempt fence", mark.args)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_superseded_generation_fence_total",
		map[string]string{"failure_class": projectorAckGenerationSupersededClass})
	if got := counterTotal(rm, "eshu_dp_superseded_generation_fence_total"); got != 1 {
		t.Fatalf("superseded generation fence count = %d, want 1", got)
	}
}

func TestProjectorAckSupersededGenerationLostClaimIsClaimRejected(t *testing.T) {
	t.Parallel()

	fake := &ackRefusalDB{}
	fake.results = ackRefusalResults(0, 0)
	queue := NewProjectorQueue(fake, "projector-1", time.Minute)

	err := queue.Ack(context.Background(), ackRefusalWork(), runtime.Result{})
	if !errors.Is(err, ErrProjectorClaimRejected) || errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack() error = %v, want only ErrProjectorClaimRejected when the mark finds no owned row", err)
	}
}

func TestProjectorAckActivatedGenerationCommits(t *testing.T) {
	t.Parallel()

	fake := &ackRefusalDB{}
	fake.results = ackRefusalResults(1, 1)
	queue := NewProjectorQueue(fake, "projector-1", time.Minute)

	if err := queue.Ack(context.Background(), ackRefusalWork(), runtime.Result{}); err != nil {
		t.Fatalf("Ack() error = %v, want nil", err)
	}
	if fake.commits != 1 || len(fake.execs) != 8 {
		t.Fatalf("commits=%d execs=%d, want one commit, no superseded mark, and both obligation inserts", fake.commits, len(fake.execs))
	}
	if got := fake.execs[6].query; !strings.Contains(got, "INSERT INTO activation_obligations") {
		t.Fatalf("exec[6] lacks the activation obligation insert:\n%s", got)
	}
	if got := fake.execs[7].query; !strings.Contains(got, "INSERT INTO producer_activation_obligations") {
		t.Fatalf("exec[7] lacks the producer activation obligation insert (#7635):\n%s", got)
	}
}
