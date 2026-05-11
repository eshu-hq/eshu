package reducer

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestWorkloadIdentityHandlerBuildsCanonicalWriteRequest(t *testing.T) {
	t.Parallel()

	writer := &recordingWorkloadIdentityWriter{
		result: WorkloadIdentityWriteResult{
			CanonicalID:      "canonical:workload/eshu",
			CanonicalWrites:  1,
			EvidenceSummary:  "canonical workload identity written",
			ReconciledScopes: 2,
		},
	}
	handler := WorkloadIdentityHandler{Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainWorkloadIdentity,
		Cause:        "shared identity follow-up required",
		EntityKeys: []string{
			"workload:eshu",
			"repo:eshu",
			"workload:eshu",
		},
		RelatedScopeIDs: []string{
			"scope-999",
			"scope-123",
			"scope-999",
		},
		EnqueuedAt:  time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt: time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:      IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(writer.requests), 1; got != want {
		t.Fatalf("writer request count = %d, want %d", got, want)
	}

	request := writer.requests[0]
	if got, want := request.IntentID, "intent-1"; got != want {
		t.Fatalf("request.IntentID = %q, want %q", got, want)
	}
	if got, want := request.ScopeID, "scope-123"; got != want {
		t.Fatalf("request.ScopeID = %q, want %q", got, want)
	}
	if got, want := request.GenerationID, "generation-456"; got != want {
		t.Fatalf("request.GenerationID = %q, want %q", got, want)
	}
	if got, want := request.SourceSystem, "git"; got != want {
		t.Fatalf("request.SourceSystem = %q, want %q", got, want)
	}
	if got, want := request.Cause, "shared identity follow-up required"; got != want {
		t.Fatalf("request.Cause = %q, want %q", got, want)
	}

	wantEntityKeys := []string{
		"repo:eshu",
		"workload:eshu",
	}
	if !slices.Equal(request.EntityKeys, wantEntityKeys) {
		t.Fatalf("request.EntityKeys = %v, want %v", request.EntityKeys, wantEntityKeys)
	}

	wantRelatedScopes := []string{
		"scope-123",
		"scope-999",
	}
	if !slices.Equal(request.RelatedScopeIDs, wantRelatedScopes) {
		t.Fatalf("request.RelatedScopeIDs = %v, want %v", request.RelatedScopeIDs, wantRelatedScopes)
	}

	if got, want := result.IntentID, "intent-1"; got != want {
		t.Fatalf("result.IntentID = %q, want %q", got, want)
	}
	if got, want := result.Domain, DomainWorkloadIdentity; got != want {
		t.Fatalf("result.Domain = %q, want %q", got, want)
	}
	if got, want := result.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("result.Status = %q, want %q", got, want)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.EvidenceSummary, "canonical workload identity written"; got != want {
		t.Fatalf("result.EvidenceSummary = %q, want %q", got, want)
	}
}

func TestWorkloadIdentityHandlerRejectsMissingEntityKeys(t *testing.T) {
	t.Parallel()

	writer := &recordingWorkloadIdentityWriter{}
	handler := WorkloadIdentityHandler{Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-2",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "shared identity follow-up required",
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
	if got, want := len(writer.requests), 0; got != want {
		t.Fatalf("writer request count = %d, want %d", got, want)
	}
}

func TestWorkloadIdentityHandlerPublishesServiceCanonicalPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := WorkloadIdentityHandler{
		Writer: &recordingWorkloadIdentityWriter{
			result: WorkloadIdentityWriteResult{CanonicalWrites: 1},
		},
		PhasePublisher: publisher,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-phase",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "shared identity follow-up required",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher call count = %d, want %d", got, want)
	}
	if got, want := len(publisher.calls[0]), 1; got != want {
		t.Fatalf("published state count = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceServiceUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("published phase = %q, want %q", got, want)
	}
}

func TestWorkloadIdentityHandlerRequiresCanonicalWriter(t *testing.T) {
	t.Parallel()

	handler := WorkloadIdentityHandler{}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-5",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "shared identity follow-up required",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
	if got, want := err.Error(), "workload identity writer is required"; got != want {
		t.Fatalf("Handle() error = %q, want %q", got, want)
	}
}

func TestNewDefaultRegistryRegistersImplementedDomainsOnly(t *testing.T) {
	t.Parallel()

	registry, err := NewDefaultRegistry(DefaultHandlers{
		WorkloadIdentityWriter: &recordingWorkloadIdentityWriter{
			result: WorkloadIdentityWriteResult{
				CanonicalID:     "canonical:workload/eshu",
				CanonicalWrites: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v, want nil", err)
	}

	workloadDefinition, ok := registry.Definition(DomainWorkloadIdentity)
	if !ok {
		t.Fatal("Definition(workload_identity) ok = false, want true")
	}

	workloadResult, err := workloadDefinition.Handler.Handle(context.Background(), Intent{
		IntentID:        "intent-3",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "shared identity follow-up required",
		EntityKeys:      []string{"workload:eshu"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("workload handler error = %v, want nil", err)
	}
	if got, want := workloadResult.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("workload handler status = %q, want %q", got, want)
	}

	governanceDefinition, ok := registry.Definition(DomainGovernance)
	if ok {
		t.Fatalf("Definition(governance) ok = true, want false with %+v", governanceDefinition)
	}
	if got, want := registry.SortedDomains(), []Domain{
		DomainCloudAssetResolution,
		DomainCodeCallMaterialization,
		DomainConfigStateDrift,
		DomainDeploymentMapping,
		DomainInheritanceMaterialization,
		DomainSemanticEntityMaterialization,
		DomainSQLRelationshipMaterialization,
		DomainWorkloadIdentity,
		DomainWorkloadMaterialization,
	}; !slices.Equal(got, want) {
		t.Fatalf("SortedDomains() = %v, want %v", got, want)
	}
}

type recordingWorkloadIdentityWriter struct {
	requests []WorkloadIdentityWrite
	result   WorkloadIdentityWriteResult
	err      error
}

func (w *recordingWorkloadIdentityWriter) WriteWorkloadIdentity(
	_ context.Context,
	request WorkloadIdentityWrite,
) (WorkloadIdentityWriteResult, error) {
	w.requests = append(w.requests, request)
	return w.result, w.err
}
