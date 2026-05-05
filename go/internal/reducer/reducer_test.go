package reducer

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

func TestIntentValidateAndScopeGenerationKey(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainWorkloadIdentity,
		Cause:        "projector emitted shared identity work",
		Priority:     10,
		EntityKeys:   []string{"workload:eshu"},
		RelatedScopeIDs: []string{
			"scope-999",
		},
		EnqueuedAt:  time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		AvailableAt: time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		Status:      IntentStatusPending,
	}

	if err := intent.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}

	if got, want := intent.ScopeGenerationKey(), "scope-123:generation-456"; got != want {
		t.Fatalf("ScopeGenerationKey() = %q, want %q", got, want)
	}
}

func TestDomainRegistryRequiresCrossSourceCrossScopeOwnership(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	definition := DomainDefinition{
		Domain:  DomainWorkloadIdentity,
		Summary: "resolve canonical workload identity across sources",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("workload_identity"),
	}

	if err := registry.Register(definition); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	got, ok := registry.Definition(DomainWorkloadIdentity)
	if !ok {
		t.Fatal("Definition() ok = false, want true")
	}

	if !got.Ownership.CrossSource || !got.Ownership.CrossScope {
		t.Fatalf("Definition().Ownership = %#v, want cross-source and cross-scope", got.Ownership)
	}

	if err := registry.Register(DomainDefinition{
		Domain:  DomainCloudAssetResolution,
		Summary: "bad domain ownership",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     false,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("cloud_asset"),
	}); err == nil {
		t.Fatal("Register() error = nil, want non-nil for non-cross-scope ownership")
	}
}

func TestRuntimeRunOnceIsBoundedAndProcessesRegisteredHandlers(t *testing.T) {
	t.Parallel()

	var handled []string
	registry := NewRegistry()
	if err := registry.Register(DomainDefinition{
		Domain:  DomainWorkloadIdentity,
		Summary: "resolve canonical workload identity across sources",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("workload_identity"),
		Handler: HandlerFunc(func(context.Context, Intent) (Result, error) {
			handled = append(handled, "workload-identity")
			return Result{
				Status:          ResultStatusSucceeded,
				EvidenceSummary: "canonical identity written",
				CanonicalWrites: 1,
			}, nil
		}),
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	runtime, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v, want nil", err)
	}

	first := Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainWorkloadIdentity,
		Cause:        "projector emitted shared identity work",
		Priority:     5,
		EntityKeys:   []string{"workload:eshu"},
		RelatedScopeIDs: []string{
			"scope-999",
		},
		EnqueuedAt:  time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		AvailableAt: time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		Status:      IntentStatusPending,
	}
	second := first
	second.IntentID = "intent-2"
	second.Cause = "second pass"
	second.Priority = 5
	second.EnqueuedAt = first.EnqueuedAt.Add(time.Minute)
	second.AvailableAt = second.EnqueuedAt

	if _, err := runtime.Enqueue(first.EnqueuedAt, first); err != nil {
		t.Fatalf("Enqueue(first) error = %v, want nil", err)
	}
	if _, err := runtime.Enqueue(second.EnqueuedAt, second); err != nil {
		t.Fatalf("Enqueue(second) error = %v, want nil", err)
	}

	report, err := runtime.RunOnce(context.Background(), second.EnqueuedAt.Add(time.Minute), 1)
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}

	if got, want := report.Processed, 1; got != want {
		t.Fatalf("RunOnce().Processed = %d, want %d", got, want)
	}
	if got, want := report.Succeeded, 1; got != want {
		t.Fatalf("RunOnce().Succeeded = %d, want %d", got, want)
	}
	if got, want := len(handled), 1; got != want {
		t.Fatalf("handled intents = %d, want %d", got, want)
	}

	stats := runtime.Stats()
	if got, want := stats.Pending, 1; got != want {
		t.Fatalf("Stats().Pending = %d, want %d", got, want)
	}
	if got, want := stats.Succeeded, 1; got != want {
		t.Fatalf("Stats().Succeeded = %d, want %d", got, want)
	}
	if got, want := stats.Domains[0].Domain, DomainWorkloadIdentity; got != want {
		t.Fatalf("Stats().Domains[0].Domain = %q, want %q", got, want)
	}
	if got, want := stats.Domains[0].Pending, 1; got != want {
		t.Fatalf("Stats().Domains[0].Pending = %d, want %d", got, want)
	}

	report, err = runtime.RunOnce(context.Background(), second.EnqueuedAt.Add(2*time.Minute), 1)
	if err != nil {
		t.Fatalf("RunOnce(second) error = %v, want nil", err)
	}
	if got, want := report.Processed, 1; got != want {
		t.Fatalf("RunOnce(second).Processed = %d, want %d", got, want)
	}
	if got, want := len(handled), 2; got != want {
		t.Fatalf("handled intents = %d, want %d", got, want)
	}
}

func TestObservabilityContractUsesReducerSpans(t *testing.T) {
	t.Parallel()

	got := ObservabilityContract()

	wantSpans := []string{
		telemetry.SpanReducerIntentEnqueue,
		telemetry.SpanReducerRun,
		telemetry.SpanCanonicalWrite,
	}

	if !slices.Equal(got.SpanNames, wantSpans) {
		t.Fatalf("ObservabilityContract().SpanNames = %v, want %v", got.SpanNames, wantSpans)
	}

	if !slices.Equal(got.MetricDimensions, telemetry.MetricDimensionKeys()) {
		t.Fatalf("ObservabilityContract().MetricDimensions = %v, want %v", got.MetricDimensions, telemetry.MetricDimensionKeys())
	}

	if !slices.Equal(got.LogKeys, telemetry.LogKeys()) {
		t.Fatalf("ObservabilityContract().LogKeys = %v, want %v", got.LogKeys, telemetry.LogKeys())
	}
}

func TestRuntimeStatsSummarizeDomainBacklog(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.Register(DomainDefinition{
		Domain:  DomainDeploymentMapping,
		Summary: "map deployments across sources",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testTruthContract("deployment_mapping"),
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	runtime, err := NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v, want nil", err)
	}

	_, err = runtime.Enqueue(time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC), Intent{
		IntentID:     "intent-3",
		ScopeID:      "scope-abc",
		GenerationID: "generation-def",
		SourceSystem: "git",
		Domain:       DomainDeploymentMapping,
		Cause:        "projector emitted deployment mapping work",
		EntityKeys:   []string{"deployment:api"},
		RelatedScopeIDs: []string{
			"scope-external",
		},
		Status:      IntentStatusPending,
		EnqueuedAt:  time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
		AvailableAt: time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}

	stats := runtime.Stats()
	if got, want := stats.Domains[0].Ownership.CrossSource, true; got != want {
		t.Fatalf("Stats().Domains[0].Ownership.CrossSource = %v, want %v", got, want)
	}
	if got, want := stats.Domains[0].Ownership.CrossScope, true; got != want {
		t.Fatalf("Stats().Domains[0].Ownership.CrossScope = %v, want %v", got, want)
	}
	if got, want := stats.Domains[0].Pending, 1; got != want {
		t.Fatalf("Stats().Domains[0].Pending = %d, want %d", got, want)
	}
	if got, want := stats.Queued, 1; got != want {
		t.Fatalf("Stats().Queued = %d, want %d", got, want)
	}
}

func testTruthContract(canonicalKind string) truth.Contract {
	return truth.Contract{
		CanonicalKind: canonicalKind,
		SourceLayers: []truth.Layer{
			truth.LayerSourceDeclaration,
		},
	}
}
