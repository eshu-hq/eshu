// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// admissionTimeoutBudgetTestError mirrors the production ingester
// reducerAdmissionTimeoutError contract: retryable, self-classifying as
// admission_timeout, and opting into the failure.AdmissionTimeoutError
// narrow interface. It stands in for that type here because the ingester
// main package cannot be imported from this package's tests.
type admissionTimeoutBudgetTestError struct{ message string }

func (e *admissionTimeoutBudgetTestError) Error() string { return e.message }

func (e *admissionTimeoutBudgetTestError) Retryable() bool { return true }

func (e *admissionTimeoutBudgetTestError) IsAdmissionTimeout() bool { return true }

func (e *admissionTimeoutBudgetTestError) FailureClass() string { return "admission_timeout" }

// productionWrapAdmissionError mirrors the projector runtime's wrap at
// runtime/projection.go ("enqueue reducer intents: %w"): the admission
// error reaches Fail through exactly one %w layer, so errors.As must find
// IsAdmissionTimeout through the wrap.
func productionWrapAdmissionError(err error) error {
	return fmt.Errorf("enqueue reducer intents: %w", err)
}

// TestProjectorQueueFailRetriesAdmissionTimeoutWhenProjectionBudgetExhausted
// proves an admission-timeout failure retries under its own admission
// budget even when the shared projection attempt budget is spent: a work
// item at attempt_count == MaxAttempts failing with the production-wrapped
// admission-timeout error must take the retry path, not the dead-letter
// path that chunk-2 and chunk-3 died on.
func TestProjectorQueueFailRetriesAdmissionTimeoutWhenProjectionBudgetExhausted(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
		// Two non-admission claims spent the projection budget; this
		// admission-timeout failure still retries because the
		// admission budget (untouched at 0) is separate.
		AttemptCount:          2,
		AdmissionWaitAttempts: 0,
		// AdmissionMaxAttempts is unset: the default admission budget
		// (48 waits) applies.
	}

	cause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	if !strings.Contains(query, "status = 'retrying'") {
		t.Fatalf("exhausted-budget admission timeout should retry, query:\n%s", query)
	}
	if strings.Contains(query, "status = 'dead_letter'") {
		t.Fatalf("exhausted-budget admission timeout must not dead-letter, query:\n%s", query)
	}
	for _, want := range []string{
		"admission_wait_attempts",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("admission retry query missing %q:\n%s", want, query)
		}
	}
}

// TestProjectorQueueFailDeadLettersAdmissionTimeoutWhenAdmissionBudgetExhausted
// proves the admission budget is still finite: once the admission waits
// hit the configured admission budget, the item dead-letters with the
// admission_timeout class instead of retrying forever.
func TestProjectorQueueFailDeadLettersAdmissionTimeoutWhenAdmissionBudgetExhausted(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 5
	queue.AdmissionMaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:                 scope.IngestionScope{ScopeID: "scope-123"},
		Generation:            scope.ScopeGeneration{GenerationID: "generation-456"},
		AttemptCount:          2,
		AdmissionWaitAttempts: 2,
	}

	cause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("budget-exhausted admission timeout should dead-letter, query:\n%s", db.execs[0].query)
	}
	gotClass, _ := db.execs[0].args[1].(string)
	if gotClass != "admission_timeout" {
		t.Fatalf("dead-letter failure_class = %q, want %q", gotClass, "admission_timeout")
	}
}

// TestProjectorQueueFailRetriesAdmissionTimeoutBelowBudget proves the
// fence counts like its sibling: with budget N, the first N-1 waits
// retry and the Nth dead-letters. Here the second wait of a budget of 3
// (counter 1: 1+1 < 3) still retries.
func TestProjectorQueueFailRetriesAdmissionTimeoutBelowBudget(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 2
	queue.AdmissionMaxAttempts = 3
	queue.Now = func() time.Time {
		return time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:                 scope.IngestionScope{ScopeID: "scope-123"},
		Generation:            scope.ScopeGeneration{GenerationID: "generation-456"},
		AttemptCount:          3,
		AdmissionWaitAttempts: 1,
	}

	cause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("below-budget admission timeout should retry, query:\n%s", db.execs[0].query)
	}
}

// TestProjectorQueueFailDeadLettersAdmissionTimeoutOnNthWait proves the
// other side of the same fence: the Nth wait of a budget of N
// dead-letters (counter N-1: (N-1)+1 < N is false), matching the sibling
// projection fence where the Nth failed claim dead-letters.
func TestProjectorQueueFailDeadLettersAdmissionTimeoutOnNthWait(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 5
	queue.AdmissionMaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:                 scope.IngestionScope{ScopeID: "scope-123"},
		Generation:            scope.ScopeGeneration{GenerationID: "generation-456"},
		AttemptCount:          3,
		AdmissionWaitAttempts: 1,
	}

	cause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("Nth admission wait should dead-letter, query:\n%s", db.execs[0].query)
	}
	gotClass, _ := db.execs[0].args[1].(string)
	if gotClass != "admission_timeout" {
		t.Fatalf("dead-letter failure_class = %q, want %q", gotClass, "admission_timeout")
	}
}

// TestProjectorQueueFailRefundsAdmissionWaitsFromProjectionBudget proves
// admission waits do not burn the projection budget: after N admission
// waits, a genuine retryable projection failure still gets the full
// projection budget measured from non-admission claims.
func TestProjectorQueueFailRefundsAdmissionWaitsFromProjectionBudget(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
		// Three claims, two of them admission waits: only one projection
		// failure so far, inside the budget of two.
		AttemptCount:          3,
		AdmissionWaitAttempts: 2,
	}

	if err := queue.Fail(context.Background(), work, &retryableTestError{message: "graph write timed out"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("refunded projection budget should retry, query:\n%s", db.execs[0].query)
	}
}

