// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// This file holds the CodeHandler-specific cases of the #6060
// interface-export tripwire (originally root's interface_export_tripwire_test.go,
// which still carries the CloudInventoryHandler and DocumentationHandler
// cases -- those handlers stay in package query). Each fake proves the
// batched/authoritative path is actually taken rather than a silent
// fallback: before the interface-export precursor, exporting the method was
// the only thing standing between "the fast path runs" and "the fast path
// silently stops matching, with zero compile error." These tests catch a
// REGRESSION of that (an accidental rename, or the production wiring no
// longer passing a real content store).
//
// Every fake here embeds querytestutil.FakePortContentStore rather than
// root's fakePortContentStore adapter -- a _test.go symbol in another
// package is not importable, and this family now lives outside package
// query -- and adds only the one interface method under test, so `ok` in
// the production type assertion is exactly as narrow as it would be for a
// real *ContentReader.

// --- HardcodedSecretInvestigator ---

type fakeHardcodedSecretTripwireStore struct {
	querytestutil.FakePortContentStore
	calls int
}

func (s *fakeHardcodedSecretTripwireStore) InvestigateHardcodedSecrets(
	context.Context, HardcodedSecretInvestigationRequest,
) ([]HardcodedSecretFindingRow, error) {
	s.calls++
	return nil, nil
}

// TestCodeHandlerHardcodedSecretRowsUsesInvestigatorFastPath proves
// (h *CodeHandler).hardcodedSecretRows's h.Content.(HardcodedSecretInvestigator)
// assertion resolves to a real implementer rather than silently returning
// errHardcodedSecretBackendUnavailable.
func TestCodeHandlerHardcodedSecretRowsUsesInvestigatorFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeHardcodedSecretTripwireStore{}
	h := &CodeHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	if _, err := h.hardcodedSecretRows(context.Background(), HardcodedSecretInvestigationRequest{RepoID: "repo-a", Limit: 10}); err != nil {
		t.Fatalf("hardcodedSecretRows() error = %v, want nil", err)
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}

// --- SymbolContentSearcher ---

type fakeSymbolSearchTripwireStore struct {
	querytestutil.FakePortContentStore
	calls int
}

func (s *fakeSymbolSearchTripwireStore) SearchSymbols(
	context.Context, SymbolSearchRequest,
) ([]EntityContent, error) {
	s.calls++
	return nil, nil
}

// TestCodeHandlerSymbolSearchResultsUsesSearcherFastPath proves
// (h *CodeHandler).symbolSearchResults's h.Content.(SymbolContentSearcher)
// assertion resolves to a real implementer rather than silently falling
// back to the different-semantics SearchEntitiesByName name lookup (the
// #6060 audit finding: that fallback used to claim the same source_backend
// as this fast path).
func TestCodeHandlerSymbolSearchResultsUsesSearcherFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeSymbolSearchTripwireStore{}
	h := &CodeHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	_, source, _, err := h.symbolSearchResults(context.Background(), SymbolSearchRequest{Symbol: "Handle", RepoID: "repo-a"})
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

// --- CodeTopicContentInvestigator ---

type fakeCodeTopicTripwireStore struct {
	querytestutil.FakePortContentStore
	calls int
}

func (s *fakeCodeTopicTripwireStore) InvestigateCodeTopic(
	context.Context, CodeTopicInvestigationRequest,
) ([]CodeTopicEvidenceRow, error) {
	s.calls++
	return nil, nil
}

// TestCodeHandlerCodeTopicRowsUsesInvestigatorFastPath proves
// (h *CodeHandler).codeTopicRows's h.Content.(CodeTopicContentInvestigator)
// assertion resolves to a real implementer. CodeTopicContentInvestigator is
// also asserted from impact_change_surface_code.go's changeSurfaceTopicRows
// -- both call sites share this one interface and implementer, so either
// family moving away from the other silently breaks both.
func TestCodeHandlerCodeTopicRowsUsesInvestigatorFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakeCodeTopicTripwireStore{}
	h := &CodeHandler{Content: fake, Profile: ProfileLocalAuthoritative}
	if _, err := h.codeTopicRows(context.Background(), CodeTopicInvestigationRequest{Topic: "auth", RepoID: "repo-a", Limit: 10}); err != nil {
		t.Fatalf("codeTopicRows() error = %v, want nil", err)
	}
	if got, want := fake.calls, 1; got != want {
		t.Fatalf("calls = %d, want %d (fast path not taken)", got, want)
	}
}
