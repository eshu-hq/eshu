// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// companionEntries is a small terraform_state result.
func companionEntries() []store.Entry {
	return []store.Entry{
		{Section: "serial", Ordinal: 1, JSON: `{"safe_locator_hash":"h","serial":3}`},
		{Section: "warning", Ordinal: 1, JSON: `{"safe_locator_hash":"h","warning_kind":"state_missing"}`},
	}
}

// withCompanion adds a terraform_state companion to the runner and returns its
// statement double.
func withCompanion(runner *Runner) *fakeStatement {
	companion := &fakeStatement{entries: companionEntries(), clock: nil}
	statement := companion.statement()
	statement.ModelKey = store.ModelTerraformState
	statement.SourceSHA256 = strings.Repeat("ef", 32)
	runner.Companions = []Statement{statement}
	return companion
}

// TestRunOnceWritesCompanionModelsInTheSameTransaction: a companion model is a
// second row with its own key, digest and guarded upsert, written in the one
// pass transaction under the same lock and the same database clock.
func TestRunOnceWritesCompanionModelsInTheSameTransaction(t *testing.T) {
	t.Parallel()
	runner, database, _, _ := newPassRunner(t)
	companion := withCompanion(runner)

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeOK || pass.Err != nil {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	want := []string{"read_committed", "set_jit_off", "try_lock", "clock", "upsert", "upsert", "commit"}
	if got := database.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("pass statements = %v, want %v: one lock and one clock for both models, one commit", got, want)
	}
	if database.begun != 1 || database.committed != 1 || database.rolledBack != 0 {
		t.Fatalf("transactions begun/committed/rolled back = %d/%d/%d, want 1/1/0", database.begun, database.committed, database.rolledBack)
	}
	for _, kind := range []string{"read_committed", "try_lock", "clock", "upsert", "commit"} {
		for _, id := range database.txOf[kind] {
			if id != 1 {
				t.Fatalf("%s ran on transaction %d, want the pass transaction 1", kind, id)
			}
		}
	}
	if len(database.upsertCalls) != 2 {
		t.Fatalf("upserts = %d, want 2", len(database.upsertCalls))
	}
	first, second := database.upsertCalls[0], database.upsertCalls[1]
	if first[0] != store.ModelActiveWorkSummary || second[0] != store.ModelTerraformState {
		t.Fatalf("upsert models = %v, %v, want the primary then the companion", first[0], second[0])
	}
	if first[2] != strings.Repeat("cd", 32) || second[2] != strings.Repeat("ef", 32) {
		t.Fatalf("each row stores its own statement digest, got %v and %v", first[2], second[2])
	}
	if !first[3].(time.Time).Equal(passClock) || !second[3].(time.Time).Equal(passClock) {
		t.Fatalf("as_of = %v and %v, want the one database clock %v for both rows", first[3], second[3], passClock)
	}
	if second[5] != 2 {
		t.Fatalf("companion row_count = %v, want 2", second[5])
	}
	if len(companion.asOfs) != 1 || !companion.asOfs[0].Equal(passClock) {
		t.Fatalf("companion compute asOf = %v, want the database clock", companion.asOfs)
	}
	if len(pass.Rows) != 2 || pass.Rows[0].ModelKey != store.ModelActiveWorkSummary || pass.Rows[1].ModelKey != store.ModelTerraformState {
		t.Fatalf("pass rows = %+v, want one result per model in order", pass.Rows)
	}
	if pass.Rows[1].RowCount != 2 || pass.Rows[1].Outcome != OutcomeOK {
		t.Fatalf("companion result = %+v, want ok with 2 entries", pass.Rows[1])
	}
}

// TestRunOnceGuardsEachCompanionRowOnItsOwn: the as_of guard decides per row.
func TestRunOnceGuardsEachCompanionRowOnItsOwn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		affected []int64
		want     string
		rows     [2]string
	}{
		{"both advance", []int64{1, 1}, OutcomeOK, [2]string{OutcomeOK, OutcomeOK}},
		{"only the companion advances", []int64{0, 1}, OutcomeOK, [2]string{OutcomeRejectedGuard, OutcomeOK}},
		{"only the primary advances", []int64{1, 0}, OutcomeOK, [2]string{OutcomeOK, OutcomeRejectedGuard}},
		{"both rejected", []int64{0, 0}, OutcomeRejectedGuard, [2]string{OutcomeRejectedGuard, OutcomeRejectedGuard}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, database, _, _ := newPassRunner(t)
			withCompanion(runner)
			database.upsertAffectedSeq = tc.affected

			pass := runner.RunOnce(context.Background())

			if pass.Outcome != tc.want {
				t.Fatalf("pass outcome = %s, want %s", pass.Outcome, tc.want)
			}
			if len(pass.Rows) != 2 || pass.Rows[0].Outcome != tc.rows[0] || pass.Rows[1].Outcome != tc.rows[1] {
				t.Fatalf("row outcomes = %+v, want %v", pass.Rows, tc.rows)
			}
			if database.committed != 1 {
				t.Fatalf("committed = %d, want 1: a rejected row is not an error", database.committed)
			}
		})
	}
}

