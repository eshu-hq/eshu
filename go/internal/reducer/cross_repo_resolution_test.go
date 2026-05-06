package reducer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// --- Test doubles ---

type fakeEvidenceFactLoader struct {
	facts []relationships.EvidenceFact
	err   error
	calls int
}

func (f *fakeEvidenceFactLoader) ListEvidenceFacts(_ context.Context, _ string) ([]relationships.EvidenceFact, error) {
	f.calls++
	return f.facts, f.err
}

type fakeAssertionLoader struct {
	assertions []relationships.Assertion
	err        error
}

func (f *fakeAssertionLoader) ListAssertions(_ context.Context, _ *relationships.RelationshipType) ([]relationships.Assertion, error) {
	return f.assertions, f.err
}

type fakeResolutionPersister struct {
	candidates           []relationships.Candidate
	resolved             []relationships.ResolvedRelationship
	activatedGenerations []string
}

func (f *fakeResolutionPersister) UpsertCandidates(_ context.Context, _ string, candidates []relationships.Candidate) error {
	f.candidates = append(f.candidates, candidates...)
	return nil
}

func (f *fakeResolutionPersister) UpsertResolved(_ context.Context, _ string, resolved []relationships.ResolvedRelationship) error {
	f.resolved = append(f.resolved, resolved...)
	return nil
}

func (f *fakeResolutionPersister) ActivateResolutionGeneration(_ context.Context, generationID, _ string) error {
	f.activatedGenerations = append(f.activatedGenerations, generationID)
	return nil
}

type recordingRepoDependencyIntentWriter struct {
	rows [][]SharedProjectionIntentRow
}

func (r *recordingRepoDependencyIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	r.rows = append(r.rows, append([]SharedProjectionIntentRow(nil), rows...))
	return nil
}

// --- Tests ---

func TestCrossRepoResolutionSkipsWhenNoEvidence(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: nil},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Resolve() = %d, want 0", count)
	}
	if len(intentWriter.rows) != 0 {
		t.Fatalf("expected 0 intent writes, got %d", len(intentWriter.rows))
	}
}

func TestCrossRepoResolutionGatesUntilBackwardEvidenceCommitted(t *testing.T) {
	t.Parallel()

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: evidenceLoader,
		IntentWriter:   intentWriter,
		ReadinessLookup: func(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
			if got, want := key.ScopeID, "scope-1"; got != want {
				t.Fatalf("ScopeID = %q, want %q", got, want)
			}
			if got, want := key.AcceptanceUnitID, "scope-1"; got != want {
				t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
			}
			if got, want := key.SourceRunID, "gen-1"; got != want {
				t.Fatalf("SourceRunID = %q, want %q", got, want)
			}
			if got, want := key.GenerationID, "gen-1"; got != want {
				t.Fatalf("GenerationID = %q, want %q", got, want)
			}
			if got, want := key.Keyspace, GraphProjectionKeyspaceCrossRepoEvidence; got != want {
				t.Fatalf("Keyspace = %q, want %q", got, want)
			}
			if got, want := phase, GraphProjectionPhaseBackwardEvidenceCommitted; got != want {
				t.Fatalf("phase = %q, want %q", got, want)
			}
			return false, false
		},
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Resolve() = %d, want 0 when readiness is missing", count)
	}
	if evidenceLoader.calls != 0 {
		t.Fatalf("evidence loader calls = %d, want 0 when gated", evidenceLoader.calls)
	}
	if len(intentWriter.rows) != 0 {
		t.Fatalf("intent writes = %d, want 0 when gated", len(intentWriter.rows))
	}
}

