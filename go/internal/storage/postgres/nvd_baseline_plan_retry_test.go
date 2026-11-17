// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// failingPlanWrite lets every statement through except the first failuresLeft
// plan upserts, which fail with a driver error carrying code, the way the server
// reports a lock cycle (40P01), a serialization failure (40001), or a missing
// table (42P01). It also counts the transactions the admission begins, so a test
// can tell a retry from a single attempt.
type failingPlanWrite struct {
	*orderedStatementRecorder
	failuresLeft int
	code         string
	begins       int
}

func (f *failingPlanWrite) Begin(context.Context) (db.Transaction, error) {
	f.begins++
	return f, nil
}

func (f *failingPlanWrite) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := f.orderedStatementRecorder.ExecContext(ctx, query, args...)
	if strings.Contains(query, "vulnerability_nvd_baseline_plans") && f.failuresLeft > 0 {
		f.failuresLeft--
		return nil, &pgconn.PgError{Code: f.code, Message: "injected plan write failure"}
	}
	return result, err
}

// TestWorkflowControlStorePlanWriteFailuresFollowTheRetryRules pins the retry
// boundary of the plan write (eshu-hq/eshu#10828). Two instances that share one
// baseline run meet only on its plan rows, so a lock cycle in the plan upsert has
// to reach the guarded admission's whole-transaction retry. That holds because
// the plan write wraps the driver error instead of flattening it into text. The
// retried attempt commits the work items with their plan rows, and the counter
// adds the plans once, for the attempt that committed. A failure the classifier
// does not retry (a missing table, which is what a binary older than migration 303
// meets) fails on the first attempt, and a cycle that never clears fails after
// workflowGuardedRunCreateMaxAttempts attempts. In both failing cases nothing is
// committed, nothing is counted, and the driver error is still visible to
// errors.As.
func TestWorkflowControlStorePlanWriteFailuresFollowTheRetryRules(t *testing.T) {
	t.Parallel()

	const attemptBudget = workflowGuardedRunCreateMaxAttempts
	cases := map[string]struct {
		failures   int
		code       string
		wantBegins int
		wantErr    bool
	}{
		"no failure is one attempt, no retry":           {wantBegins: 1},
		"one deadlock then a committed retry":           {failures: 1, code: "40P01", wantBegins: 2},
		"one serialization failure then a retry":        {failures: 1, code: "40001", wantBegins: 2},
		"deadlocks until the last attempt still commit": {failures: attemptBudget - 1, code: "40P01", wantBegins: attemptBudget},
		"a deadlock on every attempt gives up":          {failures: attemptBudget, code: "40P01", wantBegins: attemptBudget, wantErr: true},
		"an undefined table is not retried":             {failures: attemptBudget, code: "42P01", wantBegins: 1, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			run, items, plans := baselineAdmissionFixture(2026, 2025)
			attempt := queueFakeRows{rows: [][]any{{false}}}
			eligible := queueFakeRows{rows: [][]any{{0}, {1}}}
			inserted := queueFakeRows{rows: [][]any{{items[0].ScopeID}, {items[1].ScopeID}}}
			recorder := &orderedStatementRecorder{}
			for range attemptBudget {
				recorder.rows = append(recorder.rows, attempt, eligible, inserted)
				recorder.execRows = append(recorder.execRows, 1, 1, 2) // lock, run insert, plan upsert
			}
			fault := &failingPlanWrite{orderedStatementRecorder: recorder, failuresLeft: tc.failures, code: tc.code}

			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("plan-retry-test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			store := NewWorkflowControlStore(fault)
			store.Instruments = instruments

			admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil)

			if fault.begins != tc.wantBegins {
				t.Fatalf("transactions begun = %d, want %d", fault.begins, tc.wantBegins)
			}
			if got := len(recorder.argsOfStatements("INSERT INTO vulnerability_nvd_baseline_plans")); got != 3*tc.wantBegins {
				t.Fatalf("plan upsert argument count = %d, want one three-array upsert per attempt (%d attempts)", got, tc.wantBegins)
			}
			if got, want := recorder.didRollBack(), tc.failures > 0; got != want {
				t.Fatalf("rolled back = %v, want %v for %d failing upserts", got, want, tc.failures)
			}
			outcomes := planRowOutcomes(t, reader)
			if tc.wantErr {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
					t.Fatalf("error = %v, want the driver error with SQLSTATE %s still visible", err, tc.code)
				}
				if admission.InsertedWorkItems != 0 || admission.BaselinePlansRecorded != 0 || recorder.didCommit() {
					t.Fatalf("admission = %+v committed = %v, want a zero, uncommitted admission", admission, recorder.didCommit())
				}
				if len(outcomes) != 0 {
					t.Fatalf("outcomes = %v, want nothing counted when no attempt committed", outcomes)
				}
				return
			}
			if err != nil {
				t.Fatalf("admission error = %v, want the admission to commit", err)
			}
			if admission.InsertedWorkItems != 2 || admission.BaselinePlansRecorded != 2 || !recorder.didCommit() {
				t.Fatalf("admission = %+v committed = %v, want both work items and both plan rows committed", admission, recorder.didCommit())
			}
			if outcomes["recorded"] != 2 || len(outcomes) != 1 {
				t.Fatalf("outcomes = %v, want the committed attempt counted once as recorded=2", outcomes)
			}
		})
	}
}
