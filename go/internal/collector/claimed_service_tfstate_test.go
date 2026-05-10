package collector

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

func TestClaimedServiceCompletesUnchangedTerraformStateClaimWithoutCommit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 10, 11, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	store.heartbeat = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}
	source := &stubClaimedSource{
		collected: CollectedGeneration{Unchanged: true},
		ok:        true,
	}
	committer := &stubClaimedCommitter{}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, source, committer)

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
	if got, want := store.releaseCalls, 0; got != want {
		t.Fatalf("release calls = %d, want %d", got, want)
	}
	if got, want := committer.claimedCalls, 0; got != want {
		t.Fatalf("claimed commit calls = %d, want %d", got, want)
	}
}

func TestClaimedServiceFailsTerraformStateCommitWhenFactStreamReportsError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 11, 5, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	replayErr := errors.New("terraform state fact stream replay failed")
	collectedScope := scope.IngestionScope{
		ScopeID:       item.ScopeID,
		SourceSystem:  string(scope.CollectorTerraformState),
		ScopeKind:     scope.KindStateSnapshot,
		CollectorKind: scope.CollectorTerraformState,
		PartitionKey:  "terraform_state:s3:locator-hash",
	}
	collectedGeneration := testGeneration(now)
	collectedGeneration.FreshnessHint = "lineage=lineage-123 serial=17"
	collected := FactsFromSlice(collectedScope, collectedGeneration, testFacts(now))
	collected.FactStreamErr = func() error { return replayErr }
	committer := &stubClaimedCommitter{
		claimedCommitWithStreamError: func(
			_ context.Context,
			_ workflow.ClaimMutation,
			_ scope.IngestionScope,
			_ scope.ScopeGeneration,
			factStream <-chan facts.Envelope,
			factStreamErr func() error,
		) error {
			for range factStream {
			}
			return factStreamErr()
		},
	}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, &stubClaimedSource{
		collected: collected,
		ok:        true,
	}, committer)

	err := service.Run(context.Background())
	if !errors.Is(err, replayErr) {
		t.Fatalf("Run() error = %v, want %v", err, replayErr)
	}
	if got, want := store.completeCalls, 0; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := committer.claimedStreamErrorCalls, 1; got != want {
		t.Fatalf("stream-error commit calls = %d, want %d", got, want)
	}
}

func TestClaimedServiceDrainsTerraformStateStreamOnValidationFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 11, 7, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	item.ScopeID = "state_snapshot:s3:locator-hash"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	factStream, streamDrained := blockingFactStream(600, now)
	streamErrCalled := make(chan struct{})
	collected := CollectedGeneration{
		Scope: scope.IngestionScope{
			ScopeID:       "wrong-scope",
			SourceSystem:  string(scope.CollectorTerraformState),
			ScopeKind:     scope.KindStateSnapshot,
			CollectorKind: scope.CollectorTerraformState,
			PartitionKey:  "terraform_state:s3:locator-hash",
		},
		Generation: scope.ScopeGeneration{
			GenerationID:  "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17",
			ScopeID:       "wrong-scope",
			ObservedAt:    now,
			IngestedAt:    now,
			Status:        scope.GenerationStatusPending,
			TriggerKind:   scope.TriggerKindSnapshot,
			FreshnessHint: "lineage=lineage-123 serial=17",
		},
		Facts: factStream,
		FactStreamErr: func() error {
			close(streamErrCalled)
			return nil
		},
	}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, &stubClaimedSource{
		collected: collected,
		ok:        true,
	}, &stubClaimedCommitter{})

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want validation error")
	}
	if got := err.Error(); !strings.Contains(got, "claimed scope_id") {
		t.Fatalf("Run() error = %q, want claimed scope_id mismatch", got)
	}
	select {
	case <-streamDrained:
	case <-time.After(time.Second):
		t.Fatal("fact stream producer blocked; validation failure did not drain facts")
	}
	select {
	case <-streamErrCalled:
	case <-time.After(time.Second):
		t.Fatal("FactStreamErr was not called after validation failure cleanup")
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got, want := store.completeCalls, 0; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
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

func blockingFactStream(count int, observedAt time.Time) (<-chan facts.Envelope, <-chan struct{}) {
	ch := make(chan facts.Envelope, 500)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(ch)
		for i := 0; i < count; i++ {
			ch <- facts.Envelope{
				FactID:        "fact-blocking-stream-" + strconv.Itoa(i),
				ScopeID:       "wrong-scope",
				GenerationID:  "generation-blocking-stream",
				FactKind:      facts.TerraformStateResourceFactKind,
				StableFactKey: "resource:blocking-stream-" + strconv.Itoa(i),
				ObservedAt:    observedAt,
				Payload:       map[string]any{"index": float64(i)},
			}
		}
	}()
	return ch, done
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
