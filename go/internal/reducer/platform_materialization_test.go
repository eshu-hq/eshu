package reducer

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestPlatformMaterializationHandlerBuildsCanonicalWriteRequest(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{
		result: PlatformMaterializationWriteResult{
			CanonicalID:     "platform:kubernetes:aws:prod-cluster:production:us-east-1",
			CanonicalWrites: 2,
			EvidenceSummary: "materialized platform binding for prod-cluster",
		},
	}
	handler := PlatformMaterializationHandler{Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-pm-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainDeploymentMapping,
		Cause:        "platform binding discovered",
		EntityKeys: []string{
			"platform:kubernetes:aws:prod-cluster:production:us-east-1",
			"repo:infra-eks",
			"platform:kubernetes:aws:prod-cluster:production:us-east-1",
		},
		RelatedScopeIDs: []string{
			"scope-999",
			"scope-123",
			"scope-999",
		},
		EnqueuedAt:  time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt: time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:      IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(writer.requests), 1; got != want {
		t.Fatalf("writer request count = %d, want %d", got, want)
	}

	request := writer.requests[0]
	if got, want := request.IntentID, "intent-pm-1"; got != want {
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
	if got, want := request.Cause, "platform binding discovered"; got != want {
		t.Fatalf("request.Cause = %q, want %q", got, want)
	}

	wantEntityKeys := []string{
		"platform:kubernetes:aws:prod-cluster:production:us-east-1",
		"repo:infra-eks",
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

	if got, want := result.IntentID, "intent-pm-1"; got != want {
		t.Fatalf("result.IntentID = %q, want %q", got, want)
	}
	if got, want := result.Domain, DomainDeploymentMapping; got != want {
		t.Fatalf("result.Domain = %q, want %q", got, want)
	}
	if got, want := result.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("result.Status = %q, want %q", got, want)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.EvidenceSummary, "materialized platform binding for prod-cluster"; got != want {
		t.Fatalf("result.EvidenceSummary = %q, want %q", got, want)
	}
}

func TestPlatformMaterializationHandlerDefaultEvidenceSummary(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{
		result: PlatformMaterializationWriteResult{
			CanonicalWrites: 1,
		},
	}
	handler := PlatformMaterializationHandler{Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-2",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform discovered",
		EntityKeys:      []string{"platform:ecs:aws:payments-cluster"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	wantSummary := "materialized 1 platform key(s) across 1 scope(s)"
	if got := result.EvidenceSummary; got != wantSummary {
		t.Fatalf("result.EvidenceSummary = %q, want %q", got, wantSummary)
	}
}

func TestPlatformMaterializationHandlerPublishesDeploymentMappingPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := PlatformMaterializationHandler{
		Writer: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{CanonicalWrites: 1},
		},
		PhasePublisher: publisher,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-phase",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform discovered",
		EntityKeys:      []string{"workload:service-edge-api"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher call count = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceServiceUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseDeploymentMapping; got != want {
		t.Fatalf("published phase = %q, want %q", got, want)
	}
}

func TestPlatformMaterializationHandlerRejectsMissingEntityKeys(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{}
	handler := PlatformMaterializationHandler{Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-3",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform binding discovered",
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
	if got, want := len(writer.requests), 0; got != want {
		t.Fatalf("writer request count = %d, want %d", got, want)
	}
}

func TestPlatformMaterializationHandlerRequiresCanonicalWriter(t *testing.T) {
	t.Parallel()

	handler := PlatformMaterializationHandler{}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-4",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform binding discovered",
		EntityKeys:      []string{"platform:kubernetes:aws:prod-cluster"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
	if got, want := err.Error(), "platform materialization writer is required"; got != want {
		t.Fatalf("Handle() error = %q, want %q", got, want)
	}
}

func TestPlatformMaterializationHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{}
	handler := PlatformMaterializationHandler{Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID: "intent-pm-5",
		Domain:   DomainWorkloadIdentity,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
}

func TestPlatformMaterializationHandlerCallsCrossRepoResolver(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{
		result: PlatformMaterializationWriteResult{
			CanonicalWrites: 1,
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}
	replayer := &recordingWorkloadMaterializationReplayer{}
	handler := PlatformMaterializationHandler{
		Writer:                          writer,
		WorkloadMaterializationReplayer: replayer,
		CrossRepoResolver: &CrossRepoRelationshipHandler{
			EvidenceLoader: &fakeEvidenceFactLoader{
				facts: []relationships.EvidenceFact{
					{
						EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
						RelationshipType: relationships.RelProvisionsDependencyFor,
						SourceRepoID:     "infra-repo",
						TargetRepoID:     "app-repo",
						Confidence:       0.99,
						Rationale:        "Terraform app_repo reference",
					},
				},
			},
			IntentWriter: intentWriter,
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-cross",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform discovered",
		EntityKeys:      []string{"platform:kubernetes:aws:prod-cluster", "repo:service-edge-api"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	// Platform write (1) + cross-repo edge write (1) = 2 total.
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}

	if len(intentWriter.rows) != 1 {
		t.Fatalf("expected 1 repo dependency intent write, got %d", len(intentWriter.rows))
	}
	if got, want := len(replayer.calls), 1; got != want {
		t.Fatalf("replayer calls = %d, want %d", got, want)
	}
	if got, want := replayer.calls[0].scopeID, "scope-1"; got != want {
		t.Fatalf("replayer scope_id = %q, want %q", got, want)
	}
	if got, want := replayer.calls[0].generationID, "gen-1"; got != want {
		t.Fatalf("replayer generation_id = %q, want %q", got, want)
	}
	if got, want := replayer.calls[0].entityKey, "repo:service-edge-api"; got != want {
		t.Fatalf("replayer entity_key = %q, want %q", got, want)
	}
}

func TestPlatformMaterializationHandlerSkipsCrossRepoWhenNilResolver(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{
		result: PlatformMaterializationWriteResult{CanonicalWrites: 1},
	}
	handler := PlatformMaterializationHandler{
		Writer:            writer,
		CrossRepoResolver: nil,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-no-cross",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform discovered",
		EntityKeys:      []string{"platform:ecs:aws:cluster"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}
}

func TestPlatformMaterializationHandlerDoesNotReplayWithoutCrossRepoWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{
		result: PlatformMaterializationWriteResult{CanonicalWrites: 1},
	}
	replayer := &recordingWorkloadMaterializationReplayer{}
	handler := PlatformMaterializationHandler{
		Writer:                          writer,
		WorkloadMaterializationReplayer: replayer,
		CrossRepoResolver: &CrossRepoRelationshipHandler{
			EvidenceLoader: &fakeEvidenceFactLoader{},
			IntentWriter:   &recordingRepoDependencyIntentWriter{},
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-no-replay",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform discovered",
		EntityKeys:      []string{"platform:ecs:aws:cluster"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(replayer.calls), 0; got != want {
		t.Fatalf("replayer calls = %d, want %d", got, want)
	}
}

func TestPlatformMaterializationHandlerReplaysWorkloadMaterializationAcrossRelatedScopes(t *testing.T) {
	t.Parallel()

	writer := &recordingPlatformMaterializationWriter{
		result: PlatformMaterializationWriteResult{CanonicalWrites: 1},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	replayer := &recordingWorkloadMaterializationReplayer{}
	handler := PlatformMaterializationHandler{
		Writer:                          writer,
		WorkloadMaterializationReplayer: replayer,
		CrossRepoResolver: &CrossRepoRelationshipHandler{
			EvidenceLoader: &fakeEvidenceFactLoader{
				facts: []relationships.EvidenceFact{
					{
						EvidenceKind:     relationships.EvidenceKindKustomizeResource,
						RelationshipType: relationships.RelDeploysFrom,
						SourceRepoID:     "deployment-kustomize",
						TargetRepoID:     "service-edge-api",
						Confidence:       0.99,
						Rationale:        "kustomize overlay references the application repo",
					},
				},
			},
			IntentWriter: intentWriter,
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-related-replay",
		ScopeID:         "scope-delivery",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "cross-repo deployment evidence resolved",
		EntityKeys:      []string{"platform:kubernetes:aws:modern-cluster:modern:us-east-1", "repo:service-edge-api"},
		RelatedScopeIDs: []string{"scope-app", "scope-delivery", "scope-app"},
		EnqueuedAt:      time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(replayer.calls), 2; got != want {
		t.Fatalf("replayer calls = %d, want %d", got, want)
	}
	if got, want := replayer.calls[0].scopeID, "scope-app"; got != want {
		t.Fatalf("replayer calls[0].scopeID = %q, want %q", got, want)
	}
	if got, want := replayer.calls[1].scopeID, "scope-delivery"; got != want {
		t.Fatalf("replayer calls[1].scopeID = %q, want %q", got, want)
	}
	for i, call := range replayer.calls {
		if got, want := call.entityKey, "repo:service-edge-api"; got != want {
			t.Fatalf("replayer calls[%d].entityKey = %q, want %q", i, got, want)
		}
		if got, want := call.generationID, "gen-1"; got != want {
			t.Fatalf("replayer calls[%d].generationID = %q, want %q", i, got, want)
		}
	}
}

func TestWorkloadMaterializationReplayEntityKeyPrefersRepositoryKey(t *testing.T) {
	t.Parallel()

	intent := Intent{
		EntityKeys: []string{
			"platform:kubernetes:aws:modern-cluster:modern:us-east-1",
			"repo:service-edge-api",
			"platform:ecs:aws:legacy-edge",
		},
		ScopeID: "scope-delivery",
	}

	if got, want := workloadMaterializationReplayEntityKey(intent), "repo:service-edge-api"; got != want {
		t.Fatalf("workloadMaterializationReplayEntityKey() = %q, want %q", got, want)
	}
}

type recordingPlatformMaterializationWriter struct {
	requests []PlatformMaterializationWrite
	result   PlatformMaterializationWriteResult
	err      error
}

func (w *recordingPlatformMaterializationWriter) WritePlatformMaterialization(
	_ context.Context,
	request PlatformMaterializationWrite,
) (PlatformMaterializationWriteResult, error) {
	w.requests = append(w.requests, request)
	return w.result, w.err
}

type recordingWorkloadMaterializationReplayer struct {
	calls []workloadMaterializationReplayCall
	err   error
}

type workloadMaterializationReplayCall struct {
	scopeID      string
	generationID string
	entityKey    string
}

func (r *recordingWorkloadMaterializationReplayer) ReplayWorkloadMaterialization(
	_ context.Context,
	scopeID string,
	generationID string,
	entityKey string,
) (bool, error) {
	r.calls = append(r.calls, workloadMaterializationReplayCall{
		scopeID:      scopeID,
		generationID: generationID,
		entityKey:    entityKey,
	})
	return true, r.err
}
