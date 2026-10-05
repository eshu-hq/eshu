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

// baselineFenceRow is one scripted delta-baseline fence row:
// target status, is_delta, baseline, active generation id, active commit.
type baselineFenceRow struct {
	status                            string
	isDelta                           bool
	baseline, activeGen, activeCommit sql.NullString
}

type baselineFenceRows struct {
	rows []baselineFenceRow
	i    int
}

func (r *baselineFenceRows) Next() bool   { r.i++; return r.i <= len(r.rows) }
func (r *baselineFenceRows) Err() error   { return nil }
func (r *baselineFenceRows) Close() error { return nil }
func (r *baselineFenceRows) Scan(dest ...any) error {
	row := r.rows[r.i-1]
	*dest[0].(*string) = row.status
	*dest[1].(*bool) = row.isDelta
	*dest[2].(*sql.NullString) = row.baseline
	*dest[3].(*sql.NullString) = row.activeGen
	*dest[4].(*sql.NullString) = row.activeCommit
	return nil
}

// markRows is the refusal mark's (work_rows, generation_rows) result.
type markRows struct {
	work, generation int64
	done             bool
}

func (r *markRows) Next() bool   { ok := !r.done; r.done = true; return ok }
func (r *markRows) Err() error   { return nil }
func (r *markRows) Close() error { return nil }
func (r *markRows) Scan(dest ...any) error {
	*dest[0].(*int64) = r.work
	*dest[1].(*int64) = r.generation
	return nil
}

func nullString(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }

// fenceAckDB records, for every query, how many statements the Ack
// transaction had executed before it, so the fence's position in Ack's
// statement order is asserted, not assumed.
type fenceAckDB struct {
	ackRefusalDB
	execsBeforeQuery []int
}

func (d *fenceAckDB) Begin(context.Context) (db.Transaction, error) {
	d.beginCalls++
	return fenceAckTx{parent: d}, nil
}

type fenceAckTx struct{ parent *fenceAckDB }

func (tx fenceAckTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.parent.ExecContext(ctx, query, args...)
}

func (tx fenceAckTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	tx.parent.execsBeforeQuery = append(tx.parent.execsBeforeQuery, len(tx.parent.execs))
	return tx.parent.QueryContext(ctx, query, args...)
}

func (tx fenceAckTx) Commit() error   { tx.parent.commits++; return nil }
func (tx fenceAckTx) Rollback() error { tx.parent.rollbacks++; return nil }

func deltaFenceWork() projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-7319"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-d"},
		AttemptCount: 3,
	}
}

func deltaFenceCounter(t *testing.T, rm metricdata.ResourceMetrics) int64 {
	t.Helper()
	return counterTotal(rm, "eshu_dp_projector_delta_baseline_fence_total")
}

func TestProjectorAckRefusesDeltaWhoseBaselineIsNotActive(t *testing.T) {
	t.Parallel()
	fake := &fenceAckDB{}
	fake.results = []sql.Result{
		projectorRowsAffectedResult{rowsAffected: 1}, // set_config
		projectorRowsAffectedResult{rowsAffected: 1}, // scope repoint
		projectorRowsAffectedResult{rowsAffected: 1}, // work ack
	}
	fake.queryRows = []db.Rows{
		&baselineFenceRows{rows: []baselineFenceRow{{
			status: "pending", isDelta: true,
			baseline: nullString("A"), activeGen: nullString("gen-b"), activeCommit: nullString("B"),
		}}},
		&markRows{work: 1, generation: 1},
	}
	queue := NewProjectorQueue(fake, "projector-1", time.Minute)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	err := queue.Ack(context.Background(), deltaFenceWork(), runtime.Result{})
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack() = %v, want ErrWorkSuperseded", err)
	}
	if got := supersededFailureClass(err); got != projector.DeltaBaselineMismatchAfterProjectionClass {
		t.Fatalf("failure class = %q, want %q", got, projector.DeltaBaselineMismatchAfterProjectionClass)
	}
	if fake.commits != 0 || fake.rollbacks == 0 {
		t.Fatalf("commits=%d rollbacks=%d, want the Ack rolled back", fake.commits, fake.rollbacks)
	}
	// The fence ran after the scope lock and the work update, and before any
	// statement that changes another generation.
	if len(fake.execs) != 3 || len(fake.execsBeforeQuery) == 0 || fake.execsBeforeQuery[0] != 3 {
		t.Fatalf("execs=%d execsBeforeFence=%v, want 3 and [3 ...]", len(fake.execs), fake.execsBeforeQuery)
	}
	if !strings.Contains(fake.execs[1].query, "UPDATE ingestion_scopes") ||
		!strings.Contains(fake.execs[2].query, "SET status = 'succeeded'") {
		t.Fatalf("fence did not follow the scope lock and work update:\n%s\n%s", fake.execs[1].query, fake.execs[2].query)
	}
	if got := fake.queries[0].query; got != deltaBaselineFenceQuery {
		t.Fatalf("first query is not the fence read:\n%s", got)
	}
	mark := fake.queries[1]
	if mark.query != markProjectorDeltaBaselineRefusedQuery {
		t.Fatalf("second query is not the refusal mark:\n%s", mark.query)
	}
	if mark.args[3] != "projector-1" || mark.args[4] != 3 ||
		mark.args[5] != projector.DeltaBaselineMismatchAfterProjectionClass ||
		mark.args[7] != "A" || mark.args[8] != "B" || mark.args[9] != "gen-b" || mark.args[10] != "ack" {
		t.Fatalf("mark args = %v", mark.args)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_projector_delta_baseline_fence_total",
		map[string]string{"phase": "ack", "outcome": "refused_active_differs"})
	if got := deltaFenceCounter(t, rm); got != 1 {
		t.Fatalf("fence count = %d, want 1", got)
	}
	if got := counterTotal(rm, "eshu_dp_superseded_generation_fence_total"); got != 0 {
		t.Fatalf("#7130 fence counter = %d, want 0: a baseline refusal must not reuse it", got)
	}
}

