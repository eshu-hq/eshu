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

// rateLimitedCollectFailure is a retryable collect failure classified as
// provider rate-limiting but carrying no provider Retry-After delay, so its
// backoff falls back to the local poll interval.
type rateLimitedCollectFailure struct{}

func (rateLimitedCollectFailure) Error() string        { return "registry throttled" }
func (rateLimitedCollectFailure) FailureClass() string { return RegistryFailureRateLimited }

func newBackpressureInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() err = %v", err)
	}
	return instruments, reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() err = %v", err)
	}
	return rm
}

// TestClaimedServiceRecordsPerFamilyRetryCounter proves a retryable claim
// re-queue increments eshu_dp_workflow_claim_retries_total labeled by
// collector_kind, source_system, and failure_class so an operator can attribute
// retry pressure to a family without high-cardinality labels (issue #2699).
func TestClaimedServiceRecordsPerFamilyRetryCounter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: errors.New("temporary failure")}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	instruments, reader := newBackpressureInstruments(t)
	service.Instruments = instruments

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := claimMetricValue(t, collectMetrics(t, reader), "eshu_dp_workflow_claim_retries_total", map[string]string{
		telemetry.MetricDimensionCollectorKind: string(scope.CollectorGit),
		telemetry.MetricDimensionSourceSystem:  "git",
		telemetry.MetricDimensionFailureClass:  "collect_failure",
	})
	if got != 1 {
		t.Fatalf("retry counter = %d, want 1", got)
	}
}

// TestClaimedServiceProviderThrottleRecordsRetryAfterHonored proves a retryable
// failure carrying a provider Retry-After longer than the poll interval is
// counted as provider backpressure with outcome retry_after_honored.
func TestClaimedServiceProviderThrottleRecordsRetryAfterHonored(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 17, 12, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "github"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: retryAfterCollectFailure{delay: 45 * time.Second}}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	instruments, reader := newBackpressureInstruments(t)
	service.Instruments = instruments

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := claimMetricValue(t, collectMetrics(t, reader), "eshu_dp_workflow_claim_provider_throttle_total", map[string]string{
		telemetry.MetricDimensionCollectorKind: string(scope.CollectorGit),
		telemetry.MetricDimensionSourceSystem:  "github",
		telemetry.MetricDimensionOutcome:       claimThrottleOutcomeRetryAfterHonored,
	})
	if got != 1 {
		t.Fatalf("provider throttle counter = %d, want 1 with retry_after_honored", got)
	}
}

// TestClaimedServiceProviderThrottleRecordsPollBackoff proves a rate-limited
// failure with no provider Retry-After is counted as provider backpressure with
// outcome poll_backoff, so an operator can tell provider-driven pacing from
// local backoff.
func TestClaimedServiceProviderThrottleRecordsPollBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 17, 13, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "github"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: rateLimitedCollectFailure{}}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	instruments, reader := newBackpressureInstruments(t)
	service.Instruments = instruments

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := claimMetricValue(t, collectMetrics(t, reader), "eshu_dp_workflow_claim_provider_throttle_total", map[string]string{
		telemetry.MetricDimensionCollectorKind: string(scope.CollectorGit),
		telemetry.MetricDimensionSourceSystem:  "github",
		telemetry.MetricDimensionOutcome:       claimThrottleOutcomePollBackoff,
	})
	if got != 1 {
		t.Fatalf("provider throttle counter = %d, want 1 with poll_backoff", got)
	}
}

// TestClaimedServiceProviderThrottleCountsShortRetryAfterAsPollBackoff pins the
// intended magnitude-independent throttle count: a provider Retry-After shorter
// than the poll interval is still a throttle event, but its backoff is governed
// by the poll floor, so it is recorded with outcome poll_backoff.
func TestClaimedServiceProviderThrottleCountsShortRetryAfterAsPollBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 17, 13, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "github"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	// PollInterval is 1ms in testClaimedService; a zero Retry-After is shorter,
	// so the poll floor governs the backoff.
	source := &stubClaimedSource{err: retryAfterCollectFailure{delay: 0}}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	instruments, reader := newBackpressureInstruments(t)
	service.Instruments = instruments

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := claimMetricValue(t, collectMetrics(t, reader), "eshu_dp_workflow_claim_provider_throttle_total", map[string]string{
		telemetry.MetricDimensionCollectorKind: string(scope.CollectorGit),
		telemetry.MetricDimensionSourceSystem:  "github",
		telemetry.MetricDimensionOutcome:       claimThrottleOutcomePollBackoff,
	})
	if got != 1 {
		t.Fatalf("provider throttle counter = %d, want 1 with poll_backoff for short Retry-After", got)
	}
}

// TestClaimedServiceRecordsClaimLeaseAge proves the lease-age histogram records
// an active claim's held duration labeled by collector_kind and source_system,
// the 3 AM signal that a family is stalling before its lease is reaped.
func TestClaimedServiceRecordsClaimLeaseAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)
	service := testClaimedService(now, claim, scope.CollectorGit, &stubClaimStore{}, &stubClaimedSource{}, &stubClaimedCommitter{})
	instruments, reader := newBackpressureInstruments(t)
	service.Instruments = instruments

	service.recordClaimLeaseAge(context.Background(), item, 12.5)

	rm := collectMetrics(t, reader)
	if got := claimedHistogramCount(t, rm, "eshu_dp_workflow_claim_lease_age_seconds"); got != 1 {
		t.Fatalf("lease age histogram count = %d, want 1", got)
	}
	if !claimedHistogramHasAttr(t, rm, "eshu_dp_workflow_claim_lease_age_seconds", telemetry.MetricDimensionCollectorKind) {
		t.Fatal("lease age histogram missing collector_kind attribute")
	}
	if !claimedHistogramHasAttr(t, rm, "eshu_dp_workflow_claim_lease_age_seconds", telemetry.MetricDimensionSourceSystem) {
		t.Fatal("lease age histogram missing source_system attribute")
	}
}
