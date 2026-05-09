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

func deadCodeScanRow(entityID string, name string) map[string]any {
	return map[string]any{
		"entity_id":  entityID,
		"name":       name,
		"labels":     []any{"Function"},
		"file_path":  "internal/payments/" + name + ".go",
		"repo_id":    "repo-1",
		"repo_name":  "payments",
		"language":   "go",
		"start_line": int64(10),
		"end_line":   int64(12),
	}
}

func TestHandleDeadCodeFiltersIncomingEdgesWithContentReadModel(t *testing.T) {
	t.Parallel()

	var candidateCalls int
	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				candidateCalls++
				if strings.Contains(cypher, "NOT EXISTS") || strings.Contains(cypher, "NOT ()-[:") {
					t.Fatalf("candidate query should not include incoming-edge anti-join:\n%s", cypher)
				}
				if _, ok := params["entity_ids"]; ok {
					t.Fatalf("candidate params unexpectedly contain entity_ids: %#v", params)
				}
				return []map[string]any{
					deadCodeScanRow("live-helper", "liveHelper"),
					deadCodeScanRow("dead-helper", "deadHelper"),
				}, nil
			},
			runIncoming: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				t.Fatalf("dead-code should use content read model before graph probes: cypher=%s params=%#v", cypher, params)
				return nil, nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"live-helper": {
					EntityID:     "live-helper",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/live.go",
					EntityType:   "Function",
					EntityName:   "liveHelper",
					Language:     "go",
					SourceCache:  "func liveHelper() {}",
				},
				"dead-helper": {
					EntityID:     "dead-helper",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/dead.go",
					EntityType:   "Function",
					EntityName:   "deadHelper",
					Language:     "go",
					SourceCache:  "func deadHelper() {}",
				},
			},
			incomingEntityIDs: map[string]bool{"live-helper": true},
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

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	results := data["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d results=%#v", got, want, results)
	}
	result := results[0].(map[string]any)
	if got, want := result["entity_id"], "dead-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := candidateCalls, 1; got != want {
		t.Fatalf("candidate graph calls = %d, want %d", got, want)
	}
}

