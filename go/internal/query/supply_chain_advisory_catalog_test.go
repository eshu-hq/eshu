package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingAdvisoryCatalogStore struct {
	page       AdvisoryCatalogPage
	lastFilter AdvisoryCatalogFilter
	calls      int
}

func (s *recordingAdvisoryCatalogStore) ListAdvisoryCatalog(
	_ context.Context,
	filter AdvisoryCatalogFilter,
) (AdvisoryCatalogPage, error) {
	s.calls++
	s.lastFilter = filter
	return AdvisoryCatalogPage{Rows: append([]AdvisoryCatalogRow(nil), s.page.Rows...)}, nil
}

func TestSupplyChainListAdvisoryCatalogRequiresLimit(t *testing.T) {
	t.Parallel()

	store := &recordingAdvisoryCatalogStore{}
	handler := &SupplyChainHandler{AdvisoryCatalog: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/advisories", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.calls != 0 {
		t.Fatalf("store called %d times, want 0 for missing limit", store.calls)
	}
}

func TestSupplyChainListAdvisoryCatalogRejectsLimitOutOfRange(t *testing.T) {
	t.Parallel()

	store := &recordingAdvisoryCatalogStore{}
	handler := &SupplyChainHandler{AdvisoryCatalog: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/advisories?limit=0",
		"/api/v0/supply-chain/advisories?limit=201",
		"/api/v0/supply-chain/advisories?limit=abc",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("%s status = %d, want %d", target, got, want)
		}
	}
}

func TestSupplyChainListAdvisoryCatalogRejectsBadKEVAndCursor(t *testing.T) {
	t.Parallel()

	store := &recordingAdvisoryCatalogStore{}
	handler := &SupplyChainHandler{AdvisoryCatalog: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/advisories?limit=10&kev=maybe",
		"/api/v0/supply-chain/advisories?limit=10&after_cvss=9.8",
		"/api/v0/supply-chain/advisories?limit=10&after_advisory_key=CVE-2021-44228",
		"/api/v0/supply-chain/advisories?limit=10&after_cvss=high&after_advisory_key=CVE-2021-44228",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("%s status = %d, want %d; body=%s", target, got, want, w.Body.String())
		}
	}
}

