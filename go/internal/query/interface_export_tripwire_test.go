// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// This file is the permanent tripwire the #6060 interface-export precursor
// promised: for each of the 14 root interfaces that had an unexported
// method, a call-counting fake proves the batched/authoritative path is
// actually taken rather than a silent fallback. Before this PR, exporting
// the method was the only thing standing between "the fast path runs" and
// "the fast path silently stops matching, with zero compile error" -- these
// tests catch a REGRESSION of that (an accidental rename, or the production
// wiring no longer passing a *ContentReader), not the original defect class
// itself (a separate two-package reproduction proved that mechanic during
// the audit and is not part of this diff).
//
// Every fake here embeds fakePortContentStore (ports_test.go) and adds only
// the one interface method under test, so `ok` in the production type
// assertion is exactly as narrow as it would be for a real *ContentReader.

// --- cloudInventoryReadModelStore ---

type fakeCloudInventoryTripwireStore struct {
	fakePortContentStore
	identityCalls int
}

func (s *fakeCloudInventoryTripwireStore) CloudInventoryIdentities(
	context.Context, cloudInventoryFilter,
) (cloudInventoryListReadModel, error) {
	s.identityCalls++
	return cloudInventoryListReadModel{Resources: []map[string]any{{"cloud_resource_uid": "aws:acct:x"}}}, nil
}

func (s *fakeCloudInventoryTripwireStore) CloudInventoryPreRolloutEvidenceExists(
	context.Context, cloudInventoryFilter,
) (bool, error) {
	return false, nil
}

