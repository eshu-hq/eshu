package collector

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedServiceUsesClassifiedCollectFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 20, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	source := &stubClaimedSource{err: terraformstate.WaitingOnGitGenerationError{RepoIDs: []string{"platform-infra"}}}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, source, &stubClaimedCommitter{})

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want waiting error")
	}
	if got, want := store.lastRetryableFail.FailureClass, "waiting_on_git_generation"; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
}

func TestClaimedGenerationAllowsTerraformStateCandidateGenerationIDs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 9, 5, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	item.ScopeID = "state_snapshot:s3:locator-hash"
	item.GenerationID = "candidate-generation-1"
	item.SourceRunID = "candidate-source-run-1"
	collected := FactsFromSlice(
		scope.IngestionScope{
			ScopeID:       item.ScopeID,
			SourceSystem:  string(scope.CollectorTerraformState),
			ScopeKind:     scope.KindStateSnapshot,
			CollectorKind: scope.CollectorTerraformState,
			PartitionKey:  "terraform_state:s3:locator-hash",
		},
		scope.ScopeGeneration{
			GenerationID:  "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17",
			ScopeID:       item.ScopeID,
			ObservedAt:    now,
			IngestedAt:    now,
			Status:        scope.GenerationStatusPending,
			TriggerKind:   scope.TriggerKindSnapshot,
			FreshnessHint: "lineage=lineage-123 serial=17",
		},
		nil,
	)

	if err := validateClaimedGeneration(item, collected); err != nil {
		t.Fatalf("validateClaimedGeneration() error = %v, want nil", err)
	}
}

func TestClaimedGenerationRejectsGitCandidateGenerationIDs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 9, 10, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.GenerationID = "candidate-generation-1"
	item.SourceRunID = "candidate-source-run-1"
	collected := FactsFromSlice(testScope(), testGeneration(now), nil)

	err := validateClaimedGeneration(item, collected)
	if err == nil {
		t.Fatal("validateClaimedGeneration() error = nil, want git generation mismatch")
	}
	if got := err.Error(); !strings.Contains(got, "claimed generation_id") {
		t.Fatalf("validateClaimedGeneration() error = %q, want generation_id mismatch", got)
	}
}

func TestClaimedServiceRecordsTerraformStateClaimWait(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 20, 10, 0, 0, time.UTC)
	item := testClaimedWorkItem(now.Add(-10 * time.Second))
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	item.ScopeID = "scope-claim-1"
	item.GenerationID = "generation-claim-1"
	item.SourceRunID = item.GenerationID
	item.VisibleAt = now.Add(-5 * time.Second)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		release: func(context.Context, workflow.ClaimMutation) error {
			return nil
		},
	}
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("claimed-service-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, &stubClaimedSource{}, &stubClaimedCommitter{})
	service.Instruments = instruments

	ctx, cancel := context.WithCancel(context.Background())
	store.release = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := claimedHistogramCount(t, rm, "eshu_dp_tfstate_claim_wait_seconds"); got != 1 {
		t.Fatalf("eshu_dp_tfstate_claim_wait_seconds count = %d, want 1", got)
	}
	if claimedHistogramHasAttr(t, rm, "eshu_dp_tfstate_claim_wait_seconds", "scope_id") {
		t.Fatal("eshu_dp_tfstate_claim_wait_seconds has scope_id label, want bounded labels only")
	}
}

func claimedHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string) uint64 {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			var count uint64
			for _, point := range histogram.DataPoints {
				count += point.Count
			}
			return count
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return 0
}

func claimedHistogramHasAttr(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	key string,
) bool {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == key {
						return true
					}
				}
			}
			return false
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return false
}
