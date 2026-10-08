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

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// companionEntries is a small terraform_state result.
func companionEntries() []store.Entry {
	return []store.Entry{
		{Section: "last_serial", Ordinal: 1, JSON: `{"safe_locator_hash":"h","serial":3}`},
		{Section: "recent_warning", Ordinal: 1, JSON: `{"safe_locator_hash":"h","warning_kind":"state_missing"}`},
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

// oneModelSequence is the statements of one model's transaction.
var oneModelSequence = []string{"read_committed", "set_jit_off", "try_lock", "clock", "upsert", "commit"}

// TestRunOnceWritesEachModelInItsOwnTransaction: the first model is committed
// before the companion starts. Each model's lock, clock, upsert and commit
// share one transaction, and each model reads its own database clock.
func TestRunOnceWritesEachModelInItsOwnTransaction(t *testing.T) {
	t.Parallel()
	runner, database, _, _ := newPassRunner(t)
	companion := withCompanion(runner)

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeOK || pass.Err != nil {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	want := append(append([]string(nil), oneModelSequence...), oneModelSequence...)
	if got := database.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("pass statements = %v, want two complete transactions %v", got, want)
	}
	if database.begun != 2 || database.committed != 2 || database.rolledBack != 0 {
		t.Fatalf("transactions begun/committed/rolled back = %d/%d/%d, want 2/2/0", database.begun, database.committed, database.rolledBack)
	}
	for _, kind := range oneModelSequence {
		if got := database.txOf[kind]; !reflect.DeepEqual(got, []int{1, 2}) {
			t.Fatalf("%s ran on transactions %v, want one on each of 1 and 2", kind, got)
		}
	}
	if len(database.upsertCalls) != 2 {
		t.Fatalf("upserts = %d, want 2", len(database.upsertCalls))
	}
	first, second := database.upsertCalls[0], database.upsertCalls[1]
	if first[0] != store.ModelActiveWorkSummary || second[0] != store.ModelTerraformState {
		t.Fatalf("upsert models = %v, %v, want the first model then the companion", first[0], second[0])
	}
	if first[2] != strings.Repeat("cd", 32) || second[2] != strings.Repeat("ef", 32) {
		t.Fatalf("each row stores its own statement digest, got %v and %v", first[2], second[2])
	}
	if !second[3].(time.Time).Equal(passClock) || len(companion.asOfs) != 1 || !companion.asOfs[0].Equal(passClock) {
		t.Fatalf("companion as_of/compute asOf = %v/%v, want the database clock %v", second[3], companion.asOfs, passClock)
	}
	if second[5] != 2 {
		t.Fatalf("companion row_count = %v, want 2", second[5])
	}
	if len(pass.Models) != 2 || pass.Models[0].ModelKey != store.ModelActiveWorkSummary || pass.Models[1].ModelKey != store.ModelTerraformState {
		t.Fatalf("pass models = %+v, want one result per model in order", pass.Models)
	}
	if pass.Models[1].RowCount != 2 || pass.Models[1].Outcome != OutcomeOK {
		t.Fatalf("companion result = %+v, want ok with 2 entries", pass.Models[1])
	}
}

// TestRunOnceWithoutCompanionsKeepsTheSingleModelSequence pins the original
// statement sequence so adding companions cannot change a one-model runner.
func TestRunOnceWithoutCompanionsKeepsTheSingleModelSequence(t *testing.T) {
	t.Parallel()
	runner, database, _, _ := newPassRunner(t)

	pass := runner.RunOnce(context.Background())

	if got := database.snapshot(); !reflect.DeepEqual(got, oneModelSequence) {
		t.Fatalf("pass statements = %v, want %v", got, oneModelSequence)
	}
	if len(pass.Models) != 1 || pass.Models[0].Duration != pass.Duration {
		t.Fatalf("pass models = %+v for duration %s, want the one model's transaction to be the pass", pass.Models, pass.Duration)
	}
}

// TestRunOnceGuardsEachModelRowOnItsOwn: the as_of guard decides per row, and
// each row's outcome is reported on its own.
func TestRunOnceGuardsEachModelRowOnItsOwn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		affected []int64
		want     [2]string
	}{
		{"both advance", []int64{1, 1}, [2]string{OutcomeOK, OutcomeOK}},
		{"only the companion advances", []int64{0, 1}, [2]string{OutcomeRejectedGuard, OutcomeOK}},
		{"only the first model advances", []int64{1, 0}, [2]string{OutcomeOK, OutcomeRejectedGuard}},
		{"both rejected", []int64{0, 0}, [2]string{OutcomeRejectedGuard, OutcomeRejectedGuard}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, database, _, _ := newPassRunner(t)
			withCompanion(runner)
			database.upsertAffectedSeq = tc.affected

			pass := runner.RunOnce(context.Background())

			if len(pass.Models) != 2 || pass.Models[0].Outcome != tc.want[0] || pass.Models[1].Outcome != tc.want[1] {
				t.Fatalf("model outcomes = %+v, want %v", pass.Models, tc.want)
			}
			if pass.Outcome != tc.want[0] {
				t.Fatalf("pass outcome = %s, want the first model's %s", pass.Outcome, tc.want[0])
			}
			if database.committed != 2 {
				t.Fatalf("committed = %d, want 2: a rejected row is not an error", database.committed)
			}
		})
	}
}

