package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedServiceRecordsRunDurationOnSuccess proves the claimed-service seam
// records eshu_dp_workflow_claim_run_duration_seconds with outcome=success after
// a normal collect+commit+complete cycle. This is the primary per-collector
// long-pole signal the issue requires.
func TestClaimedServiceRecordsRunDurationOnSuccess(t *testing.T) {
	t.Parallel()

	rm := runClaimedServiceWithMetrics(t, claimedRunScenario{
		kind:       scope.CollectorGit,
		sourceKind: "git",
		source:     successSource(t),
	})

	gotCount := claimedHistogramCount(t, rm, "eshu_dp_workflow_claim_run_duration_seconds")
	if gotCount != 1 {
		t.Fatalf("eshu_dp_workflow_claim_run_duration_seconds count = %d, want 1", gotCount)
	}
	if !claimedHistogramHasAttr(t, rm, "eshu_dp_workflow_claim_run_duration_seconds", "collector_kind") {
		t.Fatal("eshu_dp_workflow_claim_run_duration_seconds missing collector_kind attribute")
	}
	if !claimedHistogramHasAttr(t, rm, "eshu_dp_workflow_claim_run_duration_seconds", "outcome") {
		t.Fatal("eshu_dp_workflow_claim_run_duration_seconds missing outcome attribute")
	}
}

// TestClaimedServiceRunDurationOutcomeSuccess proves the outcome label is
// "success" on a normal collect+commit+complete path.
func TestClaimedServiceRunDurationOutcomeSuccess(t *testing.T) {
	t.Parallel()

	rm := runClaimedServiceWithMetrics(t, claimedRunScenario{
		kind:       scope.CollectorGit,
		sourceKind: "git",
		source:     successSource(t),
	})

	got := claimedHistogramOutcome(t, rm, "eshu_dp_workflow_claim_run_duration_seconds")
	if got != telemetry.CollectorRunOutcomeSuccess {
		t.Fatalf("outcome = %q, want %q", got, telemetry.CollectorRunOutcomeSuccess)
	}
}

// TestClaimedServiceRunDurationOutcomeUnchanged proves the outcome label is
// "unchanged" when the source reports Unchanged == true (no new facts, claim
// completed without commit).
func TestClaimedServiceRunDurationOutcomeUnchanged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)

	unchangedGeneration := FactsFromSlice(testScope(), testGeneration(now), nil)
	unchangedGeneration.Unchanged = true

	instruments, metricReader := newTestInstrumentsPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &stubClaimStore{item: item, claim: claim, found: true}
	// Heartbeat cancels the context after the unchanged cycle completes.
	store.heartbeat = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}

	svc := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{
		collected: unchangedGeneration,
		ok:        true,
	}, &stubClaimedCommitter{})
	svc.Instruments = instruments

	if err := svc.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	got := claimedHistogramOutcome(t, rm, "eshu_dp_workflow_claim_run_duration_seconds")
	if got != telemetry.CollectorRunOutcomeUnchanged {
		t.Fatalf("outcome = %q, want %q", got, telemetry.CollectorRunOutcomeUnchanged)
	}
}

// TestClaimedServiceRunDurationOutcomeFailRetryable proves the outcome label is
// "fail_retryable" when NextClaimed returns a retryable error.
func TestClaimedServiceRunDurationOutcomeFailRetryable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)

	instruments, metricReader := newTestInstrumentsPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &stubClaimStore{item: item, claim: claim, found: true}
	store.heartbeat = func(context.Context, workflow.ClaimMutation) error { return nil }
	// After retryable fail is recorded, cancel so Run exits.
	store.retryableFail = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}

	svc := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{
		err: errors.New("transient network error"),
	}, &stubClaimedCommitter{})
	svc.Instruments = instruments

	if err := svc.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	got := claimedHistogramOutcome(t, rm, "eshu_dp_workflow_claim_run_duration_seconds")
	if got != telemetry.CollectorRunOutcomeFailRetryable {
		t.Fatalf("outcome = %q, want %q", got, telemetry.CollectorRunOutcomeFailRetryable)
	}
}

// TestClaimedServiceRunDurationOutcomeFailTerminal proves the outcome label is
// "fail_terminal" when NextClaimed returns a terminal error.
func TestClaimedServiceRunDurationOutcomeFailTerminal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)

	instruments, metricReader := newTestInstrumentsPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &stubClaimStore{item: item, claim: claim, found: true}
	store.heartbeat = func(context.Context, workflow.ClaimMutation) error { return nil }
	store.terminalFail = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}

	svc := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{
		err: &terminalTestError{msg: "permanent failure"},
	}, &stubClaimedCommitter{})
	svc.Instruments = instruments

	if err := svc.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	got := claimedHistogramOutcome(t, rm, "eshu_dp_workflow_claim_run_duration_seconds")
	if got != telemetry.CollectorRunOutcomeFailTerminal {
		t.Fatalf("outcome = %q, want %q", got, telemetry.CollectorRunOutcomeFailTerminal)
	}
}

