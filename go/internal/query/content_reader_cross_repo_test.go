package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestContentReaderSearchFileContentAnyRepo(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"deployment-charts", "charts/sample-service-api/values-qa.yaml", "", "",
					"hash-1", int64(18), "yaml", "helm_values",
				},
			},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.SearchFileContentAnyRepo(context.Background(), "sample-service-api", 10)
	if err != nil {
		t.Fatalf("SearchFileContentAnyRepo() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if got, want := results[0].RepoID, "deployment-charts"; got != want {
		t.Fatalf("results[0].RepoID = %#v, want %#v", got, want)
	}

	if len(recorder.args) != 1 {
		t.Fatalf("len(recorder.args) = %d, want 1", len(recorder.args))
	}
	if got, want := len(recorder.args[0]), 2; got != want {
		t.Fatalf("len(query args) = %d, want %d", got, want)
	}
	if got, want := recorder.args[0][0], "sample-service-api"; got != want {
		t.Fatalf("query arg pattern = %#v, want %#v", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][1]), int64(10); got != want {
		t.Fatalf("query arg limit = %d, want %d", got, want)
	}
	if strings.Contains(recorder.queries[0], "repo_id =") {
		t.Fatalf("query = %q, want cross-repo query without repo filter", recorder.queries[0])
	}
}

func TestContentReaderSearchFileContentAnyRepoExactCaseUsesCaseSensitiveLike(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
	})

	reader := NewContentReader(db)
	_, err := reader.SearchFileContentAnyRepoExactCase(context.Background(), "api.qa.example.test", 10)
	if err != nil {
		t.Fatalf("SearchFileContentAnyRepoExactCase() error = %v, want nil", err)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("len(recorder.queries) = %d, want 1", len(recorder.queries))
	}
	if !strings.Contains(recorder.queries[0], "content LIKE '%' || $1 || '%'") {
		t.Fatalf("query = %q, want case-sensitive LIKE", recorder.queries[0])
	}
	if strings.Contains(recorder.queries[0], "ILIKE") {
		t.Fatalf("query = %q, must not use ILIKE for exact-case hostname search", recorder.queries[0])
	}
}

func TestContentReaderSearchFileReferenceAnyRepoUsesIndexedReferences(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"available"},
			rows:    [][]driver.Value{{true}},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"deployment-charts", "charts/sample-service-api/values-qa.yaml", "", "",
					"hash-1", int64(18), "yaml", "helm_values",
				},
			},
		},
	})

	reader := NewContentReader(db)
	results, available, err := reader.SearchFileReferenceAnyRepo(context.Background(), "hostname", "api.qa.example.test", 10)
	if err != nil {
		t.Fatalf("SearchFileReferenceAnyRepo() error = %v, want nil", err)
	}
	if !available {
		t.Fatal("SearchFileReferenceAnyRepo() available = false, want true")
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if got, want := results[0].RepoID, "deployment-charts"; got != want {
		t.Fatalf("results[0].RepoID = %#v, want %#v", got, want)
	}

	if len(recorder.queries) != 2 {
		t.Fatalf("len(recorder.queries) = %d, want 2", len(recorder.queries))
	}
	if !strings.Contains(recorder.queries[0], "FROM content_file_references") {
		t.Fatalf("availability query = %q, want content_file_references", recorder.queries[0])
	}
	if !strings.Contains(recorder.queries[1], "JOIN content_files") {
		t.Fatalf("lookup query = %q, want content_files join", recorder.queries[1])
	}
	if strings.Contains(recorder.queries[1], "content LIKE") || strings.Contains(recorder.queries[1], "content ILIKE") {
		t.Fatalf("lookup query = %q, must not scan content body", recorder.queries[1])
	}
	if got, want := recorder.args[1][0], "hostname"; got != want {
		t.Fatalf("lookup reference kind arg = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[1][1], "api.qa.example.test"; got != want {
		t.Fatalf("lookup reference value arg = %#v, want %#v", got, want)
	}
}

func TestContentReaderSearchFileReferenceAnyRepoKeepsIndexAvailableWhenNoRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"available"},
			rows:    [][]driver.Value{{true}},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
	})

	reader := NewContentReader(db)
	results, available, err := reader.SearchFileReferenceAnyRepo(context.Background(), "hostname", "missing.example.test", 10)
	if err != nil {
		t.Fatalf("SearchFileReferenceAnyRepo() error = %v, want nil", err)
	}
	if !available {
		t.Fatal("SearchFileReferenceAnyRepo() available = false, want true for empty indexed lookup")
	}
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}
}

