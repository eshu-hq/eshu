package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// provenanceDeadCodeContentStore is a content store whose incoming-edge probe
// returns a per-entity confidence/method so provenance-weighted reachability can
// be exercised end to end through the content read-model path.
type provenanceDeadCodeContentStore struct {
	fakeDeadCodeContentStore
	incomingEdges map[string]deadCodeIncomingEdge
}

func (s provenanceDeadCodeContentStore) DeadCodeIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	incoming := make(map[string]deadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if edge, ok := s.incomingEdges[entityID]; ok {
			incoming[entityID] = edge
		}
	}
	return incoming, nil
}

func goDeadCodeEntity(entityID, name string) EntityContent {
	return EntityContent{
		EntityID:     entityID,
		RepoID:       "repo-1",
		RelativePath: "internal/payments/" + name + ".go",
		EntityType:   "Function",
		EntityName:   name,
		Language:     "go",
		SourceCache:  "func " + name + "() {}",
	}
}

func decodeDeadCodeResults(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	rawResults := data["results"].([]any)
	results := make([]map[string]any, 0, len(rawResults))
	for _, raw := range rawResults {
		results = append(results, raw.(map[string]any))
	}
	return results
}

func runDeadCodeWithContentIncoming(
	t *testing.T,
	rows []map[string]any,
	entities map[string]EntityContent,
	incoming map[string]deadCodeIncomingEdge,
) []map[string]any {
	t.Helper()
	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e:Function") {
					return nil, nil
				}
				return rows, nil
			},
			runIncoming: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				t.Fatalf("content read model should answer incoming probe: cypher=%s params=%#v", cypher, params)
				return nil, nil
			},
		},
		Content: provenanceDeadCodeContentStore{
			fakeDeadCodeContentStore: fakeDeadCodeContentStore{entities: entities},
			incomingEdges:            incoming,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return decodeDeadCodeResults(t, w)
}

func TestDeadCodeWeakIncomingEdgeKeepsResultAsAmbiguous(t *testing.T) {
	t.Parallel()

	results := runDeadCodeWithContentIncoming(
		t,
		[]map[string]any{deadCodeScanRow("weak-helper", "weakHelper")},
		map[string]EntityContent{"weak-helper": goDeadCodeEntity("weak-helper", "weakHelper")},
		map[string]deadCodeIncomingEdge{
			"weak-helper": {
				MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
				Method:        codeprovenance.MethodRepoUniqueName,
			},
		},
	)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	if got, want := results[0]["entity_id"], "weak-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["classification"], deadCodeClassificationAmbiguous; got != want {
		t.Fatalf("results[0][classification] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["weak_incoming_only"], true; got != want {
		t.Fatalf("results[0][weak_incoming_only] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["weak_incoming_method"], codeprovenance.MethodRepoUniqueName; got != want {
		t.Fatalf("results[0][weak_incoming_method] = %#v, want %#v", got, want)
	}
}

func TestDeadCodeStrongIncomingEdgeFiltersResult(t *testing.T) {
	t.Parallel()

	for _, method := range []string{
		codeprovenance.MethodSCIP,
		codeprovenance.MethodImportBinding,
		codeprovenance.MethodSameFile,
	} {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			results := runDeadCodeWithContentIncoming(
				t,
				[]map[string]any{deadCodeScanRow("live-helper", "liveHelper")},
				map[string]EntityContent{"live-helper": goDeadCodeEntity("live-helper", "liveHelper")},
				map[string]deadCodeIncomingEdge{
					"live-helper": {MaxConfidence: codeprovenance.Confidence(method), Method: method},
				},
			)
			if got, want := len(results), 0; got != want {
				t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
			}
		})
	}
}

func TestDeadCodeNoIncomingEdgeKeepsResultAsUnused(t *testing.T) {
	t.Parallel()

	results := runDeadCodeWithContentIncoming(
		t,
		[]map[string]any{deadCodeScanRow("dead-helper", "deadHelper")},
		map[string]EntityContent{"dead-helper": goDeadCodeEntity("dead-helper", "deadHelper")},
		map[string]deadCodeIncomingEdge{},
	)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	if got, want := results[0]["entity_id"], "dead-helper"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["classification"], deadCodeClassificationUnused; got != want {
		t.Fatalf("results[0][classification] = %#v, want %#v", got, want)
	}
	if _, ok := results[0]["weak_incoming_only"]; ok {
		t.Fatalf("results[0][weak_incoming_only] set, want unset")
	}
}

func TestDeadCodeUnspecifiedIncomingMethodTreatedStrong(t *testing.T) {
	t.Parallel()

	// An edge with an empty/unrecorded resolution_method resolves to
	// LegacyConfidence (strong); the entity must be filtered out, not demoted.
	results := runDeadCodeWithContentIncoming(
		t,
		[]map[string]any{deadCodeScanRow("legacy-helper", "legacyHelper")},
		map[string]EntityContent{"legacy-helper": goDeadCodeEntity("legacy-helper", "legacyHelper")},
		map[string]deadCodeIncomingEdge{
			"legacy-helper": {MaxConfidence: codeprovenance.Confidence(""), Method: ""},
		},
	)
	if got, want := len(results), 0; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
}

func TestDeadCodeIncomingEdgeIsWeakBoundary(t *testing.T) {
	t.Parallel()

	weakThreshold := codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName)
	if !deadCodeIncomingEdgeIsWeak(weakThreshold) {
		t.Fatalf("deadCodeIncomingEdgeIsWeak(%v) = false, want true", weakThreshold)
	}
	if deadCodeIncomingEdgeIsWeak(codeprovenance.Confidence(codeprovenance.MethodScopeUniqueName)) {
		t.Fatalf("deadCodeIncomingEdgeIsWeak(scope_unique_name) = true, want false")
	}
	if deadCodeIncomingEdgeIsWeak(codeprovenance.LegacyConfidence) {
		t.Fatalf("deadCodeIncomingEdgeIsWeak(LegacyConfidence) = true, want false")
	}
}

