package query

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestContentHandlerSearchFilesAcceptsPatternAndRepoIDs(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "src/app.ts", "", "",
					"hash-1", int64(24), "typescript", "source",
				},
			},
		},
	})

	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_ids":["repo-1"],"limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	if len(recorder.args) != 1 {
		t.Fatalf("len(recorder.args) = %d, want 1", len(recorder.args))
	}
	if got, want := recorder.args[0][0], "repo-1"; got != want {
		t.Fatalf("query arg repo_id = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], "renderApp"; got != want {
		t.Fatalf("query arg pattern = %#v, want %#v", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
}

func TestContentHandlerSearchEntitiesAcceptsPatternAndRepoIDs(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.ts", "Function", "renderApp",
					int64(10), int64(20), "typescript", "function renderApp() {}", []byte(`{"kind":"handler"}`),
				},
			},
		},
	})

	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_ids":["repo-1"],"limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	if len(recorder.args) != 1 {
		t.Fatalf("len(recorder.args) = %d, want 1", len(recorder.args))
	}
	if got, want := recorder.args[0][0], "repo-1"; got != want {
		t.Fatalf("query arg repo_id = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], "renderApp"; got != want {
		t.Fatalf("query arg pattern = %#v, want %#v", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
}

func TestContentHandlerSearchFilesBoundsExplicitMultiRepoSearch(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "src/one.ts", "", "",
					"hash-1", int64(11), "typescript", "source",
				},
			},
		},
	})
	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_ids":["repo-1","repo-2"],"limit":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	if got, want := len(recorder.args), 1; got != want {
		t.Fatalf("len(recorder.args) = %d, want %d", got, want)
	}
	if got, want := recorder.args[0][0], "repo-1"; got != want {
		t.Fatalf("first query repo_id = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], "renderApp"; got != want {
		t.Fatalf("first query pattern = %#v, want %#v", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][2]), int64(1); got != want {
		t.Fatalf("first query limit = %d, want %d", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
}

func TestContentHandlerSearchEntitiesBoundsExplicitMultiRepoSearch(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/one.ts", "Function", "renderApp",
					int64(5), int64(9), "typescript", "function renderApp() {}", []byte(`{"kind":"handler"}`),
				},
			},
		},
	})
	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_ids":["repo-1","repo-2"],"limit":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	if got, want := len(recorder.args), 1; got != want {
		t.Fatalf("len(recorder.args) = %d, want %d", got, want)
	}
	if got, want := recorder.args[0][0], "repo-1"; got != want {
		t.Fatalf("first query repo_id = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], "renderApp"; got != want {
		t.Fatalf("first query pattern = %#v, want %#v", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][2]), int64(1); got != want {
		t.Fatalf("first query limit = %d, want %d", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
}

func TestContentHandlerSearchFilesUsesAnyRepoWhenRepoScopeOmitted(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-2", "src/app.ts", "", "",
					"hash-1", int64(24), "typescript", "source",
				},
			},
		},
	})

	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	if len(recorder.args) != 1 {
		t.Fatalf("len(recorder.args) = %d, want 1", len(recorder.args))
	}
	if got, want := len(recorder.args[0]), 2; got != want {
		t.Fatalf("len(query args) = %d, want %d", got, want)
	}
	if got, want := recorder.args[0][0], "renderApp"; got != want {
		t.Fatalf("query arg pattern = %#v, want %#v", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][1]), int64(10); got != want {
		t.Fatalf("query arg limit = %d, want %d", got, want)
	}
	if strings.Contains(recorder.queries[0], "repo_id =") {
		t.Fatalf("query = %q, want any-repo search without repo filter", recorder.queries[0])
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("resp[results] = %#v, want one any-repo file result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[results][0] type = %T, want map[string]any", results[0])
	}
	if got, want := result["repo_id"], "repo-2"; got != want {
		t.Fatalf("result[repo_id] = %#v, want %#v", got, want)
	}
}

