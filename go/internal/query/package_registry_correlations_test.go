package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingPackageRegistryCorrelationStore struct {
	rows       []PackageRegistryCorrelationRow
	lastFilter PackageRegistryCorrelationFilter
}

func (s *recordingPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	s.lastFilter = filter
	return append([]PackageRegistryCorrelationRow(nil), s.rows...), nil
}

func TestPackageRegistryListCorrelationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Correlations: &recordingPackageRegistryCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/package-registry/correlations?limit=10",
		"/api/v0/package-registry/correlations?package_id=pkg:npm://registry.example/team-api",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestPackageRegistryListCorrelationsUsesBoundedPostgresStore(t *testing.T) {
	t.Parallel()

	store := &recordingPackageRegistryCorrelationStore{
		rows: []PackageRegistryCorrelationRow{
			{
				CorrelationID:    "correlation-1",
				RelationshipKind: "consumption",
				PackageID:        "pkg:npm://registry.example/team-api",
				RepositoryID:     "repo-web",
				RepositoryName:   "web",
				RelativePath:     "package.json",
				DependencyRange:  "^1.2.0",
				Outcome:          "manifest_declared",
				Reason:           "git manifest dependency matches package registry identity",
				CanonicalWrites:  1,
			},
			{CorrelationID: "correlation-2", RelationshipKind: "ownership"},
		},
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/package-registry/correlations?package_id=pkg:npm://registry.example/team-api&relationship_kind=consumption&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.PackageID, "pkg:npm://registry.example/team-api"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.RelationshipKind, "consumption"; got != want {
		t.Fatalf("RelationshipKind = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Correlations []PackageRegistryCorrelationResult `json:"correlations"`
		Count        int                                `json:"count"`
		Limit        int                                `json:"limit"`
		Truncated    bool                               `json:"truncated"`
		NextCursor   map[string]string                  `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 1; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Correlations[0].RepositoryID, "repo-web"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "correlation-1"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
}

func TestPackageRegistryCorrelationQueryExcludesTombstones(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listPackageRegistryCorrelationsQuery, "fact.is_tombstone = FALSE") {
		t.Fatalf("listPackageRegistryCorrelationsQuery must exclude tombstone facts:\n%s", listPackageRegistryCorrelationsQuery)
	}
}