// TestCloudInventoryHandlerStoreUsesCloudInventoryIdentitiesFastPath proves
// (h *CloudInventoryHandler).store's h.Content.(cloudInventoryReadModelStore)
// assertion resolves to a real implementer rather than silently returning
// (nil, false) -- which would 501 every request behind a green build.
func TestCloudInventoryHandlerStoreUsesCloudInventoryIdentitiesFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeCloudInventoryTripwireStore{}
	h := &CloudInventoryHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	store, ok := h.store(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory", nil))
	if !ok {
		t.Fatal("h.store() ok = false, want true (fake satisfies cloudInventoryReadModelStore)")
	}
	if _, err := store.CloudInventoryIdentities(context.Background(), cloudInventoryFilter{}); err != nil {
		t.Fatalf("CloudInventoryIdentities() error = %v, want nil", err)
	}
	if got, want := fake.identityCalls, 1; got != want {
		t.Fatalf("identityCalls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- hardcodedSecretInvestigator ---

type fakeHardcodedSecretTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeHardcodedSecretTripwireStore) InvestigateHardcodedSecrets(
	context.Context, hardcodedSecretInvestigationRequest,
) ([]hardcodedSecretFindingRow, error) {
	s.calls++
	return nil, nil
}

// TestCodeHandlerHardcodedSecretRowsUsesInvestigatorFastPath proves
// (h *CodeHandler).hardcodedSecretRows's h.Content.(hardcodedSecretInvestigator)
// assertion resolves to a real implementer rather than silently returning
// errHardcodedSecretBackendUnavailable.
func TestCodeHandlerHardcodedSecretRowsUsesInvestigatorFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeHardcodedSecretTripwireStore{}
	h := &CodeHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	if _, err := h.hardcodedSecretRows(context.Background(), hardcodedSecretInvestigationRequest{RepoID: "repo-a", Limit: 10}); err != nil {
		t.Fatalf("hardcodedSecretRows() error = %v, want nil", err)
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- symbolContentSearcher ---

type fakeSymbolSearchTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeSymbolSearchTripwireStore) SearchSymbols(
	context.Context, symbolSearchRequest,
) ([]EntityContent, error) {
	s.calls++
	return nil, nil
}

// TestCodeHandlerSymbolSearchResultsUsesSearcherFastPath proves
// (h *CodeHandler).symbolSearchResults's h.Content.(symbolContentSearcher)
// assertion resolves to a real implementer rather than silently falling
// back to the different-semantics SearchEntitiesByName name lookup (the
// #6060 audit finding: that fallback used to claim the same source_backend
// as this fast path).
func TestCodeHandlerSymbolSearchResultsUsesSearcherFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeSymbolSearchTripwireStore{}
	h := &CodeHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	_, source, _, err := h.symbolSearchResults(context.Background(), symbolSearchRequest{Symbol: "Handle", RepoID: "repo-a"})
	if err != nil {
		t.Fatalf("symbolSearchResults() error = %v, want nil", err)
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
	if got, want := source, symbolSourceBackendContentStore; got != want {
		t.Fatalf("source_backend = %q, want %q", got, want)
	}
}

// --- codeTopicContentInvestigator ---

type fakeCodeTopicTripwireStore struct {
	fakePortContentStore
	calls int
}

func (s *fakeCodeTopicTripwireStore) InvestigateCodeTopic(
	context.Context, codeTopicInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	s.calls++
	return nil, nil
}

// TestCodeHandlerCodeTopicRowsUsesInvestigatorFastPath proves
// (h *CodeHandler).codeTopicRows's h.Content.(codeTopicContentInvestigator)
// assertion resolves to a real implementer. codeTopicContentInvestigator is
// also asserted from impact_change_surface_code.go's changeSurfaceTopicRows
// -- both call sites share this one interface and implementer, so either
// family moving away from the other silently breaks both.
func TestCodeHandlerCodeTopicRowsUsesInvestigatorFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeCodeTopicTripwireStore{}
	h := &CodeHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	if _, err := h.codeTopicRows(context.Background(), codeTopicInvestigationRequest{Topic: "auth", RepoID: "repo-a", Limit: 10}); err != nil {
		t.Fatalf("codeTopicRows() error = %v, want nil", err)
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- pagedContentSearcher ---

type fakePagedContentTripwireStore struct {
	fakePortContentStore
	fileCalls   int
	entityCalls int
}

func (s *fakePagedContentTripwireStore) SearchFiles(context.Context, contentSearchRequest) ([]FileContent, error) {
	s.fileCalls++
	return nil, nil
}

func (s *fakePagedContentTripwireStore) SearchEntities(context.Context, contentSearchRequest) ([]EntityContent, error) {
	s.entityCalls++
	return nil, nil
}

// TestContentHandlerSearchByScopeUsesPagedSearcherFastPath proves
// (h *ContentHandler).searchFilesByScope/searchEntitiesByScope's
// h.Content.(pagedContentSearcher) assertion resolves to a real implementer
// rather than the per-repo SearchFileContent/SearchEntityContent loop
// fallback. Both the interface and its only assertion site live inside
// content*.go, so unlike the other 13 this one needs no cross-family
// coordination -- it is self-contained regardless of any future contentread
// package move.
func TestContentHandlerSearchByScopeUsesPagedSearcherFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakePagedContentTripwireStore{}
	h := &ContentHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	ctx := ContextWithAuthContext(context.Background(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
	})
	req := contentSearchRequest{Pattern: "handle", RepoID: "repo-a", Limit: 10}

	if _, _, err := h.searchFilesByScope(ctx, req); err != nil {
		t.Fatalf("searchFilesByScope() error = %v, want nil", err)
	}
	if got, want := fake.fileCalls, 1; got != want {
		t.Fatalf("fileCalls = %d, want %d (fast path not taken)", got, want)
	}

	if _, _, err := h.searchEntitiesByScope(ctx, req); err != nil {
		t.Fatalf("searchEntitiesByScope() error = %v, want nil", err)
	}
	if got, want := fake.entityCalls, 1; got != want {
		t.Fatalf("entityCalls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- documentationReadModelStore ---

type fakeDocumentationTripwireStore struct {
	fakePortContentStore
	findingsCalls int
}

func (s *fakeDocumentationTripwireStore) DocumentationFindings(
	context.Context, documentationFindingFilter,
) (documentationFindingListReadModel, error) {
	s.findingsCalls++
	return documentationFindingListReadModel{}, nil
}

func (s *fakeDocumentationTripwireStore) DocumentationFacts(
	context.Context, documentationFactFilter,
) (documentationFactListReadModel, error) {
	return documentationFactListReadModel{}, nil
}

func (s *fakeDocumentationTripwireStore) DocumentationEvidencePacket(
	context.Context, string,
) (documentationEvidencePacketReadModel, error) {
	return documentationEvidencePacketReadModel{}, nil
}

func (s *fakeDocumentationTripwireStore) DocumentationEvidencePacketFreshness(
	context.Context, string, string,
) (documentationEvidencePacketFreshnessReadModel, error) {
	return documentationEvidencePacketFreshnessReadModel{}, nil
}

func (s *fakeDocumentationTripwireStore) DocumentationEvidencePacketWithFilter(
	context.Context, documentationEvidencePacketFilter,
) (documentationEvidencePacketReadModel, error) {
	return documentationEvidencePacketReadModel{}, nil
}

func (s *fakeDocumentationTripwireStore) DocumentationEvidencePacketFreshnessWithFilter(
	context.Context, documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessReadModel, error) {
	return documentationEvidencePacketFreshnessReadModel{}, nil
}

// TestDocumentationHandlerStoreUsesReadModelFastPath proves
// (h *DocumentationHandler).documentationStore's
// h.Content.(documentationReadModelStore) assertion resolves to a real
// implementer rather than silently 501ing every documentation route. The
// owning family for documentationReadModelStore isn't in #6060's 17-family
// list (flagged separately); this interface has the same cross-package
// fragility as the other 13 regardless.
func TestDocumentationHandlerStoreUsesReadModelFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeDocumentationTripwireStore{}
	h := &DocumentationHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	store, ok := h.documentationStore(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings", nil))
	if !ok {
		t.Fatal("h.documentationStore() ok = false, want true (fake satisfies documentationReadModelStore)")
	}
	if _, err := store.DocumentationFindings(context.Background(), documentationFindingFilter{}); err != nil {
		t.Fatalf("DocumentationFindings() error = %v, want nil", err)
	}
	if got, want := fake.findingsCalls, 1; got != want {
		t.Fatalf("findingsCalls = %d, want %d (fast path not taken)", got, want)
	}
}
