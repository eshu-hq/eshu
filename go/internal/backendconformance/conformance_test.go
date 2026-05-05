package backendconformance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestDefaultReadCorpusRunsAgainstGraphQuery(t *testing.T) {
	t.Parallel()

	query := &recordingGraphQuery{
		rows: []map[string]any{{"ok": true}},
	}
	report, err := RunReadCorpus(context.Background(), query, DefaultReadCorpus())
	if err != nil {
		t.Fatalf("RunReadCorpus() error = %v", err)
	}
	if got, want := len(report.Results), len(DefaultReadCorpus()); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	if got, want := len(query.calls), len(DefaultReadCorpus()); got != want {
		t.Fatalf("query call count = %d, want %d", got, want)
	}
	for _, call := range query.calls {
		if strings.Contains(strings.ToUpper(call.cypher), "MERGE") {
			t.Fatalf("read corpus issued mutation query: %s", call.cypher)
		}
	}
}

func TestLiveConformanceTimeoutCoversRepeatedWriteAttempts(t *testing.T) {
	t.Parallel()

	minimumBudget := time.Duration(liveWriteAttempts) * liveWriteAttemptTimeout
	if liveTestTimeout < minimumBudget {
		t.Fatalf("liveTestTimeout = %s, want at least %s for %d write attempts", liveTestTimeout, minimumBudget, liveWriteAttempts)
	}
}

func TestReadCorpusReportsRowsBelowMinimum(t *testing.T) {
	t.Parallel()

	readCases := []ReadCase{{
		Name:       "direct repository read",
		Capability: CapabilityDirectGraphReads,
		Cypher:     "MATCH (r:Repository {id: $repo_id}) RETURN r.id AS id",
		Parameters: map[string]any{"repo_id": "repo:backend-conformance"},
		MinRows:    1,
	}}

	_, err := RunReadCorpus(context.Background(), &recordingGraphQuery{}, readCases)
	if err == nil {
		t.Fatal("RunReadCorpus() error = nil, want minimum row failure")
	}
	if !strings.Contains(err.Error(), "returned 0 rows, want at least 1") {
		t.Fatalf("RunReadCorpus() error = %v, want minimum row context", err)
	}
}

func TestReadCorpusRejectsMutationQueries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cypher string
	}{
		{name: "merge", cypher: "MERGE (r:Repository {id: $repo_id}) RETURN r"},
		{name: "set newline", cypher: "MATCH (r:Repository {id: $repo_id}) SET\nr.name = 'bad' RETURN r"},
		{name: "set tab", cypher: "MATCH (r:Repository {id: $repo_id}) SET\tr.name = 'bad' RETURN r"},
		{name: "remove", cypher: "MATCH (r:Repository {id: $repo_id}) REMOVE r.name RETURN r"},
		{name: "load csv", cypher: "LOAD CSV FROM 'file:///bad.csv' AS row RETURN row"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mutating := []ReadCase{{
				Name:       tt.name,
				Capability: CapabilityDirectGraphReads,
				Cypher:     tt.cypher,
				Parameters: map[string]any{"repo_id": "repo:backend-conformance"},
			}}

			_, err := RunReadCorpus(context.Background(), &recordingGraphQuery{}, mutating)
			if err == nil {
				t.Fatal("RunReadCorpus() error = nil, want mutation rejection")
			}
			if !strings.Contains(err.Error(), "read case") {
				t.Fatalf("RunReadCorpus() error = %v, want read case context", err)
			}
		})
	}
}

func TestReadCorpusAllowsReadTokensContainingMutationSubstrings(t *testing.T) {
	t.Parallel()

	readCases := []ReadCase{{
		Name:       "offset pagination",
		Capability: CapabilityDirectGraphReads,
		Cypher: `MATCH (r:Repository)
RETURN r.id AS id
ORDER BY id
OFFSET 10
LIMIT 5`,
	}}

	_, err := RunReadCorpus(context.Background(), &recordingGraphQuery{}, readCases)
	if err != nil {
		t.Fatalf("RunReadCorpus() error = %v, want nil", err)
	}
}

