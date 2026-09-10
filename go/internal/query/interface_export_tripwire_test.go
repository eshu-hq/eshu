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

// --- codequery.HardcodedSecretInvestigator, codequery.SymbolContentSearcher,
// codequery.CodeTopicContentInvestigator ---
//
// These three tripwires moved with the CodeHandler family to
// internal/query/codequery (code_interface_export_tripwire_test.go) in the
// #6060 lane-A move: each drives an unexported CodeHandler method and
// request type that cannot be named from root once the family moves.

// --- pagedContentSearcher ---
//
// The paged-searcher tripwire moved with the ContentHandler family to
// internal/query/contentread (paged_searcher_tripwire_test.go) in the #6060
// lane-B1 move: it drives the handler's unexported searchFilesByScope and
// the unexported request type, neither of which can be named from root once
// the family moves. Root keeps the compile-time half
// (_ querycontract.PagedContentSearcher = (*ContentReader)(nil) in
// content_reader.go).

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