func TestProjectorAckCountsMatchedDeltaOnceAfterCommit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		row     baselineFenceRow
		outcome string
	}{
		{"matched", baselineFenceRow{
			status: "pending", isDelta: true, baseline: nullString("A"),
			activeGen: nullString("gen-a"), activeCommit: nullString("A"),
		}, "matched"},
		{"unfenced", baselineFenceRow{
			status: "pending", isDelta: true,
			activeGen: nullString("gen-a"), activeCommit: nullString("A"),
		}, "unfenced"},
		{"already_active", baselineFenceRow{
			status: "active", isDelta: true, baseline: nullString("A"),
			activeGen: nullString("gen-d"), activeCommit: nullString("D"),
		}, "already_active"},
		{"full", baselineFenceRow{status: "pending", activeGen: nullString("gen-a"), activeCommit: nullString("A")}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fenceAckDB{}
			fake.result = projectorRowsAffectedResult{rowsAffected: 1}
			fake.queryRows = []db.Rows{&baselineFenceRows{rows: []baselineFenceRow{tc.row}}}
			queue := NewProjectorQueue(fake, "projector-1", time.Minute)
			instruments, reader := newEnqueueInstruments(t)
			queue.Instruments = instruments
			if err := queue.Ack(context.Background(), deltaFenceWork(), runtime.Result{}); err != nil {
				t.Fatalf("Ack() = %v, want nil", err)
			}
			if fake.commits != 1 || len(fake.execs) != 7 {
				t.Fatalf("commits=%d execs=%d, want 1 and 7", fake.commits, len(fake.execs))
			}
			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("collect: %v", err)
			}
			if tc.outcome == "" {
				if got := deltaFenceCounter(t, rm); got != 0 {
					t.Fatalf("full generation counted %d times, want 0", got)
				}
				return
			}
			assertCounterPresentWithLabels(t, rm, "eshu_dp_projector_delta_baseline_fence_total",
				map[string]string{"phase": "ack", "outcome": tc.outcome})
			if got := deltaFenceCounter(t, rm); got != 1 {
				t.Fatalf("fence count = %d, want 1", got)
			}
		})
	}
}

func TestProjectorAckDeltaRefusalLostClaimIsClaimRejected(t *testing.T) {
	t.Parallel()
	fake := &fenceAckDB{}
	fake.result = projectorRowsAffectedResult{rowsAffected: 1}
	fake.queryRows = []db.Rows{
		&baselineFenceRows{rows: []baselineFenceRow{{status: "pending", isDelta: true, baseline: nullString("A")}}},
		&markRows{work: 0},
	}
	queue := NewProjectorQueue(fake, "projector-1", time.Minute)
	err := queue.Ack(context.Background(), deltaFenceWork(), runtime.Result{})
	if !errors.Is(err, ErrProjectorClaimRejected) || errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack() = %v, want ErrProjectorClaimRejected", err)
	}
}

