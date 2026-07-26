// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mustMarshalPackageRegistryCorrelationPayload is a small test helper that
// JSON-encodes a fixture payload map, failing the test on error.
func mustMarshalPackageRegistryCorrelationPayload(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return out
}

// TestBuildPackageRegistryCorrelationPageDerivesTruncationFromFetchedFactsNotDecodedRows
// is the codex P1 finding regression test on PR #5816 (package_registry_correlations.go:157).
// Root cause: the pre-fix ListPackageRegistryCorrelations fetched limit+1
// facts, decoded the WHOLE window, and dropped a malformed/unsupported-version
// fact with a bare `continue` -- shrinking the decoded slice below the raw
// fetch count. listCorrelations and listDependencyChains then computed
// truncated from len(decodedRows) > limit, so a malformed fact anywhere in
// the window could make a genuinely truncated page report truncated=false
// with no cursor, hiding every valid fact beyond it.
//
// This test fetches a window of 4 facts for a requested visible limit of 3
// (fetchLimit=4): fact-1 (valid, consumption), fact-2 (valid, consumption),
// fact-3 (malformed: missing the required package_id identity field -- the
// LAST fact of the visible window, so a naive "last decoded row" cursor would
// be wrong), fact-4 (valid, the "+1" lookahead fact beyond the page). It
// asserts:
//   - Truncated is true (4 fetched > 3 requested), even though only 2 of the
//     3 visible-window facts decoded.
//   - NextCursorCorrelationID is "fact-3" -- the last FETCHED fact in the
//     visible window -- not "fact-2" (the last successfully DECODED row) and
//     not "fact-4" (the lookahead fact, which must never appear as a cursor
//     or a row in this page).
//   - fact-4 never appears in Rows: the lookahead fact must not leak into the
//     visible page just because an earlier fact in the window was dropped.
func TestBuildPackageRegistryCorrelationPageDerivesTruncationFromFetchedFactsNotDecodedRows(t *testing.T) {
	t.Parallel()

	facts := []packageRegistryCorrelationFactRow{
		{
			FactID:        "fact-1",
			FactKind:      packageConsumptionCorrelationFactKind,
			SchemaVersion: "1.0.0",
			Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
				"package_id":        "pkg:npm://registry.example/a",
				"relationship_kind": "consumption",
				"repository_id":     "repo-consumer",
			}),
		},
		{
			FactID:        "fact-2",
			FactKind:      packageConsumptionCorrelationFactKind,
			SchemaVersion: "1.0.0",
			Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
				"package_id":        "pkg:npm://registry.example/b",
				"relationship_kind": "consumption",
				"repository_id":     "repo-consumer",
			}),
		},
		{
			FactID:        "fact-3",
			FactKind:      packageConsumptionCorrelationFactKind,
			SchemaVersion: "1.0.0",
			Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
				// package_id absent: this fact must dead-letter (drop), never
				// zero-value, per decodePackageRegistryCorrelationRow's
				// required-identity-field contract.
				"relationship_kind": "consumption",
				"repository_id":     "repo-consumer",
			}),
		},
		{
			FactID:        "fact-4",
			FactKind:      packageConsumptionCorrelationFactKind,
			SchemaVersion: "1.0.0",
			Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
				"package_id":        "pkg:npm://registry.example/d",
				"relationship_kind": "consumption",
				"repository_id":     "repo-consumer",
			}),
		},
	}

	// fetchLimit=4 means the caller requested a visible page of 3
	// (fetchLimit-1) and the store fetched one extra lookahead fact, matching
	// PostgresPackageRegistryCorrelationStore's "+1" convention.
	page, err := buildPackageRegistryCorrelationPage(facts, 4)
	if err != nil {
		t.Fatalf("buildPackageRegistryCorrelationPage: %v", err)
	}

	if !page.Truncated {
		t.Fatal("Truncated = false, want true (4 facts fetched for a 3-fact page)")
	}
	if got, want := page.NextCursorCorrelationID, "fact-3"; got != want {
		t.Fatalf("NextCursorCorrelationID = %q, want %q (last FETCHED fact in the visible window, not the last decoded row)", got, want)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2 (fact-1 and fact-2; fact-3 dropped, fact-4 beyond the page): %+v", len(page.Rows), page.Rows)
	}
	for _, row := range page.Rows {
		if row.PackageID == "pkg:npm://registry.example/d" {
			t.Fatalf("lookahead fact-4 leaked into the visible page: %+v", row)
		}
	}
}