// TestRunOnceKeepsTheFirstRowWhenACompanionFails: a failed companion rolls
// back only its own transaction. The first model's row is already committed
// and the pass is not an error.
func TestRunOnceKeepsTheFirstRowWhenACompanionFails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		fail func(companion *fakeStatement, database *fakeDatabase)
	}{
		{"companion compute fails", func(c *fakeStatement, _ *fakeDatabase) { c.err = errors.New("tfstate boom") }},
		{"companion upsert fails", func(_ *fakeStatement, d *fakeDatabase) { d.upsertErrSeq = []error{nil, errors.New("upsert boom")} }},
		{"companion deadline cut", func(c *fakeStatement, _ *fakeDatabase) { c.err = context.DeadlineExceeded }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, database, _, _ := newPassRunner(t)
			companion := withCompanion(runner)
			tc.fail(companion, database)

			pass := runner.RunOnce(context.Background())

			if pass.Outcome != OutcomeOK || pass.Err != nil {
				t.Fatalf("pass = %+v, want the first model's ok", pass)
			}
			if got := pass.Models[1]; got.Outcome != OutcomeError || got.Err == nil {
				t.Fatalf("companion = %+v, want an error outcome", got)
			}
			if database.committed != 1 || database.rolledBack != 1 {
				t.Fatalf("committed/rolled back = %d/%d, want 1/1: the first row stays", database.committed, database.rolledBack)
			}
			got := database.snapshot()
			if got[len(oneModelSequence)-1] != "commit" || got[len(got)-1] != "rollback" {
				t.Fatalf("pass statements = %v, want the first model committed and then the companion rolled back", got)
			}
		})
	}
}

// TestRunOnceRunsTheCompanionAfterAFirstModelFailure: the rows are
// independent, so a failed first model does not stop the companion.
func TestRunOnceRunsTheCompanionAfterAFirstModelFailure(t *testing.T) {
	t.Parallel()
	runner, database, primary, _ := newPassRunner(t)
	companion := withCompanion(runner)
	primary.err = errors.New("active work boom")

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeError || pass.Models[0].Err == nil {
		t.Fatalf("pass = %+v, want the first model's error", pass)
	}
	if pass.Models[1].Outcome != OutcomeOK || companion.callCount() != 1 {
		t.Fatalf("companion = %+v after %d computes, want it written", pass.Models[1], companion.callCount())
	}
	if database.committed != 1 || database.rolledBack != 1 {
		t.Fatalf("committed/rolled back = %d/%d, want 1/1", database.committed, database.rolledBack)
	}
}