func TestContentHandlerSearchFilesReturnsCompatibilityAliases(t *testing.T) {
	t.Parallel()

	db, _ := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", "src/app.ts", "", "",
					"hash-1", int64(24), "typescript", "source",
				},
			},
		},
	})

	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_ids":["repo-1"],"limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one result", resp["results"])
	}
	matches, ok := resp["matches"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches = %#v, want alias for one result", resp["matches"])
	}
	if !reflect.DeepEqual(matches, results) {
		t.Fatalf("matches = %#v, want alias of results %#v", matches, results)
	}
	if got, want := resp["source_backend"], "postgres_content_store"; got != want {
		t.Fatalf("source_backend = %#v, want %#v", got, want)
	}
}

func TestContentHandlerSearchEntitiesUsesAnyRepoWhenRepoScopeOmitted(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-2", "repo-2", "src/app.ts", "Function", "renderApp",
					int64(10), int64(20), "typescript", "function renderApp() {}", []byte(`{"kind":"handler"}`),
				},
			},
		},
	})

	handler := &ContentHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"renderApp","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	if len(recorder.args) != 1 {
		t.Fatalf("len(recorder.args) = %d, want 1", len(recorder.args))
	}
	if got, want := len(recorder.args[0]), 2; got != want {
		t.Fatalf("len(query args) = %d, want %d", got, want)
	}
	if got, want := recorder.args[0][0], "renderApp"; got != want {
		t.Fatalf("query arg pattern = %#v, want %#v", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][1]), int64(10); got != want {
		t.Fatalf("query arg limit = %d, want %d", got, want)
	}
	if strings.Contains(recorder.queries[0], "repo_id =") {
		t.Fatalf("query = %q, want any-repo search without repo filter", recorder.queries[0])
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("resp[results] = %#v, want one any-repo entity result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[results][0] type = %T, want map[string]any", results[0])
	}
	if got, want := result["repo_id"], "repo-2"; got != want {
		t.Fatalf("result[repo_id] = %#v, want %#v", got, want)
	}
}

type contentSearchQueryResult struct {
	columns []string
	rows    [][]driver.Value
	err     error
}

type recordingContentSearch struct {
	queries []string
	args    [][]driver.Value
}

func openRecordingContentSearchDB(t *testing.T, results []contentSearchQueryResult) (*sql.DB, *recordingContentSearch) {
	t.Helper()

	name := fmt.Sprintf("content-search-handler-test-%d", atomic.AddUint64(&contentSearchDriverSeq, 1))
	recorder := &recordingContentSearch{}
	sql.Register(name, &contentSearchDriver{
		results:  results,
		recorder: recorder,
	})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db, recorder
}

var contentSearchDriverSeq uint64

type contentSearchDriver struct {
	results  []contentSearchQueryResult
	recorder *recordingContentSearch
}

func (d *contentSearchDriver) Open(string) (driver.Conn, error) {
	return &contentSearchConn{
		results:  append([]contentSearchQueryResult(nil), d.results...),
		recorder: d.recorder,
	}, nil
}

type contentSearchConn struct {
	results  []contentSearchQueryResult
	recorder *recordingContentSearch
}

func (c *contentSearchConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *contentSearchConn) Close() error {
	return nil
}

func (c *contentSearchConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *contentSearchConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.recorder.queries = append(c.recorder.queries, query)

	recorded := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		recorded = append(recorded, arg.Value)
	}
	c.recorder.args = append(c.recorder.args, recorded)

	if len(c.results) == 0 {
		return nil, fmt.Errorf("unexpected query")
	}
	result := c.results[0]
	c.results = c.results[1:]
	if result.err != nil {
		return nil, result.err
	}
	return &contentSearchRows{columns: result.columns, rows: result.rows}, nil
}

var _ driver.QueryerContext = (*contentSearchConn)(nil)

type contentSearchRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *contentSearchRows) Columns() []string {
	return r.columns
}

func (r *contentSearchRows) Close() error {
	return nil
}

func (r *contentSearchRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.index]
	r.index++
	copy(dest, row)
	return nil
}