// TestClaimedServiceRecordsFactsEmittedOnSuccess proves
// eshu_dp_workflow_claim_facts_emitted_total is recorded with the fact count
// from CollectedGeneration.FactCount on a successful commit.
func TestClaimedServiceRecordsFactsEmittedOnSuccess(t *testing.T) {
	t.Parallel()

	rm := runClaimedServiceWithMetrics(t, claimedRunScenario{
		kind:       scope.CollectorGit,
		sourceKind: "git",
		source:     successSource(t),
	})

	got := collectorCounterValue(t, rm, "eshu_dp_workflow_claim_facts_emitted_total", map[string]string{
		"collector_kind": string(scope.CollectorGit),
		"source_system":  "git",
	})
	// successSource returns FactCount == 1.
	if got != 1 {
		t.Fatalf("eshu_dp_workflow_claim_facts_emitted_total = %d, want 1", got)
	}
}

// TestClaimedServiceDoesNotRecordFactsEmittedOnFailure proves that
// eshu_dp_workflow_claim_facts_emitted_total is NOT incremented when collection
// fails (retryable or terminal).
func TestClaimedServiceDoesNotRecordFactsEmittedOnFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)

	instruments, metricReader := newTestInstrumentsPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &stubClaimStore{item: item, claim: claim, found: true}
	store.heartbeat = func(context.Context, workflow.ClaimMutation) error { return nil }
	store.retryableFail = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}

	svc := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{
		err: errors.New("transient error"),
	}, &stubClaimedCommitter{})
	svc.Instruments = instruments

	if err := svc.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Counter must not appear for the failure case.
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "eshu_dp_workflow_claim_facts_emitted_total" {
				t.Fatal("eshu_dp_workflow_claim_facts_emitted_total recorded on failure, want no recording")
			}
		}
	}
}

// TestClaimedServiceRunMetricsSafeWithNilInstruments proves that a
// ClaimedService with nil Instruments does not panic when processClaimed runs.
func TestClaimedServiceRunMetricsSafeWithNilInstruments(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &stubClaimStore{item: item, claim: claim, found: true}
	store.heartbeat = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}

	svc := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{
		collected: successCollectedGeneration(t, now),
		ok:        true,
	}, &stubClaimedCommitter{})
	// Instruments is nil — must not panic.
	svc.Instruments = nil

	if err := svc.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

// ---- helpers ----------------------------------------------------------------

// claimedRunScenario configures a single-cycle ClaimedService run for metric tests.
type claimedRunScenario struct {
	kind       scope.CollectorKind
	sourceKind string
	source     *stubClaimedSource
}

// runClaimedServiceWithMetrics runs a ClaimedService scenario and returns the
// collected ResourceMetrics. The service runs until the heartbeat cancels the
// context after at least one claimed cycle completes.
func runClaimedServiceWithMetrics(t *testing.T, sc claimedRunScenario) metricdata.ResourceMetrics {
	t.Helper()

	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = sc.sourceKind
	item.CollectorKind = sc.kind
	claim := testWorkflowClaim(item.WorkItemID, now)

	instruments, metricReader := newTestInstrumentsPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	store := &stubClaimStore{item: item, claim: claim, found: true}
	store.heartbeat = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}

	svc := testClaimedService(now, claim, sc.kind, store, sc.source, &stubClaimedCommitter{})
	svc.Instruments = instruments

	if err := svc.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

// successSource returns a stubClaimedSource with one fact and FactCount == 1.
func successSource(t *testing.T) *stubClaimedSource {
	t.Helper()
	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	return &stubClaimedSource{
		collected: successCollectedGeneration(t, now),
		ok:        true,
	}
}

// successCollectedGeneration returns a CollectedGeneration with FactCount == 1
// and one fact in the channel so committers can drain it.
func successCollectedGeneration(t *testing.T, now time.Time) CollectedGeneration {
	t.Helper()
	gen := FactsFromSlice(testScope(), testGeneration(now), testFacts(now))
	gen.FactCount = 1
	return gen
}

// newTestInstrumentsPair creates a fresh Instruments wired to a ManualReader so
// Collect returns all recorded values. Both the Instruments and the reader share
// the same MeterProvider instance.
func newTestInstrumentsPair(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("claimed-run-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// claimedHistogramOutcome extracts the "outcome" attribute value from the first
// data point of a Float64Histogram metric. It fails the test if the metric is
// not found or the outcome attribute is absent.
func claimedHistogramOutcome(t *testing.T, rm metricdata.ResourceMetrics, metricName string) string {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want Histogram[float64]", metricName, m.Data)
			}
			for _, dp := range histogram.DataPoints {
				for _, attr := range dp.Attributes.ToSlice() {
					if string(attr.Key) == "outcome" {
						return attr.Value.AsString()
					}
				}
			}
			t.Fatalf("metric %s has no outcome attribute", metricName)
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return ""
}

// terminalTestError is a test-only error that implements the terminalFailure
// interface so isTerminalFailure returns true.
type terminalTestError struct{ msg string }

func (e *terminalTestError) Error() string         { return e.msg }
func (e *terminalTestError) TerminalFailure() bool { return true }