// collectCounter sums the int64 data points of one counter by name.
func collectCounter(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// TestProjectorQueueFailEmitsSeparateAdmissionRetryCounter proves an
// operator can count admission-wait retries separately from projection
// retries: an admission-timeout retry lands on
// eshu_dp_projector_admission_wait_retries_total and not on the retry
// surge, while a generic retryable failure does the reverse.
func TestProjectorQueueFailEmitsSeparateAdmissionRetryCounter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	work := projector.ScopeGenerationWork{
		Scope:                 scope.IngestionScope{ScopeID: "scope-123"},
		Generation:            scope.ScopeGeneration{GenerationID: "generation-456"},
		AttemptCount:          1,
		AdmissionWaitAttempts: 0,
	}

	admissionReader := sdkmetric.NewManualReader()
	admissionInstruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(admissionReader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	admissionDB := &recordingExecQueryer{}
	admissionQueue := NewProjectorQueue(admissionDB, "projector-1", 30*time.Second)
	admissionQueue.MaxAttempts = 3
	admissionQueue.Instruments = admissionInstruments
	admissionQueue.Now = func() time.Time { return now }
	cause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := admissionQueue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got := collectCounter(t, admissionReader, "eshu_dp_projector_admission_wait_retries_total"); got != 1 {
		t.Fatalf("admission wait retries = %d, want 1", got)
	}
	if got := collectCounter(t, admissionReader, "eshu_dp_projector_retry_surge_total"); got != 0 {
		t.Fatalf("retry surge after admission retry = %d, want 0", got)
	}

	projectionReader := sdkmetric.NewManualReader()
	projectionInstruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(projectionReader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	projectionDB := &recordingExecQueryer{}
	projectionQueue := NewProjectorQueue(projectionDB, "projector-1", 30*time.Second)
	projectionQueue.MaxAttempts = 3
	projectionQueue.Instruments = projectionInstruments
	projectionQueue.Now = func() time.Time { return now }
	if err := projectionQueue.Fail(context.Background(), work, &retryableTestError{message: "graph write timed out"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got := collectCounter(t, projectionReader, "eshu_dp_projector_retry_surge_total"); got != 1 {
		t.Fatalf("retry surge = %d, want 1", got)
	}
	if got := collectCounter(t, projectionReader, "eshu_dp_projector_admission_wait_retries_total"); got != 0 {
		t.Fatalf("admission wait retries after projection retry = %d, want 0", got)
	}
}

// TestReprojectReopenMirrorsAttemptResetForAdmissionCounter pins the
// #10671 paired-budget invariant on the freshness reconcile re-open: the
// CASE that resets attempt_count for terminal rows must reset
// admission_wait_attempts on the same predicate, and the INSERT must seed
// the counter at 0, or a reopened terminal row keeps a spent admission
// budget (dead-letter on its first new wait) while the refund goes
// negative (retrying everything as "projection" failures). The
// refinalize twin is pinned in rebuild/reset;
// TestProjectorQueueAdmissionRefinalizeCycleLive proves the reopen path
// end to end.
func TestReprojectReopenMirrorsAttemptResetForAdmissionCounter(t *testing.T) {
	t.Parallel()

	const terminal = "WHEN fact_work_items.status IN ('succeeded', 'failed', 'dead_letter') THEN 0"
	// Four CASEs: attempt_count, admission_wait_attempts, source_paced_waits
	// (#10882) and the #11182 replay_attempt_baseline (TestReprojectResetsReplayBaseline
	// pins it).
	if got := strings.Count(reprojectReopenedCTE, terminal); got != 4 {
		t.Fatalf("reproject reopen CTE resets attempt/admission/source-paced/baseline counters %d times, want 4 (one CASE each)", got)
	}
	if !strings.Contains(reprojectReopenedCTE, "source_paced_waits = CASE") {
		t.Fatalf("reproject reopen CTE must reset source_paced_waits with the terminal CASE (#10882)")
	}
	if !strings.Contains(reprojectReopenedCTE, "admission_wait_attempts = CASE") {
		t.Fatalf("reproject reopen CTE must reset admission_wait_attempts with the terminal CASE")
	}
	insert, _, _ := strings.Cut(reprojectReopenedCTE, "ON CONFLICT")
	for _, want := range []string{"attempt_count, admission_wait_attempts, source_paced_waits", "'pending', 0, 0, 0,"} {
		if !strings.Contains(insert, want) {
			t.Fatalf("reproject reopen INSERT missing %q", want)
		}
	}
}

// TestProjectorQueueFailDeadLettersGenericRetryableWhenRefundedBudgetExhausted
// pins the other side of the refund fence: when non-admission claims alone
// exhaust the projection budget, a generic retryable failure dead-letters.
func TestProjectorQueueFailDeadLettersGenericRetryableWhenRefundedBudgetExhausted(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
		// Four claims, two admission waits: two genuine projection
		// failures, at the budget of two.
		AttemptCount:          4,
		AdmissionWaitAttempts: 2,
	}

	if err := queue.Fail(context.Background(), work, &retryableTestError{message: "still broken"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'dead_letter'") {
		t.Fatalf("exhausted refunded budget should dead-letter, query:\n%s", db.execs[0].query)
	}
}
