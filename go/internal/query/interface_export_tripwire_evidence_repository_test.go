// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Continues interface_export_tripwire_test.go's #6060 permanent tripwire for
// the remaining 8 unexported-method interfaces.
//
// evidenceCitationFileStore and semanticEvidenceStore are NOT duplicated
// here: evidence_citation_test.go's
// TestEvidenceHandlerBuildEvidenceCitationsPacketFromFileAndEntityHandles
// already asserts store.fileBatchCalls == 1 and
// store.singleFileLineCalls+store.singleEntityCalls == 0 (the batched path
// ran, the per-file fallback did not), and semantic_evidence_test.go already
// asserts store.calls == 1 against fakeSemanticEvidenceStore. Both are the
// same tripwire shape as the tests below; see this PR's description for the
// break/restore proof run against both.

// --- relationshipEvidenceReadModelStore ---

type fakeRelationshipEvidenceTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeRelationshipEvidenceTripwireStore) RelationshipEvidenceByResolvedID(
	context.Context, string,
) (relationshipEvidenceReadModel, error) {
	s.calls++
	return relationshipEvidenceReadModel{
		Available: true,
		Row: map[string]any{
			"resolved_id":       "resolved-1",
			"relationship_type": "DEPLOYS_FROM",
			"confidence":        0.9,
			"source":            map[string]any{"repo_id": "repo-a"},
			"target":            map[string]any{"repo_id": "repo-b"},
		},
	}, nil
}

// TestEvidenceHandlerGetRelationshipEvidenceUsesReadModelFastPath proves
// (h *EvidenceHandler).getRelationshipEvidence's
// h.Content.(relationshipEvidenceReadModelStore) assertion resolves to a
// real implementer rather than silently 501ing.
func TestEvidenceHandlerGetRelationshipEvidenceUsesReadModelFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeRelationshipEvidenceTripwireStore{}
	h := &EvidenceHandler{Content: fake, Profile: ProfileProduction}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-1", nil)
	req.SetPathValue("resolved_id", "resolved-1")
	w := httptest.NewRecorder()

	h.getRelationshipEvidence(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- repositoryDeploymentEvidenceReadModelStore ---

type fakeRepositoryDeploymentEvidenceTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeRepositoryDeploymentEvidenceTripwireStore) RepositoryDeploymentEvidence(
	context.Context, string,
) (repositoryDeploymentEvidenceReadModel, error) {
	s.calls++
	return repositoryDeploymentEvidenceReadModel{Available: true, Rows: []map[string]any{{"artifact_id": "a1"}}}, nil
}

// TestLoadRepositoryDeploymentEvidenceUsesReadModelFastPath proves
// loadRepositoryDeploymentEvidence's
// content.(repositoryDeploymentEvidenceReadModelStore) assertion resolves to
// a real implementer rather than silently falling back to the Neo4j
// queryRepoDeploymentEvidence graph traversal.
func TestLoadRepositoryDeploymentEvidenceUsesReadModelFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeRepositoryDeploymentEvidenceTripwireStore{}
	result, err := loadRepositoryDeploymentEvidence(context.Background(), fake, "repo-a")
	if err != nil {
		t.Fatalf("loadRepositoryDeploymentEvidence() error = %v, want nil", err)
	}
	if len(result) == 0 {
		t.Fatal("loadRepositoryDeploymentEvidence() returned no rows, want the fake's fast-path row")
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- repositoryEntryPointReadModelStore ---

type fakeRepositoryEntryPointTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeRepositoryEntryPointTripwireStore) RepositoryEntryPoints(
	context.Context, string,
) (repositoryEntryPointReadModel, error) {
	s.calls++
	return repositoryEntryPointReadModel{Available: true, Rows: []map[string]any{{"name": "main"}}}, nil
}