func TestCrossRepoResolutionUsesReadinessPrefetchWhenAvailable(t *testing.T) {
	t.Parallel()

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	prefetchCalls := 0
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: evidenceLoader,
		IntentWriter:   intentWriter,
		ReadinessLookup: func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
			t.Fatal("ReadinessLookup should be replaced by prefetched lookup")
			return false, false
		},
		ReadinessPrefetch: func(_ context.Context, keys []GraphProjectionPhaseKey, phase GraphProjectionPhase) (GraphProjectionReadinessLookup, error) {
			prefetchCalls++
			if got, want := phase, GraphProjectionPhaseBackwardEvidenceCommitted; got != want {
				t.Fatalf("phase = %q, want %q", got, want)
			}
			if len(keys) != 1 {
				t.Fatalf("prefetch keys = %d, want 1", len(keys))
			}
			key := keys[0]
			if got, want := key.ScopeID, "scope-1"; got != want {
				t.Fatalf("ScopeID = %q, want %q", got, want)
			}
			if got, want := key.AcceptanceUnitID, "scope-1"; got != want {
				t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
			}
			if got, want := key.SourceRunID, "gen-1"; got != want {
				t.Fatalf("SourceRunID = %q, want %q", got, want)
			}
			if got, want := key.GenerationID, "gen-1"; got != want {
				t.Fatalf("GenerationID = %q, want %q", got, want)
			}
			if got, want := key.Keyspace, GraphProjectionKeyspaceCrossRepoEvidence; got != want {
				t.Fatalf("Keyspace = %q, want %q", got, want)
			}
			return func(lookupKey GraphProjectionPhaseKey, lookupPhase GraphProjectionPhase) (bool, bool) {
				if lookupKey != key {
					t.Fatalf("lookup key = %#v, want %#v", lookupKey, key)
				}
				if lookupPhase != phase {
					t.Fatalf("lookup phase = %q, want %q", lookupPhase, phase)
				}
				return true, true
			}, nil
		},
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1 when readiness is committed", count)
	}
	if prefetchCalls != 1 {
		t.Fatalf("prefetch calls = %d, want 1", prefetchCalls)
	}
	if evidenceLoader.calls != 1 {
		t.Fatalf("evidence loader calls = %d, want 1 after readiness", evidenceLoader.calls)
	}
	if len(intentWriter.rows) != 1 {
		t.Fatalf("intent writes = %d, want 1", len(intentWriter.rows))
	}
}

func TestCrossRepoResolutionWarnsWhenReadinessGateBypassed(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(previous)

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: evidenceLoader,
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1 when gate wiring is absent", count)
	}
	if !bytes.Contains(logs.Bytes(), []byte("cross-repo readiness lookup not configured")) {
		t.Fatalf("warning log = %q, want missing readiness warning", logs.String())
	}
}

func TestCrossRepoResolutionSkipsWhenNilLoader(t *testing.T) {
	t.Parallel()

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: nil,
		IntentWriter:   &recordingRepoDependencyIntentWriter{},
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Resolve() = %d, want 0", count)
	}
}

func TestCrossRepoResolutionSkipsWhenNilIntentWriter(t *testing.T) {
	t.Parallel()

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{
			facts: []relationships.EvidenceFact{
				{
					EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
					RelationshipType: relationships.RelProvisionsDependencyFor,
					SourceRepoID:     "infra-repo",
					TargetRepoID:     "app-repo",
					Confidence:       0.99,
				},
			},
		},
		IntentWriter: nil,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Resolve() = %d, want 0", count)
	}
}

func TestCrossRepoResolutionPropagatesEvidenceLoaderError(t *testing.T) {
	t.Parallel()

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{err: fmt.Errorf("db timeout")},
		IntentWriter:   &recordingRepoDependencyIntentWriter{},
	}

	_, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err == nil {
		t.Fatal("Resolve() error = nil, want non-nil")
	}
}

func TestCrossRepoResolutionWritesDependencyEdges(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at the target repository",
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}
	persister := &fakeResolutionPersister{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
		Persister:      persister,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1", count)
	}

	if len(intentWriter.rows) != 1 {
		t.Fatalf("expected 1 intent write, got %d", len(intentWriter.rows))
	}
	if len(intentWriter.rows[0]) != 1 {
		t.Fatalf("expected 1 intent row, got %d", len(intentWriter.rows[0]))
	}

	row := intentWriter.rows[0][0]
	if got := row.Payload["repo_id"]; got != "infra-repo" {
		t.Fatalf("row repo_id = %v, want infra-repo", got)
	}
	if got := row.Payload["target_repo_id"]; got != "app-repo" {
		t.Fatalf("row target_repo_id = %v, want app-repo", got)
	}
	if got, want := row.Payload["evidence_type"], "terraform_app_repo"; got != want {
		t.Fatalf("row evidence_type = %v, want %q", got, want)
	}
	if got, want := row.ScopeID, "scope-1"; got != want {
		t.Fatalf("row ScopeID = %q, want %q", got, want)
	}
	if got, want := row.SourceRunID, crossRepoContributionSourceRunID("scope-1"); got != want {
		t.Fatalf("row SourceRunID = %q, want %q", got, want)
	}

	// Verify generation was activated for downstream consumers.
	if len(persister.activatedGenerations) != 1 {
		t.Fatalf("activatedGenerations count = %d, want 1", len(persister.activatedGenerations))
	}
	if got, want := persister.activatedGenerations[0], "gen-1"; got != want {
		t.Fatalf("activatedGenerations[0] = %q, want %q", got, want)
	}
}