func TestProjectorQueueReadDeltaBaseline(t *testing.T) {
	t.Parallel()
	fake := &recordingExecQueryer{queryRows: []db.Rows{&baselineFenceRows{rows: []baselineFenceRow{{
		status:  "pending",
		isDelta: true, baseline: nullString(" A "), activeGen: nullString("gen-b"), activeCommit: nullString("B"),
	}}}}}
	queue := NewProjectorQueue(fake, "projector-1", time.Minute)
	state, err := queue.ReadDeltaBaseline(context.Background(), deltaFenceWork())
	if err != nil {
		t.Fatalf("ReadDeltaBaseline() = %v", err)
	}
	want := projector.DeltaBaselineState{
		TargetFound: true, TargetStatus: scope.GenerationStatusPending,
		IsDelta: true, BaselineCommitSHA: "A", ActiveGenerationID: "gen-b", ActiveCommitSHA: "B",
	}
	if state != want {
		t.Fatalf("state = %+v, want %+v", state, want)
	}
	if got := fake.queries[0].args; got[0] != "scope-7319" || got[1] != "gen-d" {
		t.Fatalf("fence args = %v", got)
	}

	empty := NewProjectorQueue(&recordingExecQueryer{}, "projector-1", time.Minute)
	missing, err := empty.ReadDeltaBaseline(context.Background(), deltaFenceWork())
	if err != nil || missing.TargetFound {
		t.Fatalf("missing row = %+v, %v; want TargetFound=false", missing, err)
	}
}

func TestProjectorQueueRefuseDeltaBaselinePreflight(t *testing.T) {
	t.Parallel()
	fake := &recordingExecQueryer{queryRows: []db.Rows{&markRows{work: 1, generation: 1}}}
	queue := NewProjectorQueue(fake, "projector-1", time.Minute)
	err := queue.RefuseDeltaBaseline(context.Background(), deltaFenceWork(), projector.DeltaBaselineRefusal{
		Phase: projector.DeltaBaselinePhasePreflight, Outcome: projector.DeltaBaselineRefusedNoActive,
		State: projector.DeltaBaselineState{TargetFound: true, IsDelta: true, BaselineCommitSHA: "A"},
	})
	if !errors.Is(err, failure.ErrWorkSuperseded) || supersededFailureClass(err) != projector.DeltaBaselineMismatchClass {
		t.Fatalf("RefuseDeltaBaseline() = %v, want ErrWorkSuperseded with the preflight class", err)
	}
	args := fake.queries[0].args
	if args[5] != projector.DeltaBaselineMismatchClass || args[8] != "none" || args[10] != "preflight" {
		t.Fatalf("mark args = %v", args)
	}
	if fake.beginCalls != 0 {
		t.Fatalf("preflight refusal opened %d transactions, want 0", fake.beginCalls)
	}
}

// TestDeltaBaselineMarkStatementShape pins the refusal mark's safety
// predicates: claim fences on the work row, no demotion of an active
// generation, and no scope-row lock.
func TestDeltaBaselineMarkStatementShape(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"work.lease_owner = $4",
		"work.attempt_count = $5",
		"work.status IN ('claimed', 'running')",
		"generation.status IN ('pending', 'failed')",
		"SET status = 'superseded'",
	} {
		if !strings.Contains(markProjectorDeltaBaselineRefusedQuery, want) {
			t.Fatalf("mark statement missing %q:\n%s", want, markProjectorDeltaBaselineRefusedQuery)
		}
	}
	if strings.Contains(markProjectorDeltaBaselineRefusedQuery, "ingestion_scopes") {
		t.Fatalf("mark statement must not touch the scope row:\n%s", markProjectorDeltaBaselineRefusedQuery)
	}
	workAt := strings.Index(markProjectorDeltaBaselineRefusedQuery, "UPDATE fact_work_items")
	genAt := strings.Index(markProjectorDeltaBaselineRefusedQuery, "UPDATE scope_generations")
	if workAt < 0 || genAt < workAt {
		t.Fatalf("mark must update the work row before the generation row:\n%s", markProjectorDeltaBaselineRefusedQuery)
	}
}

// TestDeltaBaselineFenceSharesActivePredicate derives both reads from one
// active-generation predicate, so the collector's baseline read and the fence
// cannot drift apart.
func TestDeltaBaselineFenceSharesActivePredicate(t *testing.T) {
	t.Parallel()
	if !strings.Contains(lastProjectedCommitSHAQuery, "AND "+activeGenerationPredicate) {
		t.Fatalf("baseline read does not use the shared predicate:\n%s", lastProjectedCommitSHAQuery)
	}
	if !strings.Contains(deltaBaselineFenceQuery, "AND "+activeGenerationPredicate) {
		t.Fatalf("fence read does not use the shared predicate:\n%s", deltaBaselineFenceQuery)
	}
	for _, forbidden := range []string{"FOR UPDATE", "FOR NO KEY UPDATE", "FOR SHARE", "ingestion_scopes"} {
		if strings.Contains(deltaBaselineFenceQuery, forbidden) {
			t.Fatalf("fence read must take no lock and never touch the scope row (%q):\n%s", forbidden, deltaBaselineFenceQuery)
		}
	}
}