// TestBuildPackageRegistryCorrelationPageNotTruncatedKeepsAllDecodedRows proves
// the unexceptional case is unaffected: when the fetch does not exceed the
// visible window, every fact is in scope and nothing is trimmed.
func TestBuildPackageRegistryCorrelationPageNotTruncatedKeepsAllDecodedRows(t *testing.T) {
	t.Parallel()

	facts := []packageRegistryCorrelationFactRow{
		{
			FactID:        "fact-1",
			FactKind:      packageConsumptionCorrelationFactKind,
			SchemaVersion: "1.0.0",
			Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
				"package_id":        "pkg:npm://registry.example/a",
				"relationship_kind": "consumption",
				"repository_id":     "repo-consumer",
			}),
		},
	}

	page, err := buildPackageRegistryCorrelationPage(facts, 4)
	if err != nil {
		t.Fatalf("buildPackageRegistryCorrelationPage: %v", err)
	}

	if page.Truncated {
		t.Fatal("Truncated = true, want false (only 1 of 3 requested facts exists)")
	}
	if page.NextCursorCorrelationID != "" {
		t.Fatalf("NextCursorCorrelationID = %q, want empty when not truncated", page.NextCursorCorrelationID)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(page.Rows))
	}
}

// rawFactPackageRegistryCorrelationStore is a PackageRegistryCorrelationStore
// fake that stores raw (undecoded) facts and calls the real
// buildPackageRegistryCorrelationPage, exercising the actual production
// pagination logic end-to-end through the HTTP handlers rather than a
// hand-built stand-in. It routes on filter.RepositoryID so a
// listDependencyChains phase-1 (consumption, RepositoryID set) request and a
// phase-2 (batched publisher, RepositoryID empty) request each see their own
// fixed fact set, mirroring fakeChainCorrelationStore's routing.
type rawFactPackageRegistryCorrelationStore struct {
	consumptionFacts []packageRegistryCorrelationFactRow
	publisherFacts   []packageRegistryCorrelationFactRow
}

func (s *rawFactPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) (PackageRegistryCorrelationPage, error) {
	if filter.RepositoryID != "" {
		return buildPackageRegistryCorrelationPage(s.consumptionFacts, filter.Limit+1)
	}
	return buildPackageRegistryCorrelationPage(s.publisherFacts, filter.Limit+1)
}