func TestCrossRepoResolutionNormalizesScopedRepositoryIDs(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "git-repository-scope:repository:r_infra",
			TargetRepoID:     "repository:r_app",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at the target repository",
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}
	persister := &fakeResolutionPersister{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
		Persister:      persister,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1", count)
	}
	if len(intentWriter.rows) != 1 || len(intentWriter.rows[0]) != 1 {
		t.Fatalf("unexpected intent writes: %#v", intentWriter.rows)
	}
	if got, want := intentWriter.rows[0][0].Payload["repo_id"], "repository:r_infra"; got != want {
		t.Fatalf("write repo_id = %v, want %q", got, want)
	}
	if got, want := intentWriter.rows[0][0].Payload["target_repo_id"], "repository:r_app"; got != want {
		t.Fatalf("write target_repo_id = %v, want %q", got, want)
	}
	if len(persister.resolved) != 1 {
		t.Fatalf("persisted resolved = %d, want 1", len(persister.resolved))
	}
	if got, want := persister.resolved[0].SourceRepoID, "repository:r_infra"; got != want {
		t.Fatalf("persisted source repo = %q, want %q", got, want)
	}
}

func TestCrossRepoResolutionPersistsAuditTrail(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at target",
		},
	}

	persister := &fakeResolutionPersister{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   &recordingRepoDependencyIntentWriter{},
		Persister:      persister,
	}

	_, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if len(persister.candidates) == 0 {
		t.Fatal("expected candidates to be persisted")
	}
	if len(persister.resolved) == 0 {
		t.Fatal("expected resolved relationships to be persisted")
	}
}

func TestCrossRepoResolutionRespectsAssertionRejections(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at target",
		},
	}

	assertions := []relationships.Assertion{
		{
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			RelationshipType: relationships.RelProvisionsDependencyFor,
			Decision:         "reject",
			Reason:           "false positive",
			Actor:            "admin",
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		Assertions:     &fakeAssertionLoader{assertions: assertions},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Resolve() = %d, want 0 (rejected by assertion)", count)
	}
}

func TestCrossRepoResolutionFiltersLowConfidence(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformConfigPath,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.50, // below threshold
			Rationale:        "low confidence match",
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("Resolve() = %d, want 0 (below confidence threshold)", count)
	}
}

func TestCrossRepoResolutionMultipleRelationshipTypes(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo-1",
			Confidence:       0.99,
			Rationale:        "terraform app_repo",
		},
		{
			EvidenceKind:     relationships.EvidenceKindHelmChart,
			RelationshipType: relationships.RelDeploysFrom,
			SourceRepoID:     "deploy-repo",
			TargetRepoID:     "app-repo-2",
			Confidence:       0.90,
			Rationale:        "helm chart reference",
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("Resolve() = %d, want 2", count)
	}

	if len(intentWriter.rows) != 1 {
		t.Fatalf("expected 1 intent write, got %d", len(intentWriter.rows))
	}
	if len(intentWriter.rows[0]) != 2 {
		t.Fatalf("expected 2 intent rows, got %d", len(intentWriter.rows[0]))
	}
}