func TestHandleDeadCodePagesCandidatesFromContentReadModel(t *testing.T) {
	t.Parallel()

	content := &contentCandidateDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"dead-helper": {
					EntityID:     "dead-helper",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/dead.go",
					EntityType:   "Function",
					EntityName:   "deadHelper",
					Language:     "go",
					SourceCache:  "func deadHelper() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeScanRow("dead-helper", "deadHelper"),
		},
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				t.Fatalf("dead-code candidate scan should use content read model before graph paging: cypher=%s params=%#v", cypher, params)
				return nil, nil
			},
		},
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := content.candidateCalls, len(deadCodeCandidateLabels); got != want {
		t.Fatalf("candidate content calls = %d, want %d", got, want)
	}
	if got, want := content.candidateRepoID, "repo-1"; got != want {
		t.Fatalf("candidate repo id = %q, want %q", got, want)
	}
	if got, want := content.candidateLabels, deadCodeCandidateLabels; !equalStringSlices(got, want) {
		t.Fatalf("candidate labels = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeBatchesCandidateContentHydration(t *testing.T) {
	t.Parallel()

	singleCalls := 0
	batchCalls := 0
	var batchIDs []string
	store := &batchingDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"first-helper": {
					EntityID:     "first-helper",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/first.go",
					EntityType:   "Function",
					EntityName:   "firstHelper",
					Language:     "go",
					SourceCache:  "func firstHelper() {}",
					Metadata:     map[string]any{"root_kinds": []any{"none"}},
				},
				"second-helper": {
					EntityID:     "second-helper",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/second.go",
					EntityType:   "Function",
					EntityName:   "secondHelper",
					Language:     "go",
					SourceCache:  "func secondHelper() {}",
				},
			},
		},
		singleCalls: &singleCalls,
		batchCalls:  &batchCalls,
		batchIDs:    &batchIDs,
	}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					deadCodeScanRow("first-helper", "firstHelper"),
					deadCodeScanRow("second-helper", "secondHelper"),
				}, nil
			},
		},
		Content: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := batchCalls, 1; got != want {
		t.Fatalf("batch content calls = %d, want %d", got, want)
	}
	if got, want := singleCalls, 0; got != want {
		t.Fatalf("single content calls = %d, want %d", got, want)
	}
	if got, want := batchIDs, []string{"first-helper", "second-helper"}; !equalStringSlices(got, want) {
		t.Fatalf("batch ids = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeContinuesCandidateScanAfterPolicyExclusions(t *testing.T) {
	t.Parallel()

	pageLimit := deadCodeCandidateQueryLimit(2)
	rawCandidates := make([]map[string]any, 0, pageLimit+1)
	for i := 0; i < pageLimit; i++ {
		rawCandidates = append(rawCandidates, map[string]any{
			"entity_id": "public-api", "name": "PublicAPI", "labels": []any{"Function"},
			"file_path": "pkg/payments/api.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		})
	}
	rawCandidates = append(rawCandidates, map[string]any{
		"entity_id": "internal-helper", "name": "privateAlpha", "labels": []any{"Function"},
		"file_path": "internal/payments/a.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
	})

	var offsets []int
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "SKIP $skip") {
					t.Fatalf("cypher = %q, want bounded page offset", cypher)
				}
				offset, ok := params["skip"].(int)
				if !ok {
					t.Fatalf("params[skip] type = %T, want int", params["skip"])
				}
				limit, ok := params["limit"].(int)
				if !ok {
					t.Fatalf("params[limit] type = %T, want int", params["limit"])
				}
				offsets = append(offsets, offset)
				if offset >= len(rawCandidates) {
					return nil, nil
				}
				end := offset + limit
				if end > len(rawCandidates) {
					end = len(rawCandidates)
				}
				return rawCandidates[offset:end], nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"public-api": {
					EntityID:     "public-api",
					RelativePath: "pkg/payments/api.go",
					EntityType:   "Function",
					EntityName:   "PublicAPI",
					Language:     "go",
					SourceCache:  "func PublicAPI() {}",
				},
				"internal-helper": {
					EntityID:     "internal-helper",
					RelativePath: "internal/payments/a.go",
					EntityType:   "Function",
					EntityName:   "privateAlpha",
					Language:     "go",
					SourceCache:  "func privateAlpha() {}",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", resp["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "internal-helper"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := offsets, []int{0, pageLimit}; !equalIntSlices(got, want) {
		t.Fatalf("offsets = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_truncated"], false; got != want {
		t.Fatalf("resp[candidate_scan_truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_pages"], float64(5); got != want {
		t.Fatalf("resp[candidate_scan_pages] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_rows"], float64(pageLimit+1); got != want {
		t.Fatalf("resp[candidate_scan_rows] = %#v, want %#v", got, want)
	}
}

func equalIntSlices(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type batchingDeadCodeContentStore struct {
	fakeDeadCodeContentStore
	singleCalls *int
	batchCalls  *int
	batchIDs    *[]string
}

type contentCandidateDeadCodeStore struct {
	fakeDeadCodeContentStore
	rows            []map[string]any
	candidateCalls  int
	candidateRepoID string
	candidateLabels []string
}

func (f *contentCandidateDeadCodeStore) DeadCodeCandidateRows(
	_ context.Context,
	repoID string,
	label string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	f.candidateCalls++
	f.candidateRepoID = repoID
	f.candidateLabels = append(f.candidateLabels, label)
	if label != "Function" {
		return nil, nil
	}
	if offset >= len(f.rows) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.rows) {
		end = len(f.rows)
	}
	return f.rows[offset:end], nil
}

func (f *batchingDeadCodeContentStore) GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error) {
	*f.singleCalls++
	return f.fakeDeadCodeContentStore.GetEntityContent(ctx, entityID)
}

func (f *batchingDeadCodeContentStore) GetEntityContents(
	_ context.Context,
	entityIDs []string,
) (map[string]*EntityContent, error) {
	*f.batchCalls++
	*f.batchIDs = append((*f.batchIDs)[:0], entityIDs...)
	entities := make(map[string]*EntityContent, len(entityIDs))
	for _, entityID := range entityIDs {
		entity, ok := f.entities[entityID]
		if !ok {
			continue
		}
		cloned := entity
		entities[entityID] = &cloned
	}
	return entities, nil
}