// TestPackageRegistryListCorrelationsHandlerAdvancesPastMalformedFactInsideWindow
// is the codex P1 regression test at the HTTP handler level for
// GET /api/v0/package-registry/correlations: a malformed fact inside the fetch
// window must not make a genuinely-truncated page report itself complete. It
// exercises the real handler, the real store contract
// (buildPackageRegistryCorrelationPage), and the real typed-decode drop path
// together.
func TestPackageRegistryListCorrelationsHandlerAdvancesPastMalformedFactInsideWindow(t *testing.T) {
	t.Parallel()

	store := &rawFactPackageRegistryCorrelationStore{
		consumptionFacts: []packageRegistryCorrelationFactRow{
			{
				FactID:        "fact-1",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/a",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-2",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/b",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-3",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					// missing package_id: dead-letters.
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-4",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/d",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
		},
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/correlations?repository_id=repo-consumer&limit=3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Count      int               `json:"count"`
		Truncated  bool              `json:"truncated"`
		NextCursor map[string]string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true: a malformed fact inside the window must not hide remaining correlations")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "fact-3"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
	if got, want := resp.Count, 2; got != want {
		t.Fatalf("count = %d, want %d (fact-1 and fact-2; fact-3 dropped, fact-4 beyond the page)", got, want)
	}
}

// TestPackageRegistryDependencyChainsHandlerAdvancesPastMalformedConsumptionFactInsideWindow
// is the listDependencyChains sibling of the test above: the codex finding
// named both listCorrelations and listDependencyChains as affected, since both
// compute truncated/cursor from a ListPackageRegistryCorrelations result. No
// publisher facts are configured, so every chain terminates without a
// publisher leg -- the point of this test is proving the consumption-phase
// pagination fix, not dependency-chain join behavior (already covered by
// package_registry_dependency_chains_test.go).
func TestPackageRegistryDependencyChainsHandlerAdvancesPastMalformedConsumptionFactInsideWindow(t *testing.T) {
	t.Parallel()

	store := &rawFactPackageRegistryCorrelationStore{
		consumptionFacts: []packageRegistryCorrelationFactRow{
			{
				FactID:        "fact-1",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/a",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-2",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/b",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-3",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					// missing package_id: dead-letters.
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-4",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/d",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
		},
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Count      int               `json:"count"`
		Truncated  bool              `json:"truncated"`
		NextCursor map[string]string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true: a malformed consumption fact inside the window must not hide remaining chains")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "fact-3"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
	if got, want := resp.Count, 2; got != want {
		t.Fatalf("count = %d, want %d (chains for fact-1 and fact-2; fact-3 dropped, fact-4 beyond the page)", got, want)
	}
}

// TestPackageRegistryDependencyChainsHandlerForwardsPaginationWhenEveryConsumptionFactInWindowFailsDecode
// is the ALL-dropped sibling of the tests above: it pins the #5461/#5816
// finding that ResolvePackageDependencyChains's len(consumptionPage.Rows) == 0
// early return (package_registry_dependency_chains.go) forwards Truncated and
// NextCursorCorrelationID from the raw consumption fetch rather than the
// pre-fix behavior of returning a bare empty page and losing both. Both
// fact-1 and fact-2 -- the entire visible window -- carry a
// valid package_id but an unsupported schema major (SchemaVersion "2.0.0"),
// so buildPackageRegistryCorrelationPage's typed decode drops both and
// consumptionPage.Rows is empty even though the raw fetch found two facts
// plus a lookahead fact-3 beyond the page. This exercises the real handler,
// the real store contract, and the real early-return path together, so it
// would fail if that early return ever again forwarded a bare empty page
// instead of the raw fetch's Truncated/NextCursorCorrelationID.
func TestPackageRegistryDependencyChainsHandlerForwardsPaginationWhenEveryConsumptionFactInWindowFailsDecode(t *testing.T) {
	t.Parallel()

	store := &rawFactPackageRegistryCorrelationStore{
		consumptionFacts: []packageRegistryCorrelationFactRow{
			{
				FactID:        "fact-1",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "2.0.0", // unsupported major: dead-letters, never a hard error
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/a",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-2",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "2.0.0", // unsupported major: dead-letters, never a hard error
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/b",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
			{
				FactID:        "fact-3",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					// The "+1" lookahead fact beyond the requested 2-fact page;
					// its own decodability is irrelevant since it is never
					// decoded or placed in the visible window.
					"package_id":        "pkg:npm://registry.example/c",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
		},
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Chains              []map[string]any  `json:"chains"`
		Count               int               `json:"count"`
		Truncated           bool              `json:"truncated"`
		PublishersTruncated bool              `json:"publishers_truncated"`
		NextCursor          map[string]string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Chains == nil {
		t.Fatal("chains = null, want an empty array")
	}
	if got, want := resp.Count, 0; got != want {
		t.Fatalf("count = %d, want %d (fact-1 and fact-2 both failed typed decode)", got, want)
	}
	if len(resp.Chains) != 0 {
		t.Fatalf("chains = %#v, want empty", resp.Chains)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true: the raw fetch found fact-1, fact-2, and the fact-3 lookahead beyond the 2-fact page, even though every visible-window fact failed decode")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "fact-2"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q (last FETCHED fact in the visible window, not a decoded row -- there are none)", got, want)
	}
	if resp.PublishersTruncated {
		t.Fatal("publishers_truncated = true, want false: no package ids survived decode, so the phase-2 publisher read is never attempted")
	}
}