// TestRunOnceRollsBackBothRowsWhenACompanionFails: the pass is one
// transaction, so a failed companion leaves no half-written pair.
func TestRunOnceRollsBackBothRowsWhenACompanionFails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		fail func(primary, companion *fakeStatement, database *fakeDatabase)
	}{
		{"companion compute fails", func(_, c *fakeStatement, _ *fakeDatabase) { c.err = errors.New("tfstate boom") }},
		{"companion upsert fails", func(_, _ *fakeStatement, d *fakeDatabase) { d.upsertErr = errors.New("upsert boom") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, database, primary, _ := newPassRunner(t)
			companion := withCompanion(runner)
			tc.fail(primary, companion, database)

			pass := runner.RunOnce(context.Background())

			if pass.Outcome != OutcomeError || pass.Err == nil {
				t.Fatalf("RunOnce() = %+v, want an error outcome", pass)
			}
			if database.committed != 0 || database.rolledBack != 1 {
				t.Fatalf("committed/rolled back = %d/%d, want 0/1: never a committed half pair", database.committed, database.rolledBack)
			}
			if got := database.snapshot(); got[len(got)-1] != "rollback" {
				t.Fatalf("pass statements = %v, want to end in rollback", got)
			}
		})
	}
}

// TestRunOnceComputesEveryModelBeforeItWritesAny keeps the row write at the
// end of the transaction, so a slow companion statement does not hold the
// primary row's lock for its duration.
func TestRunOnceComputesEveryModelBeforeItWritesAny(t *testing.T) {
	t.Parallel()
	runner, database, primary, _ := newPassRunner(t)
	companion := withCompanion(runner)
	companion.err = errors.New("tfstate boom")

	runner.RunOnce(context.Background())

	if primary.callCount() != 1 || companion.callCount() != 1 {
		t.Fatalf("computes = %d and %d, want 1 each", primary.callCount(), companion.callCount())
	}
	for _, kind := range database.snapshot() {
		if kind == "upsert" {
			t.Fatal("an upsert ran before every model was computed")
		}
	}
}

func TestRunnerRejectsAnInvalidCompanion(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Statement){
		"blank key":     func(s *Statement) { s.ModelKey = " " },
		"blank digest":  func(s *Statement) { s.SourceSHA256 = "" },
		"no compute":    func(s *Statement) { s.Compute = nil },
		"duplicate key": func(s *Statement) { s.ModelKey = store.ModelActiveWorkSummary },
	} {
		runner, _, _, _ := newPassRunner(t)
		withCompanion(runner)
		mutate(&runner.Companions[0])
		if err := runner.Run(context.Background()); err == nil {
			t.Fatalf("%s: Run() error = nil, want a configuration error", name)
		}
	}
}

// TestRunOnceRecordsEachModelOnItsOwn: the pass counter carries each model's
// own outcome, the per-model compute histogram its compute time, and a pass
// that never reached the rows counts for every model.
func TestRunOnceRecordsEachModelOnItsOwn(t *testing.T) {
	t.Parallel()
	instruments, reader := newTestInstruments(t)
	runner, database, primary, _ := newPassRunner(t)
	runner.Instruments = instruments
	companion := withCompanion(runner)
	companion.clock, companion.cost = primary.clock, 700*time.Millisecond
	database.upsertAffectedSeq = []int64{1, 0}

	runner.RunOnce(context.Background())

	ok := counterValue(t, reader, "eshu_dp_status_summary_writer_passes_total",
		telemetry.AttrModelKey(store.ModelActiveWorkSummary), telemetry.AttrOutcome(OutcomeOK))
	rejected := counterValue(t, reader, "eshu_dp_status_summary_writer_passes_total",
		telemetry.AttrModelKey(store.ModelTerraformState), telemetry.AttrOutcome(OutcomeRejectedGuard))
	if ok != 1 || rejected != 1 {
		t.Fatalf("passes_total ok(active_work)/rejected_guard(terraform_state) = %d/%d, want 1/1", ok, rejected)
	}
	if n := histogramCount(t, reader, "eshu_dp_status_summary_writer_model_compute_seconds", telemetry.AttrModelKey(store.ModelTerraformState)); n != 1 {
		t.Fatalf("terraform_state compute samples = %d, want 1", n)
	}
	if n := histogramCount(t, reader, "eshu_dp_status_summary_writer_model_compute_seconds", telemetry.AttrModelKey(store.ModelActiveWorkSummary)); n != 1 {
		t.Fatalf("active_work_summary compute samples = %d, want 1", n)
	}

	database.lockAcquired = false
	runner.RunOnce(context.Background())
	for _, model := range []string{store.ModelActiveWorkSummary, store.ModelTerraformState} {
		if got := counterValue(t, reader, "eshu_dp_status_summary_writer_passes_total",
			attribute.String("model_key", model), telemetry.AttrOutcome(OutcomeSkippedLock)); got != 1 {
			t.Fatalf("skipped_lock for %s = %d, want 1: a skipped pass skips every model", model, got)
		}
	}
}

// TestRunOnceStoresEachModelsComputeTime: pass_duration_ms is the model's own
// compute time, not the pass's.
func TestRunOnceStoresEachModelsComputeTime(t *testing.T) {
	t.Parallel()
	runner, database, primary, _ := newPassRunner(t)
	companion := withCompanion(runner)
	companion.clock, companion.cost = primary.clock, 700*time.Millisecond

	runner.RunOnce(context.Background())

	if database.upsertCalls[0][4] != 300.0 || database.upsertCalls[1][4] != 700.0 {
		t.Fatalf("stored pass_duration_ms = %v and %v, want each model's own 300 and 700", database.upsertCalls[0][4], database.upsertCalls[1][4])
	}
}