// TestLoadRepositoryEntryPointsUsesReadModelFastPath proves
// loadRepositoryEntryPoints's content.(repositoryEntryPointReadModelStore)
// assertion resolves to a real implementer rather than silently falling
// back to the Neo4j well-known-entry-point-name Cypher scan.
func TestLoadRepositoryEntryPointsUsesReadModelFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeRepositoryEntryPointTripwireStore{}
	result := loadRepositoryEntryPoints(context.Background(), fake, "repo-a")
	if len(result) == 0 {
		t.Fatal("loadRepositoryEntryPoints() returned no rows, want the fake's fast-path row")
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- repositoryReadModelSummaryStore ---

type fakeRepositoryReadModelSummaryTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeRepositoryReadModelSummaryTripwireStore) RepositoryReadModelSummary(
	context.Context, string,
) (RepositoryReadModelSummary, error) {
	s.calls++
	return RepositoryReadModelSummary{Available: true, PlatformCount: 3}, nil
}

// TestLoadRepositoryReadModelSummaryUsesReadModelFastPath proves
// loadRepositoryReadModelSummary's content.(repositoryReadModelSummaryStore)
// assertion resolves to a real implementer rather than silently falling
// back to per-field Neo4j counts in queryRepositoryContextCounts.
func TestLoadRepositoryReadModelSummaryUsesReadModelFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeRepositoryReadModelSummaryTripwireStore{}
	summary := loadRepositoryReadModelSummary(context.Background(), fake, "repo-a")
	if summary == nil {
		t.Fatal("loadRepositoryReadModelSummary() = nil, want the fake's fast-path summary")
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- repositoryRelationshipReadModelStore ---

type fakeRepositoryRelationshipReadModelTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeRepositoryRelationshipReadModelTripwireStore) RepositoryRelationshipReadModel(
	context.Context, string,
) (RepositoryRelationshipReadModel, error) {
	s.calls++
	return RepositoryRelationshipReadModel{Available: true, Relationships: []map[string]any{{"type": "DEPENDS_ON"}}}, nil
}

// TestLoadRepositoryRelationshipReadModelUsesReadModelFastPath proves
// loadRepositoryRelationshipReadModel's
// content.(repositoryRelationshipReadModelStore) assertion resolves to a
// real implementer rather than silently falling back to the three separate
// Neo4j queries (queryRepoDependencies, queryRepoRelationshipOverview,
// queryRepoConsumers) repository_context.go uses when this read model is
// unavailable.
func TestLoadRepositoryRelationshipReadModelUsesReadModelFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeRepositoryRelationshipReadModelTripwireStore{}
	readModel := loadRepositoryRelationshipReadModel(context.Background(), fake, "repo-a")
	if readModel == nil {
		t.Fatal("loadRepositoryRelationshipReadModel() = nil, want the fake's fast-path read model")
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- serviceStoryTargetSupportStore ---

type fakeServiceStoryTargetSupportTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeServiceStoryTargetSupportTripwireStore) ServiceStoryTargetSupportEvidence(
	context.Context, serviceStoryTargetSupportFilter,
) (serviceStoryTargetSupportReadModel, error) {
	s.calls++
	return serviceStoryTargetSupportReadModel{Support: map[string]any{"evidence_count": 1}}, nil
}

// TestLoadServiceStoryTargetSupportUsesReadModelFastPath proves
// loadServiceStoryTargetSupport's content.(serviceStoryTargetSupportStore)
// assertion resolves to a real implementer. This is the worst fallback the
// #6060 audit found: with no implementer, loadServiceStoryTargetSupport and
// loadRepositoryStoryTargetSupport both return (nil, nil) with NO fallback
// of any kind -- the target_support/support_overview response sections just
// silently vanish.
func TestLoadServiceStoryTargetSupportUsesReadModelFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeServiceStoryTargetSupportTripwireStore{}
	support, err := loadServiceStoryTargetSupport(context.Background(), fake, map[string]any{"id": "svc-a", "repo_id": "repo-a"})
	if err != nil {
		t.Fatalf("loadServiceStoryTargetSupport() error = %v, want nil", err)
	}
	if len(support) == 0 {
		t.Fatal("loadServiceStoryTargetSupport() returned nothing, want the fake's fast-path support map")
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}