// TestRunOnceSkipsTheCompanionWhenAnotherWriterHoldsTheLock: a companion that
// loses the lock after the first model committed is skipped_lock, and a first
// model that lost it leaves the companion unattempted with the same outcome.
func TestRunOnceSkipsTheCompanionWhenAnotherWriterHoldsTheLock(t *testing.T) {
	t.Parallel()
	t.Run("lock lost between the two transactions", func(t *testing.T) {
		t.Parallel()
		runner, database, _, _ := newPassRunner(t)
		companion := withCompanion(runner)
		database.lockAcquiredSeq = []bool{true, false}

		pass := runner.RunOnce(context.Background())

		if pass.Models[0].Outcome != OutcomeOK || pass.Models[1].Outcome != OutcomeSkippedLock {
			t.Fatalf("outcomes = %s, %s, want ok then skipped_lock", pass.Models[0].Outcome, pass.Models[1].Outcome)
		}
		if companion.callCount() != 0 || database.begun != 2 || database.committed != 1 {
			t.Fatalf("companion computes/begun/committed = %d/%d/%d, want 0/2/1", companion.callCount(), database.begun, database.committed)
		}
	})
	t.Run("lock never taken", func(t *testing.T) {
		t.Parallel()
		runner, database, primary, _ := newPassRunner(t)
		companion := withCompanion(runner)
		database.lockAcquired = false

		pass := runner.RunOnce(context.Background())

		if pass.Models[0].Outcome != OutcomeSkippedLock || pass.Models[1].Outcome != OutcomeSkippedLock {
			t.Fatalf("outcomes = %+v, want skipped_lock for both", pass.Models)
		}
		if database.begun != 1 || primary.callCount() != 0 || companion.callCount() != 0 {
			t.Fatalf("begun/computes = %d/%d/%d, want one transaction and no compute", database.begun, primary.callCount(), companion.callCount())
		}
	})
	t.Run("table missing", func(t *testing.T) {
		t.Parallel()
		runner, database, _, _ := newPassRunner(t)
		withCompanion(runner)
		database.tableInstalled = false

		pass := runner.RunOnce(context.Background())

		if pass.Models[0].Outcome != OutcomeSkippedMissingTable || pass.Models[1].Outcome != OutcomeSkippedMissingTable {
			t.Fatalf("outcomes = %+v, want skipped_missing_table for both", pass.Models)
		}
		if database.begun != 1 {
			t.Fatalf("begun = %d, want 1: a missing table is not probed twice", database.begun)
		}
	})
}

// TestRunOnceBoundsTheCompanionByOneInterval: the first model runs with the
// whole two-interval pass budget, a companion with at most one interval.
func TestRunOnceBoundsTheCompanionByOneInterval(t *testing.T) {
	t.Parallel()
	runner, _, primary, _ := newPassRunner(t)
	companion := withCompanion(runner)

	runner.RunOnce(context.Background())

	if len(primary.deadlines) != 1 || len(companion.deadlines) != 1 {
		t.Fatalf("deadlines recorded = %d and %d, want 1 each", len(primary.deadlines), len(companion.deadlines))
	}
	if primary.deadlines[0] <= MinInterval || primary.deadlines[0] > 2*MinInterval {
		t.Fatalf("first model budget = %s, want within (%s, %s]", primary.deadlines[0], MinInterval, 2*MinInterval)
	}
	if companion.deadlines[0] > MinInterval {
		t.Fatalf("companion budget = %s, want at most one interval %s", companion.deadlines[0], MinInterval)
	}
}

