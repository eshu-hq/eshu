package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingServiceCatalogCorrelationStore struct {
	rows       []ServiceCatalogCorrelationRow
	lastFilter ServiceCatalogCorrelationFilter
}

func (s *recordingServiceCatalogCorrelationStore) ListServiceCatalogCorrelations(
	_ context.Context,
	filter ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	s.lastFilter = filter
	return append([]ServiceCatalogCorrelationRow(nil), s.rows...), nil
}

func TestServiceCatalogListCorrelationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &ServiceCatalogHandler{Correlations: &recordingServiceCatalogCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/service-catalog/correlations?limit=10",
		"/api/v0/service-catalog/correlations?repository_id=repo-api",
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

func TestServiceCatalogListCorrelationsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{
				CorrelationID:  "catalog-correlation-1",
				Provider:       "backstage",
				EntityRef:      "component:default/checkout",
				EntityType:     "component",
				DisplayName:    "Checkout",
				RepositoryID:   "repo-checkout",
				ServiceID:      "service-checkout",
				OwnerRef:       "group:default/payments",
				Lifecycle:      "production",
				Tier:           "tier_1",
				Outcome:        "exact",
				Reason:         "catalog repository annotation matched canonical repository identity",
				DriftKind:      "owner",
				DriftStatus:    "matches",
				ProvenanceOnly: false,
			},
			{CorrelationID: "catalog-correlation-2", EntityRef: "component:default/catalog-only"},
		},
	}
	handler := &ServiceCatalogHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=repo-checkout&owner_ref=group:default/payments&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo-checkout"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.OwnerRef, "group:default/payments"; got != want {
		t.Fatalf("OwnerRef = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Correlations []ServiceCatalogCorrelationResult `json:"correlations"`
		Count        int                               `json:"count"`
		Limit        int                               `json:"limit"`
		Truncated    bool                              `json:"truncated"`
		NextCursor   map[string]string                 `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 1; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Correlations[0].EntityRef, "component:default/checkout"; got != want {
		t.Fatalf("EntityRef = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "catalog-correlation-1"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
}

func TestServiceCatalogCorrelationQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'entity_ref' = $4",
		"fact.payload->>'repository_id' = $5",
		"fact.payload->>'owner_ref' = $8",
		"fact.payload->>'outcome' = $9",
	} {
		if !strings.Contains(listServiceCatalogCorrelationsQuery, want) {
			t.Fatalf("listServiceCatalogCorrelationsQuery missing %q:\n%s", want, listServiceCatalogCorrelationsQuery)
		}
	}
}