func TestSupplyChainListAdvisoryCatalogReturnsBackendUnavailable(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/advisories?limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSupplyChainListAdvisoryCatalogPassesFiltersAndPaginates(t *testing.T) {
	t.Parallel()

	store := &recordingAdvisoryCatalogStore{
		page: AdvisoryCatalogPage{Rows: []AdvisoryCatalogRow{
			{
				AdvisoryKey:   "CVE-2021-44228",
				CanonicalID:   "CVE-2021-44228",
				CVEID:         "CVE-2021-44228",
				SeverityLabel: "CRITICAL",
				CVSSScore:     10.0,
				KEV:           true,
				Ecosystems:    []string{"maven"},
				Sources:       []string{"nvd"},
			},
			{
				AdvisoryKey:   "CVE-2021-45046",
				CanonicalID:   "CVE-2021-45046",
				CVEID:         "CVE-2021-45046",
				SeverityLabel: "CRITICAL",
				CVSSScore:     9.0,
				KEV:           false,
			},
		}},
	}
	handler := &SupplyChainHandler{AdvisoryCatalog: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/advisories?limit=1&severity=critical&ecosystem=maven&kev=true&q=CVE-2021",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	if got, want := store.lastFilter.Severity, "critical"; got != want {
		t.Fatalf("Severity = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Ecosystem, "maven"; got != want {
		t.Fatalf("Ecosystem = %q, want %q", got, want)
	}
	if !store.lastFilter.KEVOnly {
		t.Fatal("KEVOnly = false, want true")
	}
	if got, want := store.lastFilter.Query, "CVE-2021"; got != want {
		t.Fatalf("Query = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d (page+1)", got, want)
	}

	var resp struct {
		Advisories []AdvisoryCatalogRow `json:"advisories"`
		Count      int                  `json:"count"`
		Limit      int                  `json:"limit"`
		Truncated  bool                 `json:"truncated"`
		NextCursor map[string]any       `json:"next_cursor"`
		Scope      map[string]any       `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Advisories), 1; got != want {
		t.Fatalf("len(advisories) = %d, want %d", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_advisory_key"], "CVE-2021-44228"; got != want {
		t.Fatalf("next_cursor.after_advisory_key = %v, want %v", got, want)
	}
	if got, want := resp.NextCursor["after_cvss"].(float64), 10.0; got != want {
		t.Fatalf("next_cursor.after_cvss = %v, want %v", got, want)
	}
	if resp.Scope["kev"] != true {
		t.Fatalf("scope.kev = %v, want true", resp.Scope["kev"])
	}
}

func TestSupplyChainListAdvisoryCatalogAcceptsCursor(t *testing.T) {
	t.Parallel()

	store := &recordingAdvisoryCatalogStore{}
	handler := &SupplyChainHandler{AdvisoryCatalog: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/advisories?limit=5&after_cvss=9.8&after_advisory_key=cve-2021-44228",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.AfterCVSS, 9.8; got != want {
		t.Fatalf("AfterCVSS = %v, want %v", got, want)
	}
	// The store normalizes the cursor key to canonical upper-case form.
	if got, want := store.lastFilter.AfterAdvisoryKey, "cve-2021-44228"; got != want {
		t.Fatalf("handler AfterAdvisoryKey = %q, want %q (store normalizes)", got, want)
	}
}

func TestNormalizeAdvisoryCatalogFilterUppercasesCursorKey(t *testing.T) {
	t.Parallel()

	got := normalizeAdvisoryCatalogFilter(AdvisoryCatalogFilter{
		Severity:         " HIGH ",
		Ecosystem:        " npm ",
		Query:            " cve-2021 ",
		AfterAdvisoryKey: " cve-2021-44228 ",
	})
	if got.Severity != "HIGH" {
		t.Fatalf("Severity = %q, want trimmed HIGH", got.Severity)
	}
	if got.Ecosystem != "npm" {
		t.Fatalf("Ecosystem = %q, want trimmed npm", got.Ecosystem)
	}
	if got.Query != "cve-2021" {
		t.Fatalf("Query = %q, want trimmed query", got.Query)
	}
	if got.AfterAdvisoryKey != "CVE-2021-44228" {
		t.Fatalf("AfterAdvisoryKey = %q, want canonical upper-case key", got.AfterAdvisoryKey)
	}
}

func TestPostgresAdvisoryCatalogStoreRejectsPaginationLimit(t *testing.T) {
	t.Parallel()

	store := NewPostgresAdvisoryCatalogStore(unusedAdvisoryEvidenceQueryer{})
	_, err := store.ListAdvisoryCatalog(context.Background(), AdvisoryCatalogFilter{
		Limit: advisoryCatalogMaxLimit + 2,
	})
	if err == nil {
		t.Fatal("ListAdvisoryCatalog() error = nil, want pagination limit error")
	}
	want := "limit must be between 1 and"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err.Error(), want)
	}
}

func TestPostgresAdvisoryCatalogStoreRequiresDB(t *testing.T) {
	t.Parallel()

	store := PostgresAdvisoryCatalogStore{}
	_, err := store.ListAdvisoryCatalog(context.Background(), AdvisoryCatalogFilter{Limit: 10})
	if err == nil {
		t.Fatal("ListAdvisoryCatalog() error = nil, want missing-db error")
	}
}

func TestOpenAPISpecIncludesAdvisoryCatalog(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/advisories")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listAdvisoryCatalog"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	names := map[string]bool{}
	for _, parameter := range parameters {
		parameterMap, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		names[parameterMap["name"].(string)] = true
	}
	for _, want := range []string{"severity", "ecosystem", "kev", "q", "after_cvss", "after_advisory_key", "limit"} {
		if !names[want] {
			t.Fatalf("catalog parameters missing %q", want)
		}
	}
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	advisories := mustMapField(t, properties, "advisories")
	items := mustMapField(t, advisories, "items")
	itemProperties := mustMapField(t, items, "properties")
	for _, want := range []string{"advisory_key", "canonical_id", "severity_label", "cvss_score", "kev", "ecosystems", "package_ids"} {
		if _, ok := itemProperties[want]; !ok {
			t.Fatalf("catalog advisory schema missing %q", want)
		}
	}
}

func TestAdvisoryCatalogQueryUsesActiveSourceFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"'vulnerability.cve'",
		"'vulnerability.affected_package'",
		"'vulnerability.known_exploited'",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"ORDER BY cvss_score DESC, advisory_key ASC",
		"LIMIT $7",
	} {
		if !strings.Contains(listAdvisoryCatalogQuery, want) {
			t.Fatalf("listAdvisoryCatalogQuery missing %q:\n%s", want, listAdvisoryCatalogQuery)
		}
	}
}

// TestAdvisoryCatalogQueryUsesBoundedSinglePassShape pins the #3389 catalog
// reshape. The catalog aggregates the three vulnerability fact kinds as per-kind
// UNION ALL legs feeding a single GROUP BY (no per-kind aggregate CTEs joined
// back together), because the previous shape joined two whole-fact-kind
// aggregates on a computed advisory_key the planner estimates at one row. That
// misestimate collapsed the rollup joins into an O(active_facts^2) nested-loop
// left join that does not complete within a 600s statement timeout at ~250k
// vulnerability facts. The #3402 per-fact_kind active_scan partial indexes bound
// the per-kind scans but cannot bound that join; removing the join is what keeps
// the catalog bounded as the advisory count grows.
func TestAdvisoryCatalogQueryUsesBoundedSinglePassShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		// Per-kind legs unioned into one input, then one grouped pass.
		"UNION ALL",
		"GROUP BY advisory_key",
		// Spine identity preserved: an advisory still requires a cve fact.
		"HAVING bool_or(fact_kind = 'vulnerability.cve')",
		// Per-kind rollups are FILTERed aggregates over the single group.
		"FILTER (WHERE fact_kind = 'vulnerability.cve')",
		"FILTER (WHERE fact_kind = 'vulnerability.affected_package')",
	} {
		if !strings.Contains(listAdvisoryCatalogQuery, want) {
			t.Fatalf("listAdvisoryCatalogQuery missing bounded-shape marker %q:\n%s", want, listAdvisoryCatalogQuery)
		}
	}

	// Regression guard: the old nested-loop-prone shape must be gone. The
	// MATERIALIZED CTEs hid cardinality from the planner and the separate
	// affected_rollup/kev aggregates were LEFT JOINed back to the catalog spine.
	for _, banned := range []string{
		"AS MATERIALIZED",
		"LEFT JOIN affected_rollup",
		"LEFT JOIN kev",
	} {
		if strings.Contains(listAdvisoryCatalogQuery, banned) {
			t.Fatalf("listAdvisoryCatalogQuery still contains unbounded-shape construct %q:\n%s", banned, listAdvisoryCatalogQuery)
		}
	}
}

// TestAdvisoryCatalogQueryKeepsPerFactKindActiveScanAnchor pins the bounded
// per-fact-kind scan shape that #3389 relies on. Each vulnerability fact kind is
// read by its own UNION ALL leg, so each must keep its single-fact_kind
// predicate plus `is_tombstone = FALSE` and the active-generation join. Those are
// exactly the columns the partial indexes
// fact_records_vulnerability_cve_active_scan_idx and
// fact_records_vulnerability_known_exploited_active_scan_idx are built on (the
// index DDL/presence is pinned in
// go/internal/storage/postgres/facts_active_supply_chain_impact_test.go). If a
// later edit folds these legs onto a multi-kind predicate or drops the active
// filter, the planner can no longer bound the scan to one kind's active rows and
// the whole-table scan regression from #3389 returns.
func TestAdvisoryCatalogQueryKeepsPerFactKindActiveScanAnchor(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		// Catalog spine: bounded to exactly the active vulnerability.cve tuples.
		"WHERE fact.fact_kind = 'vulnerability.cve'\n      AND fact.is_tombstone = FALSE",
		// Affected leg: bounded to exactly the active vulnerability.affected_package tuples.
		"WHERE fact.fact_kind = 'vulnerability.affected_package'\n      AND fact.is_tombstone = FALSE",
		// KEV leg: bounded to exactly the active vulnerability.known_exploited tuples.
		"WHERE fact.fact_kind = 'vulnerability.known_exploited'\n      AND fact.is_tombstone = FALSE",
		// Active-generation join the (scope_id, generation_id)-leading partial
		// index resolves alongside the fact_kind bound.
		"ON fact.scope_id = scope.scope_id\n     AND scope.active_generation_id = fact.generation_id",
	} {
		if !strings.Contains(listAdvisoryCatalogQuery, want) {
			t.Fatalf("listAdvisoryCatalogQuery missing #3389 bounded-scan anchor %q:\n%s", want, listAdvisoryCatalogQuery)
		}
	}
}