// TestRunOnceBoundsTheCompanionByTheRemainingPassBudget: a pass whose caller
// deadline leaves less than one interval gives the companion that remainder,
// not a full interval, so the companion budget is min(remaining, one interval).
func TestRunOnceBoundsTheCompanionByTheRemainingPassBudget(t *testing.T) {
	t.Parallel()
	runner, _, _, _ := newPassRunner(t)
	companion := withCompanion(runner)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runner.RunOnce(ctx)

	if len(companion.deadlines) != 1 {
		t.Fatalf("companion deadlines recorded = %d, want 1", len(companion.deadlines))
	}
	if companion.deadlines[0] <= 0 || companion.deadlines[0] > 2*time.Second {
		t.Fatalf("companion budget = %s, want at most the 2 s the pass has left (not the %s interval)", companion.deadlines[0], MinInterval)
	}
}

// TestRunOnceDoesNotRunACompanionAfterTheCallerCancels: the companion's context
// is derived from the pass context, so a caller cancel during the first model
// stops the companion before it computes. A companion on a context cut loose
// from the pass would run to completion.
func TestRunOnceDoesNotRunACompanionAfterTheCallerCancels(t *testing.T) {
	t.Parallel()
	runner, _, primary, _ := newPassRunner(t)
	companion := withCompanion(runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := runner.Statement.Compute
	runner.Statement.Compute = func(c context.Context, q db.Queryer, asOf time.Time) ([]store.Entry, error) {
		entries, err := first(c, q, asOf)
		cancel()
		return entries, err
	}

	pass := runner.RunOnce(ctx)

	if primary.callCount() != 1 {
		t.Fatalf("first model computes = %d, want 1", primary.callCount())
	}
	if companion.callCount() != 0 {
		t.Fatalf("the companion computed %d times after the caller cancelled, want 0", companion.callCount())
	}
	if len(pass.Models) != 2 || pass.Models[1].Outcome == OutcomeOK {
		t.Fatalf("pass models = %+v, want the companion not written", pass.Models)
	}
}

// TestRunOnceCutsTheCompanionWhenTheFirstModelUsesTheWholePassBudget: a first
// model that runs until the pass deadline leaves the companion no budget, so the
// companion does not compute and reports an error. A companion on a context cut
// loose from the pass would still compute.
func TestRunOnceCutsTheCompanionWhenTheFirstModelUsesTheWholePassBudget(t *testing.T) {
	t.Parallel()
	runner, _, primary, _ := newPassRunner(t)
	companion := withCompanion(runner)
	primary.block = make(chan struct{}) // the first model waits for its context to end
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pass := runner.RunOnce(ctx)

	if pass.Outcome != OutcomeError || pass.Models[0].Err == nil {
		t.Fatalf("first model = %+v, want an error at the deadline", pass.Models[0])
	}
	if companion.callCount() != 0 {
		t.Fatalf("the companion computed %d times after the pass deadline passed, want 0", companion.callCount())
	}
	if got := pass.Models[1]; got.Outcome != OutcomeError || got.Err == nil {
		t.Fatalf("companion = %+v, want an error outcome (cut by the pass deadline)", got)
	}
}

// TestRunOnceBoundsThePassByItsOwnTwoIntervalBudget: the runner's own pass
// deadline, not a caller's, ends the first model and leaves the companion no
// budget. The caller passes a context with no deadline, as the production loop
// does, and the test shortens the runner's budget so it does not wait seconds.
// A companion on the caller's context (without the pass deadline) would still
// compute with a full interval.
func TestRunOnceBoundsThePassByItsOwnTwoIntervalBudget(t *testing.T) {
	t.Parallel()
	runner, _, primary, _ := newPassRunner(t)
	companion := withCompanion(runner)
	runner.passBudgetOverride = 200 * time.Millisecond
	primary.block = make(chan struct{}) // the first model waits for the pass deadline

	pass := runner.RunOnce(context.Background())

	if pass.Models[0].Err == nil {
		t.Fatalf("first model = %+v, want it cut by the pass budget", pass.Models[0])
	}
	if companion.callCount() != 0 {
		t.Fatalf("the companion computed %d times with the pass budget spent, want 0", companion.callCount())
	}
	if got := pass.Models[1]; got.Outcome != OutcomeError {
		t.Fatalf("companion = %+v, want an error outcome", got)
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

// TestRunOnceRecordsEachModelTransactionOnItsOwn: the pass counter and the
// duration histogram carry each model's own outcome, and a companion skipped by
// the first model counts with that outcome and no duration.
func TestRunOnceRecordsEachModelTransactionOnItsOwn(t *testing.T) {
	t.Parallel()
	instruments, reader := newTestInstruments(t)
	runner, database, primary, _ := newPassRunner(t)
	runner.Instruments = instruments
	companion := withCompanion(runner)
	companion.clock, companion.cost = primary.clock, 700*time.Millisecond
	database.upsertAffectedSeq = []int64{1, 0}

	runner.RunOnce(context.Background())

	active, tfstate := telemetry.AttrModelKey(store.ModelActiveWorkSummary), telemetry.AttrModelKey(store.ModelTerraformState)
	const passes, durations = "eshu_dp_status_summary_writer_passes_total", "eshu_dp_status_summary_writer_pass_duration_seconds"
	if got := counterValue(t, reader, passes, active, telemetry.AttrOutcome(OutcomeOK)); got != 1 {
		t.Fatalf("passes_total ok(active_work_summary) = %d, want 1", got)
	}
	if got := counterValue(t, reader, passes, tfstate, telemetry.AttrOutcome(OutcomeRejectedGuard)); got != 1 {
		t.Fatalf("passes_total rejected_guard(terraform_state) = %d, want 1", got)
	}
	for name, sample := range map[string][2]attribute.KeyValue{
		"active_work_summary": {active, telemetry.AttrOutcome(OutcomeOK)},
		"terraform_state":     {tfstate, telemetry.AttrOutcome(OutcomeRejectedGuard)},
	} {
		if n := histogramCount(t, reader, durations, sample[0], sample[1]); n != 1 {
			t.Fatalf("pass_duration samples for %s = %d, want 1", name, n)
		}
	}

	database.lockAcquired = false
	runner.RunOnce(context.Background())
	for _, key := range []attribute.KeyValue{active, tfstate} {
		if got := counterValue(t, reader, passes, key, telemetry.AttrOutcome(OutcomeSkippedLock)); got != 1 {
			t.Fatalf("skipped_lock for %v = %d, want 1", key, got)
		}
	}
	if n := histogramCount(t, reader, durations, tfstate, telemetry.AttrOutcome(OutcomeSkippedLock)); n != 0 {
		t.Fatalf("duration samples for an unattempted companion = %d, want 0", n)
	}
}

// TestRunReportsUpForEveryModel: absent(writer_up{model_key="terraform_state"})
// is only meaningful when the runner reports the companion's gauge.
func TestRunReportsUpForEveryModel(t *testing.T) {
	t.Parallel()
	instruments, reader := newTestInstruments(t)
	runner, _, _, _ := newPassRunner(t)
	runner.Instruments = instruments
	withCompanion(runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up := map[string]int64{}
	runner.Wait = (&scriptedWait{limit: 1, cancel: cancel, before: func(int) {
		for _, model := range []string{store.ModelActiveWorkSummary, store.ModelTerraformState} {
			up[model] = counterValue(t, reader, "eshu_dp_status_summary_writer_up", telemetry.AttrModelKey(model))
		}
	}}).wait

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	for model, got := range up {
		if got != 1 {
			t.Fatalf("writer_up{model_key=%s} during the loop = %d, want 1", model, got)
		}
	}
	if len(up) != 2 {
		t.Fatalf("writer_up observed for %d models, want 2", len(up))
	}
	for _, model := range []string{store.ModelActiveWorkSummary, store.ModelTerraformState} {
		if got := counterValue(t, reader, "eshu_dp_status_summary_writer_up", telemetry.AttrModelKey(model)); got != 0 {
			t.Fatalf("writer_up{model_key=%s} after the loop = %d, want 0", model, got)
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