func TestDeadCodeWeakGraphIncomingEdgeKeepsSQLFunctionAsAmbiguous(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e:SqlFunction") {
					return nil, nil
				}
				return []map[string]any{
					{
						"entity_id": "sql-weak", "name": "public.refresh_users", "labels": []any{"SqlFunction"},
						"file_path": "db/functions.sql", "repo_id": "repo-1", "repo_name": "warehouse", "language": "sql",
					},
				}, nil
			},
			runIncoming: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "resolution_method") {
					t.Fatalf("incoming cypher missing resolution_method projection:\n%s", cypher)
				}
				return []map[string]any{
					{
						"incoming_entity_id": "sql-weak",
						"resolution_method":  codeprovenance.MethodRepoUniqueName,
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"sql-weak": {
					EntityID:     "sql-weak",
					RepoID:       "repo-1",
					RelativePath: "db/functions.sql",
					EntityType:   "SqlFunction",
					EntityName:   "public.refresh_users",
					Language:     "sql",
					SourceCache:  "CREATE FUNCTION public.refresh_users() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	results := decodeDeadCodeResults(t, w)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	if got, want := results[0]["entity_id"], "sql-weak"; got != want {
		t.Fatalf("results[0][entity_id] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["classification"], deadCodeClassificationAmbiguous; got != want {
		t.Fatalf("results[0][classification] = %#v, want %#v", got, want)
	}
	if got, want := results[0]["weak_incoming_only"], true; got != want {
		t.Fatalf("results[0][weak_incoming_only] = %#v, want %#v", got, want)
	}
}

func TestDeadCodeInvestigationSurfacesWeakIncomingAmbiguityReason(t *testing.T) {
	t.Parallel()

	content := &investigationWeakIncomingStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{ID: "repo-1", Name: "payments"}},
			},
			entities: map[string]EntityContent{
				"weak-helper": goDeadCodeEntity("weak-helper", "weakHelper"),
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("weak-helper", "weakHelper", "go", "internal/payments/weakHelper.go", 10, 12),
		},
		incomingEdges: map[string]deadCodeIncomingEdge{
			"weak-helper": {
				MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
				Method:        codeprovenance.MethodRepoUniqueName,
			},
		},
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j:   fakeGraphReader{},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/investigate",
		bytes.NewBufferString(`{"repo_id":"payments","limit":10,"offset":0}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	data := decodeEnvelopeData(t, w.Body.Bytes())
	buckets := requireDeadCodeInvestigationMap(t, data, "candidate_buckets")
	ambiguous := requireDeadCodeInvestigationSlice(t, buckets, "ambiguous")
	if got, want := len(ambiguous), 1; got != want {
		t.Fatalf("len(ambiguous) = %d, want %d ambiguous=%#v", got, want, ambiguous)
	}
	entry := ambiguous[0].(map[string]any)
	if got, want := entry["entity_id"], "weak-helper"; got != want {
		t.Fatalf("ambiguous[0][entity_id] = %#v, want %#v", got, want)
	}
	reasons, ok := entry["ambiguity_reasons"].([]any)
	if !ok {
		t.Fatalf("ambiguity_reasons type = %T, want []any", entry["ambiguity_reasons"])
	}
	wantReason := "weak_incoming_edge:" + codeprovenance.MethodRepoUniqueName
	found := false
	for _, reason := range reasons {
		if reason == wantReason {
			found = true
		}
	}
	if !found {
		t.Fatalf("ambiguity_reasons = %#v, want to contain %q", reasons, wantReason)
	}
}

type investigationWeakIncomingStore struct {
	fakeDeadCodeContentStore
	rows          []map[string]any
	incomingEdges map[string]deadCodeIncomingEdge
}

func (s *investigationWeakIncomingStore) DeadCodeCandidateRows(
	_ context.Context,
	_ string,
	label string,
	_ string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	if label != "Function" || offset >= len(s.rows) {
		return nil, nil
	}
	end := offset + limit
	if end > len(s.rows) {
		end = len(s.rows)
	}
	return s.rows[offset:end], nil
}

func (s *investigationWeakIncomingStore) DeadCodeIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	incoming := make(map[string]deadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if edge, ok := s.incomingEdges[entityID]; ok {
			incoming[entityID] = edge
		}
	}
	return incoming, nil
}

func TestDeadCodeStrongGraphIncomingEdgeFiltersSQLFunction(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e:SqlFunction") {
					return nil, nil
				}
				return []map[string]any{
					{
						"entity_id": "sql-live", "name": "public.refresh_users", "labels": []any{"SqlFunction"},
						"file_path": "db/functions.sql", "repo_id": "repo-1", "repo_name": "warehouse", "language": "sql",
					},
				}, nil
			},
			runIncoming: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"incoming_entity_id": "sql-live",
						"resolution_method":  codeprovenance.MethodSCIP,
					},
				}, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"sql-live": {
					EntityID:     "sql-live",
					RepoID:       "repo-1",
					RelativePath: "db/functions.sql",
					EntityType:   "SqlFunction",
					EntityName:   "public.refresh_users",
					Language:     "sql",
					SourceCache:  "CREATE FUNCTION public.refresh_users() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	results := decodeDeadCodeResults(t, w)
	if got, want := len(results), 0; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
}