func TestDefaultWriteCorpusRunsGroupedCanonicalCases(t *testing.T) {
	t.Parallel()

	executor := &recordingCypherExecutor{}
	report, err := RunWriteCorpus(context.Background(), executor, DefaultWriteCorpus())
	if err != nil {
		t.Fatalf("RunWriteCorpus() error = %v", err)
	}
	if got, want := len(report.Results), len(DefaultWriteCorpus()); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	if executor.groupCalls == 0 {
		t.Fatal("RunWriteCorpus() did not exercise grouped write case")
	}
	if executor.singleCalls == 0 {
		t.Fatal("RunWriteCorpus() did not exercise single-statement write case")
	}
}

func TestDefaultWriteCorpusSeedsDeadCodeReadinessFixture(t *testing.T) {
	t.Parallel()

	executor := &recordingCypherExecutor{}
	if _, err := RunWriteCorpus(context.Background(), executor, DefaultWriteCorpus()); err != nil {
		t.Fatalf("RunWriteCorpus() error = %v", err)
	}

	var functionStatements int
	for _, stmt := range executor.groupStatements {
		if !strings.Contains(stmt.Cypher, "SET ") {
			continue
		}
		if !strings.Contains(stmt.Cypher, ":Function") {
			continue
		}
		functionStatements++
		if _, ok := stmt.Parameters["repo_id"]; !ok {
			t.Fatalf("function statement parameters = %#v, want repo_id for dead-code read corpus", stmt.Parameters)
		}
	}
	if functionStatements == 0 {
		t.Fatal("grouped write corpus did not include function statements")
	}
}

func TestDefaultWriteCorpusCoversCanonicalFileEntityContainment(t *testing.T) {
	t.Parallel()

	writeCases := DefaultWriteCorpus()
	var matched bool
	for _, tc := range writeCases {
		if tc.Name != "canonical file entity containment idempotency" {
			continue
		}
		matched = true
		if !tc.RequireAtomicGroup {
			t.Fatal("containment write case must require atomic visibility")
		}
		var sawFile, sawEntity, sawContains bool
		for _, stmt := range tc.Statements {
			if strings.Contains(stmt.Cypher, "MERGE (f:File") {
				sawFile = true
			}
			if strings.Contains(stmt.Cypher, "MERGE (n:Function") {
				sawEntity = true
			}
			if strings.Contains(stmt.Cypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
				sawContains = true
			}
		}
		if !sawFile || !sawEntity || !sawContains {
			t.Fatalf("containment case file/entity/edge coverage = %v/%v/%v, want all true", sawFile, sawEntity, sawContains)
		}
	}
	if !matched {
		t.Fatal("DefaultWriteCorpus() missing canonical file entity containment case")
	}
}

func TestDefaultReadCorpusVerifiesCanonicalFileEntityContainment(t *testing.T) {
	t.Parallel()

	var matched bool
	for _, tc := range DefaultReadCorpus() {
		if tc.Name != "canonical file entity containment readback" {
			continue
		}
		matched = true
		if tc.MinRows != 1 {
			t.Fatalf("containment read MinRows = %d, want 1", tc.MinRows)
		}
		if !strings.Contains(tc.Cypher, "contains_count = 1") {
			t.Fatalf("containment read query does not enforce idempotent relationship count: %s", tc.Cypher)
		}
		if !strings.Contains(tc.Cypher, "MATCH (f:File") || !strings.Contains(tc.Cypher, "CONTAINS") {
			t.Fatalf("containment read query does not verify File CONTAINS entity: %s", tc.Cypher)
		}
	}
	if !matched {
		t.Fatal("DefaultReadCorpus() missing canonical file entity containment readback")
	}
}

