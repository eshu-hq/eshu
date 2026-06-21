package query

import (
	"encoding/json"
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
