package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingChangeSurfaceGraph struct {
	runCalls []changeSurfaceRunCall
	runRows  [][]map[string]any
}

type changeSurfaceRunCall struct {
	cypher string
	params map[string]any
}

func (g *recordingChangeSurfaceGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.runCalls = append(g.runCalls, changeSurfaceRunCall{cypher: cypher, params: params})
	if len(g.runRows) == 0 {
		return nil, nil
	}
	rows := g.runRows[0]
	g.runRows = g.runRows[1:]
	return rows, nil
}

func (g *recordingChangeSurfaceGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func TestInvestigateChangeSurfaceReturnsAmbiguityWithoutTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingChangeSurfaceGraph{runRows: [][]map[string]any{{
		{"id": "workload:orders-api", "name": "orders", "labels": []any{"Workload"}, "repo_id": "repo-api"},
		{"id": "workload:orders-worker", "name": "orders", "labels": []any{"Workload"}, "repo_id": "repo-worker"},
	}}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface/investigate",
		bytes.NewBufferString(`{"target":"orders","target_type":"service","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 1; got != want {
		t.Fatalf("graph Run calls = %d, want only resolver call", got)
	}

	data := decodeChangeSurfaceData(t, w)
	resolution := data["target_resolution"].(map[string]any)
	if got, want := resolution["status"], "ambiguous"; got != want {
		t.Fatalf("resolution.status = %#v, want %#v", got, want)
	}
	candidates := resolution["candidates"].([]any)
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestInvestigateChangeSurfaceUsesBoundedTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingChangeSurfaceGraph{runRows: [][]map[string]any{
		{
			{"id": "workload:orders-api", "name": "orders-api", "labels": []any{"Workload"}, "repo_id": "repo-api"},
		},
		{
			{"id": "repo-api", "name": "orders-api", "labels": []any{"Repository"}, "depth": int64(1), "rel_type": "DEPENDS_ON", "repo_id": "repo-api"},
			{"id": "resource-db", "name": "orders-db", "labels": []any{"CloudResource"}, "depth": int64(1), "rel_type": "USES", "environment": "prod"},
			{"id": "repo-web", "name": "orders-web", "labels": []any{"Repository"}, "depth": int64(2), "rel_type": "DEPENDS_ON", "repo_id": "repo-web"},
		},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface/investigate",
		bytes.NewBufferString(`{"service_name":"orders-api","environment":"prod","max_depth":3,"limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 2; got != want {
		t.Fatalf("graph Run calls = %d, want resolver and traversal", got)
	}
	traversalCypher := graph.runCalls[1].cypher
	for _, want := range []string{"*1..3", "LIMIT 3", "ORDER BY depth, impacted.name, impacted.id"} {
		if !strings.Contains(traversalCypher, want) {
			t.Fatalf("traversal cypher missing %q: %s", want, traversalCypher)
		}
	}

	data := decodeChangeSurfaceData(t, w)
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	direct := data["direct_impact"].([]any)
	transitive := data["transitive_impact"].([]any)
	if got, want := len(direct), 2; got != want {
		t.Fatalf("direct impact count = %d, want %d", got, want)
	}
	if got, want := len(transitive), 0; got != want {
		t.Fatalf("transitive impact count after limit = %d, want %d", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "resolved_change_surface_traversal"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestInvestigateChangeSurfaceMarksRawGraphTruncationBeforeEnvironmentFilter(t *testing.T) {
	t.Parallel()

	graph := &recordingChangeSurfaceGraph{runRows: [][]map[string]any{
		{
			{"id": "workload:orders-api", "name": "orders-api", "labels": []any{"Workload"}, "repo_id": "repo-api"},
		},
		{
			{"id": "resource-staging", "name": "orders-staging", "labels": []any{"CloudResource"}, "depth": int64(1), "environment": "staging"},
			{"id": "resource-prod-a", "name": "orders-prod-a", "labels": []any{"CloudResource"}, "depth": int64(1), "environment": "prod"},
			{"id": "resource-prod-b", "name": "orders-prod-b", "labels": []any{"CloudResource"}, "depth": int64(2), "environment": "prod"},
		},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/change-surface/investigate",
		bytes.NewBufferString(`{"service_name":"orders-api","environment":"prod","limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeChangeSurfaceData(t, w)
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["truncated"], true; got != want {
		t.Fatalf("coverage.truncated = %#v, want %#v", got, want)
	}
}

func TestInvestigateChangeSurfaceAcceptsCodeTopicAndChangedPaths(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{
		fakePortContentStore: fakePortContentStore{
			entities: []EntityContent{
				{
					EntityID:     "entity-auth",
					EntityName:   "resolveGitHubAppAuth",
					EntityType:   "Function",
					RepoID:       "repo-1",
					RelativePath: "go/internal/collector/reposync/auth.go",
					Language:     "go",
					StartLine:    44,
					EndLine:      88,
				},
			},
		},
		rows: []codeTopicEvidenceRow{
			{
				SourceKind:   "entity",
				RepoID:       "repo-1",
				RelativePath: "go/internal/collector/reposync/auth.go",
				EntityID:     "entity-auth",
				EntityName:   "resolveGitHubAppAuth",
				EntityType:   "Function",
				Language:     "go",
				StartLine:    44,
				EndLine:      88,
				MatchedTerms: []string{"repo", "sync", "auth"},
				Score:        3,
			},
		},
	}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"topic":"repo-sync auth behavior in the ingester","repo_id":"repo-1","changed_paths":["go/internal/collector/reposync/auth.go"],"limit":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeChangeSurfaceData(t, w)
	codeSurface := data["code_surface"].(map[string]any)
	if got, want := int(codeSurface["matched_file_count"].(float64)), 1; got != want {
		t.Fatalf("matched_file_count = %d, want %d", got, want)
	}
	symbols := codeSurface["touched_symbols"].([]any)
	if got, want := len(symbols), 1; got != want {
		t.Fatalf("touched symbol count = %d, want %d", got, want)
	}
	nextCalls := data["recommended_next_calls"].([]any)
	if got, want := len(nextCalls), 2; got < want {
		t.Fatalf("recommended_next_calls = %d, want at least %d", got, want)
	}
}

func TestInvestigateChangeSurfaceMapsChangedPathSymbolsPastRepoProbeWindow(t *testing.T) {
	t.Parallel()

	const staleRepoWideProbeLimit = 200

	entities := make([]EntityContent, 0, staleRepoWideProbeLimit+1)
	for i := 0; i < staleRepoWideProbeLimit; i++ {
		entities = append(entities, EntityContent{
			EntityID:     "entity-noise",
			EntityName:   "noise",
			EntityType:   "Function",
			RepoID:       "repo-1",
			RelativePath: "go/internal/noise.go",
			Language:     "go",
			StartLine:    i + 1,
			EndLine:      i + 1,
		})
	}
	entities = append(entities, EntityContent{
		EntityID:     "entity-late-auth",
		EntityName:   "resolveGitHubAppAuth",
		EntityType:   "Function",
		RepoID:       "repo-1",
		RelativePath: "go/internal/collector/reposync/auth.go",
		Language:     "go",
		StartLine:    44,
		EndLine:      88,
	})

	store := &topicInvestigationContentStore{
		fakePortContentStore: fakePortContentStore{entities: entities},
	}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"repo_id":"repo-1","changed_paths":["go/internal/collector/reposync/auth.go"],"limit":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeChangeSurfaceData(t, w)
	codeSurface := data["code_surface"].(map[string]any)
	symbols := codeSurface["touched_symbols"].([]any)
	if got, want := len(symbols), 1; got != want {
		t.Fatalf("touched symbol count = %d, want %d", got, want)
	}
	symbol := symbols[0].(map[string]any)
	if got, want := symbol["entity_id"], "entity-late-auth"; got != want {
		t.Fatalf("symbol.entity_id = %#v, want %#v", got, want)
	}
	coverage := codeSurface["coverage"].(map[string]any)
	if got, want := coverage["changed_path_lookup"], "path_scoped"; got != want {
		t.Fatalf("coverage.changed_path_lookup = %#v, want %#v", got, want)
	}
}

func decodeChangeSurfaceData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil, want capability metadata")
	}
	if got, want := envelope.Truth.Capability, "platform_impact.change_surface"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", envelope.Data)
	}
	return data
}