func TestContentReaderSearchFileContentAnyRepoDefaultsLimit(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.SearchFileContentAnyRepo(context.Background(), "qa.example.internal", 0)
	if err != nil {
		t.Fatalf("SearchFileContentAnyRepo() error = %v, want nil", err)
	}
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}

	if len(recorder.args) != 1 {
		t.Fatalf("len(recorder.args) = %d, want 1", len(recorder.args))
	}
	if got, want := numericDriverValue(t, recorder.args[0][1]), int64(50); got != want {
		t.Fatalf("query arg default limit = %d, want %d", got, want)
	}
}

type recordingContentReaderQueryResult struct {
	columns []string
	rows    [][]driver.Value
	err     error
}

type recordingContentReader struct {
	mu      sync.Mutex
	results []recordingContentReaderQueryResult
	queries []string
	args    [][]driver.Value
}

func openRecordingContentReaderDB(t *testing.T, results []recordingContentReaderQueryResult) (*sql.DB, *recordingContentReader) {
	t.Helper()

	name := fmt.Sprintf("content-reader-recording-test-%d", atomic.AddUint64(&recordingContentReaderDriverSeq, 1))
	recorder := &recordingContentReader{results: append([]recordingContentReaderQueryResult(nil), results...)}
	sql.Register(name, &recordingContentReaderDriver{
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

var recordingContentReaderDriverSeq uint64

type recordingContentReaderDriver struct {
	recorder *recordingContentReader
}

func (d *recordingContentReaderDriver) Open(string) (driver.Conn, error) {
	return &recordingContentReaderConn{
		recorder: d.recorder,
	}, nil
}

type recordingContentReaderConn struct {
	recorder *recordingContentReader
}

func (c *recordingContentReaderConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *recordingContentReaderConn) Close() error {
	return nil
}

func (c *recordingContentReaderConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *recordingContentReaderConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.recorder.mu.Lock()
	defer c.recorder.mu.Unlock()

	c.recorder.queries = append(c.recorder.queries, query)

	recordedArgs := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		recordedArgs = append(recordedArgs, arg.Value)
	}
	c.recorder.args = append(c.recorder.args, recordedArgs)

	if strings.Contains(query, "SELECT EXISTS") &&
		strings.Contains(query, "FROM content_file_references") &&
		(len(c.recorder.results) == 0 || !recordingContentReaderResultColumnsEqual(c.recorder.results[0], []string{"available"})) {
		return &recordingContentReaderRows{
			columns: []string{"available"},
			rows:    [][]driver.Value{{false}},
		}, nil
	}
	if len(c.recorder.results) == 0 {
		return nil, fmt.Errorf("unexpected query")
	}
	result := c.recorder.results[0]
	c.recorder.results = c.recorder.results[1:]
	if result.err != nil {
		return nil, result.err
	}
	return &recordingContentReaderRows{columns: result.columns, rows: result.rows}, nil
}

func recordingContentReaderResultColumnsEqual(result recordingContentReaderQueryResult, columns []string) bool {
	if len(result.columns) != len(columns) {
		return false
	}
	for i, column := range columns {
		if result.columns[i] != column {
			return false
		}
	}
	return true
}

type recordingContentReaderRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *recordingContentReaderRows) Columns() []string {
	return r.columns
}

func (r *recordingContentReaderRows) Close() error {
	return nil
}

func (r *recordingContentReaderRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func numericDriverValue(t *testing.T, value driver.Value) int64 {
	t.Helper()

	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		t.Fatalf("driver value type = %T, want numeric", value)
		return 0
	}
}