func TestDefaultWriteCorpusRunsPhaseGroupedCanonicalCases(t *testing.T) {
	t.Parallel()

	executor := &recordingPhaseCypherExecutor{}
	report, err := RunPhaseWriteCorpus(context.Background(), executor, DefaultWriteCorpus())
	if err != nil {
		t.Fatalf("RunPhaseWriteCorpus() error = %v", err)
	}
	if got, want := len(report.Results), len(DefaultWriteCorpus()); got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	if got, want := executor.phaseCalls, len(DefaultWriteCorpus()); got != want {
		t.Fatalf("phase call count = %d, want %d", got, want)
	}
}

func TestWriteCorpusRequiresGroupedExecutorWhenCaseRequiresAtomicVisibility(t *testing.T) {
	t.Parallel()

	cases := []WriteCase{{
		Name:                  "atomic relationship upsert",
		Capability:            CapabilityCanonicalWrites,
		RequireAtomicGroup:    true,
		TransactionVisibility: "all statements must commit together",
		Statements: []sourcecypher.Statement{
			{Operation: sourcecypher.OperationCanonicalUpsert, Cypher: "MERGE (a:Function {uid: $uid})", Parameters: map[string]any{"uid": "fn:a"}},
			{Operation: sourcecypher.OperationCanonicalUpsert, Cypher: "MERGE (b:Function {uid: $uid})", Parameters: map[string]any{"uid": "fn:b"}},
		},
	}}

	_, err := RunWriteCorpus(context.Background(), executeOnlyRecorder{}, cases)
	if err == nil {
		t.Fatal("RunWriteCorpus() error = nil, want missing grouped executor error")
	}
	if !strings.Contains(err.Error(), "requires grouped execution") {
		t.Fatalf("RunWriteCorpus() error = %v, want grouped execution context", err)
	}
}

func TestWriteCorpusReportsExecutorErrorsWithCaseName(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("backend unavailable")
	_, err := RunWriteCorpus(context.Background(), &recordingCypherExecutor{err: wantErr}, []WriteCase{{
		Name:       "canonical upsert",
		Capability: CapabilityCanonicalWrites,
		Statements: []sourcecypher.Statement{{
			Operation: sourcecypher.OperationCanonicalUpsert,
			Cypher:    "MERGE (r:Repository {id: $repo_id})",
			Parameters: map[string]any{
				"repo_id": "repo:backend-conformance",
			},
		}},
	}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunWriteCorpus() error = %v, want wrapped backend error", err)
	}
	if !strings.Contains(err.Error(), "canonical upsert") {
		t.Fatalf("RunWriteCorpus() error = %v, want case name", err)
	}
}

type recordingGraphQuery struct {
	calls []recordedGraphQueryCall
	rows  []map[string]any
	err   error
}

type recordedGraphQueryCall struct {
	cypher     string
	parameters map[string]any
}

func (q *recordingGraphQuery) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	q.calls = append(q.calls, recordedGraphQueryCall{cypher: cypher, parameters: params})
	return q.rows, q.err
}

func (q *recordingGraphQuery) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := q.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

type recordingCypherExecutor struct {
	singleCalls     int
	groupCalls      int
	groupStatements []sourcecypher.Statement
	err             error
}

func (e *recordingCypherExecutor) Execute(context.Context, sourcecypher.Statement) error {
	e.singleCalls++
	return e.err
}

func (e *recordingCypherExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	e.groupCalls++
	e.groupStatements = append(e.groupStatements, stmts...)
	return e.err
}

type executeOnlyRecorder struct{}

func (executeOnlyRecorder) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

type recordingPhaseCypherExecutor struct {
	phaseCalls int
	err        error
}

func (e *recordingPhaseCypherExecutor) ExecutePhaseGroup(context.Context, []sourcecypher.Statement) error {
	e.phaseCalls++
	return e.err
}
