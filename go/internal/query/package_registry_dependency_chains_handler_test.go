package query

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPackageRegistryDependencyChainsRequiresRepository(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Correlations: &recordingPackageRegistryCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependency-chains?limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

// TestPackageRegistryDependencyChainsNextCursorRoundTrip verifies that when the
// handler truncates results it emits a next_cursor with after_correlation_id,
// and that a follow-up request carrying that cursor passes it through to the
// consumption read (keyset pagination).
func TestPackageRegistryDependencyChainsNextCursorRoundTrip(t *testing.T) {
	t.Parallel()

	// Return limit+1 rows so the handler sees truncation.
	consumption := make([]PackageRegistryCorrelationRow, 0, 3)
	for i, pkgID := range []string{"pkg:npm://registry.example/a", "pkg:npm://registry.example/b", "pkg:npm://registry.example/c"} {
		consumption = append(consumption, PackageRegistryCorrelationRow{
			CorrelationID:    fmt.Sprintf("consume-%d", i+1),
			RelationshipKind: "consumption",
			PackageID:        pkgID,
			RepositoryID:     "repo-consumer",
			CanonicalWrites:  1,
		})
	}
	store := &fakeChainCorrelationStore{consumption: consumption}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// First page: limit=2, expect 2 results + next_cursor.
	req1 := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=2",
		nil,
	)
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)
	if got, want := w1.Code, http.StatusOK; got != want {
		t.Fatalf("page1 status = %d, want %d; body = %s", got, want, w1.Body.String())
	}
	var page1 struct {
		Count      int    `json:"count"`
		Truncated  bool   `json:"truncated"`
		NextCursor *struct {
			AfterCorrelationID string `json:"after_correlation_id"`
		} `json:"next_cursor"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &page1); err != nil {
		t.Fatalf("json.Unmarshal page1: %v; body = %s", err, w1.Body.String())
	}
	if !page1.Truncated {
		t.Fatal("page1 must be truncated (3 rows, limit=2)")
	}
	if page1.NextCursor == nil || page1.NextCursor.AfterCorrelationID == "" {
		t.Fatalf("page1 must include next_cursor.after_correlation_id; got next_cursor=%v", page1.NextCursor)
	}
	cursor := page1.NextCursor.AfterCorrelationID

	// Reset call log; issue second page with the cursor.
	store.calls = nil
	req2 := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=2&after_correlation_id="+cursor,
		nil,
	)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if got, want := w2.Code, http.StatusOK; got != want {
		t.Fatalf("page2 status = %d, want %d; body = %s", got, want, w2.Body.String())
	}

	// Verify the cursor was threaded to the phase-1 consumption read.
	if len(store.calls) == 0 {
		t.Fatal("page2 must issue at least one store call")
	}
	if got := store.calls[0].AfterCorrelationID; got != cursor {
		t.Fatalf("phase-1 AfterCorrelationID = %q, want %q (cursor must be threaded through)", got, cursor)
	}
}

func TestPackageRegistryDependencyChainsSurfacesLabeledChain(t *testing.T) {
	t.Parallel()

	store := &fakeChainCorrelationStore{
		consumption: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "consume-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/team-api",
				PackageName:      "@acme/team-api",
				Ecosystem:        "npm",
				RepositoryID:     "repo-consumer",
				RepositoryName:   "consumer-app",
				DependencyRange:  "^1.2.0",
				ProvenanceOnly:   false,
				CanonicalWrites:  1,
			},
		},
		publication: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "publish-1",
				RelationshipKind: "publication",
				PackageID:        "pkg:npm://registry.example/team-api",
				RepositoryID:     "repo-publisher",
				RepositoryName:   "team-api",
				ProvenanceOnly:   true,
				CanonicalWrites:  0,
			},
		},
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=50",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Chains []struct {
			ConsumerRepositoryID      string `json:"consumer_repository_id"`
			PackageID                 string `json:"package_id"`
			ConsumptionProvenanceOnly bool   `json:"consumption_provenance_only"`
			Ambiguous                 bool   `json:"ambiguous"`
			Publishers                []struct {
				RepositoryID   string `json:"repository_id"`
				ProvenanceOnly bool   `json:"provenance_only"`
			} `json:"publishers"`
		} `json:"chains"`
		RepositoryID string `json:"repository_id"`
		Count        int    `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := resp.RepositoryID, "repo-consumer"; got != want {
		t.Fatalf("repository_id = %q, want %q", got, want)
	}
	chain := resp.Chains[0]
	if chain.ConsumptionProvenanceOnly {
		t.Fatal("consumption leg must be canonical (provenance_only=false)")
	}
	if len(chain.Publishers) != 1 {
		t.Fatalf("len(publishers) = %d, want 1", len(chain.Publishers))
	}
	if !chain.Publishers[0].ProvenanceOnly {
		t.Fatal("publisher leg must carry provenance_only=true (inferred, not exact)")
	}
	if chain.Publishers[0].RepositoryID != "repo-publisher" {
		t.Fatalf("publisher repository_id = %q, want repo-publisher", chain.Publishers[0].RepositoryID)
	}
}