func TestCrossRepoResolutionPreservesGitHubActionsTypedRelationships(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindGitHubActionsReusableWorkflow,
			RelationshipType: relationships.RelDeploysFrom,
			SourceRepoID:     "repo-service",
			TargetRepoID:     "repo-automation",
			Confidence:       0.93,
		},
		{
			EvidenceKind:     relationships.EvidenceKindGitHubActionsWorkflowInputRepository,
			RelationshipType: relationships.RelDiscoversConfigIn,
			SourceRepoID:     "repo-service",
			TargetRepoID:     "repo-automation",
			Confidence:       0.90,
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-gha", "gen-gha")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("Resolve() = %d, want 2", count)
	}

	if len(intentWriter.rows) != 1 {
		t.Fatalf("expected 1 intent write, got %d", len(intentWriter.rows))
	}
	rows := intentWriter.rows[0]
	if len(rows) != 2 {
		t.Fatalf("expected 2 write rows, got %d", len(rows))
	}

	gotTypes := map[string]struct{}{}
	for _, row := range rows {
		gotTypes[stringValue(row.Payload["relationship_type"])] = struct{}{}
		if got := stringValue(row.Payload["repo_id"]); got != "repo-service" {
			t.Fatalf("row repo_id = %q, want %q", got, "repo-service")
		}
		if got := stringValue(row.Payload["target_repo_id"]); got != "repo-automation" {
			t.Fatalf("row target_repo_id = %q, want %q", got, "repo-automation")
		}
		if got := stringValue(row.Payload["generation_id"]); got != "gen-gha" {
			t.Fatalf("row generation_id = %q, want %q", got, "gen-gha")
		}
		if got := stringValue(row.Payload["resolved_id"]); got == "" {
			t.Fatalf("row missing resolved_id: %#v", row.Payload)
		}
		if got := stringValue(row.Payload["resolution_source"]); got != string(relationships.ResolutionSourceInferred) {
			t.Fatalf("row resolution_source = %q, want %q", got, relationships.ResolutionSourceInferred)
		}
		if got := intValue(row.Payload["evidence_count"]); got != 1 {
			t.Fatalf("row evidence_count = %d, want 1", got)
		}
		if got := stringSliceValue(row.Payload["evidence_kinds"]); len(got) != 1 {
			t.Fatalf("row evidence_kinds = %#v, want one kind", got)
		}
	}
	for _, want := range []string{string(relationships.RelDeploysFrom), string(relationships.RelDiscoversConfigIn)} {
		if _, ok := gotTypes[want]; !ok {
			t.Fatalf("missing relationship_type %q in write rows", want)
		}
	}
}

func TestCrossRepoResolutionDeduplicatesEvidence(t *testing.T) {
	t.Parallel()

	// Same evidence duplicated — should produce only 1 resolved edge.
	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at target",
		},
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at target",
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1 (deduped)", count)
	}
}

func TestBuildResolvedEdgeIntentRowsCarriesEvidenceArtifactsForGraphStory(t *testing.T) {
	t.Parallel()

	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.9,
			EvidenceCount:    2,
			Rationale:        "deployment config references service repository",
			ResolutionSource: relationships.ResolutionSourceInferred,
			Details: map[string]any{
				"evidence_kinds": []string{
					string(relationships.EvidenceKindHelmValues),
					string(relationships.EvidenceKindKustomizeResource),
				},
				"evidence_preview": []map[string]any{
					{
						"kind":       string(relationships.EvidenceKindHelmValues),
						"confidence": 0.84,
						"details": map[string]any{
							"path":          "argocd/service-api/overlays/prod/values.yaml",
							"extractor":     "helm",
							"matched_alias": "service-api",
							"matched_value": "registry.example.test/service-api",
						},
					},
				},
			},
		},
	}

	rows, _ := buildResolvedEdgeIntentRows(resolved, "scope-1", "source-run-1", "gen-1")
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	artifacts := mapSliceValueForTest(rows[0].Payload["evidence_artifacts"])
	if got, want := len(artifacts), 1; got != want {
		t.Fatalf("len(evidence_artifacts) = %d, want %d: %#v", got, want, rows[0].Payload)
	}
	artifact := artifacts[0]
	for key, want := range map[string]string{
		"evidence_kind":   string(relationships.EvidenceKindHelmValues),
		"artifact_family": "helm",
		"path":            "argocd/service-api/overlays/prod/values.yaml",
		"extractor":       "helm",
		"environment":     "prod",
		"matched_alias":   "service-api",
		"matched_value":   "registry.example.test/service-api",
	} {
		if got := stringValue(artifact[key]); got != want {
			t.Fatalf("artifact[%s] = %q, want %q; artifact=%#v", key, got, want, artifact)
		}
	}
	if got := floatValueForTest(artifact["confidence"]); got != 0.84 {
		t.Fatalf("artifact confidence = %v, want 0.84", got)
	}
}

func intValue(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func floatValueForTest(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func mapSliceValueForTest(v any) []map[string]any {
	switch value := v.(type) {
	case []map[string]any:
		return value
	case []any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func stringSliceValue(v any) []string {
	switch value := v.(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
