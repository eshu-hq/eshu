// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPostgresServiceCatalogCorrelationsResolveCandidateRepositoryIDs(t *testing.T) {
	t.Parallel()

	db, recorder := openServiceCatalogRecordingDB(t, []serviceCatalogRecordingResult{
		{
			columns: []string{"fact_id", "payload"},
			rows: [][]driver.Value{
				{
					"catalog-correlation-ambiguous",
					[]byte(`{
						"entity_ref": "component:default/payments-shared",
						"outcome": "ambiguous",
						"provenance_only": true,
						"candidate_repository_ids": ["repository:r_payments", "repository:r_payments_fork"]
					}`),
				},
			},
		},
	})
	store := NewPostgresServiceCatalogCorrelationStore(db)

	rows, err := store.ListServiceCatalogCorrelations(context.Background(), CatalogCorrelationFilter{
		RepositoryID: "repository:r_payments",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("ListServiceCatalogCorrelations() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	wantCandidates := []string{"repository:r_payments", "repository:r_payments_fork"}
	if got := rows[0].CandidateRepositoryIDs; !slices.Equal(got, wantCandidates) {
		t.Fatalf("CandidateRepositoryIDs = %#v, want %#v", got, wantCandidates)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(queries) = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "fact.payload->'candidate_repository_ids' ? $5") {
		t.Fatalf("query missing candidate repository predicate:\n%s", recorder.queries[0])
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
		"fact.payload->'candidate_repository_ids' ? $5",
		"fact.payload->>'owner_ref' = $8",
		"fact.payload->>'outcome' = $9",
	} {
		if !strings.Contains(ListServiceCatalogCorrelationsQuery, want) {
			t.Fatalf("ListServiceCatalogCorrelationsQuery missing %q:\n%s", want, ListServiceCatalogCorrelationsQuery)
		}
	}
}

func TestServiceCatalogLocalDescriptorEvidenceQueryUsesActiveRepositoryScope(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.scope_id = $1",
		"fact.fact_kind = ANY($2::text[])",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"ORDER BY COALESCE(fact.source_uri, ''), fact.fact_kind, fact.fact_id",
	} {
		if !strings.Contains(ListServiceCatalogLocalDescriptorEvidenceQuery, want) {
			t.Fatalf("ListServiceCatalogLocalDescriptorEvidenceQuery missing %q:\n%s", want, ListServiceCatalogLocalDescriptorEvidenceQuery)
		}
	}
}

// serviceCatalogRecordingResult is one canned answer for
// openServiceCatalogRecordingDB: the columns and rows the next captured query
// receives, or err to fail it.
type serviceCatalogRecordingResult struct {
	columns []string
	rows    [][]driver.Value
	err     error
}

// serviceCatalogQueryRecorder captures every statement the store under test
// sends. It mirrors the staying root recordingContentReader helper without
// importing root test files (a symbol declared in a _test.go file cannot
// cross a package boundary).
type serviceCatalogQueryRecorder struct {
	mu      sync.Mutex
	results []serviceCatalogRecordingResult
	queries []string
}

func openServiceCatalogRecordingDB(t *testing.T, results []serviceCatalogRecordingResult) (*sql.DB, *serviceCatalogQueryRecorder) {
	t.Helper()

	name := fmt.Sprintf("service-catalog-recording-test-%d", atomic.AddUint64(&serviceCatalogRecordingDriverSeq, 1))
	recorder := &serviceCatalogQueryRecorder{results: append([]serviceCatalogRecordingResult(nil), results...)}
	sql.Register(name, &serviceCatalogRecordingDriver{recorder: recorder})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db, recorder
}

var serviceCatalogRecordingDriverSeq uint64

type serviceCatalogRecordingDriver struct {
	recorder *serviceCatalogQueryRecorder
}

func (d *serviceCatalogRecordingDriver) Open(string) (driver.Conn, error) {
	return &serviceCatalogRecordingConn{recorder: d.recorder}, nil
}

type serviceCatalogRecordingConn struct {
	recorder *serviceCatalogQueryRecorder
}

func (c *serviceCatalogRecordingConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *serviceCatalogRecordingConn) Close() error {
	return nil
}

func (c *serviceCatalogRecordingConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *serviceCatalogRecordingConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.recorder.mu.Lock()
	defer c.recorder.mu.Unlock()

	c.recorder.queries = append(c.recorder.queries, query)
	if len(c.recorder.results) == 0 {
		return nil, fmt.Errorf("unexpected query")
	}
	result := c.recorder.results[0]
	c.recorder.results = c.recorder.results[1:]
	if result.err != nil {
		return nil, result.err
	}
	return &serviceCatalogRecordingRows{columns: result.columns, rows: result.rows}, nil
}

type serviceCatalogRecordingRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *serviceCatalogRecordingRows) Columns() []string {
	return r.columns
}

func (r *serviceCatalogRecordingRows) Close() error {
	return nil
}

func (r *serviceCatalogRecordingRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
