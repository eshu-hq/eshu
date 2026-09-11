// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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

	rows, err := store.ListServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationFilter{
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

// TestServiceCatalogCorrelationsOutsideGrantQueryDiffersOnlyInTheGrantClause is
// the lockstep pin for the two shipped statements. The outside-grant read
// exists to answer one question -- does anything OUTSIDE the caller's grant
// also correlate this service id -- and it must answer it over exactly the
// same rows the ordinary read considers. Anything else (a dropped tombstone
// arm, a missing active-generation join, a different selector) would make the
// exclusivity check answer about a different population than the admission
// check, which is how a fail-open slips back in.
func TestServiceCatalogCorrelationsOutsideGrantQueryDiffersOnlyInTheGrantClause(t *testing.T) {
	t.Parallel()

	const grantClause = `  AND (
    (COALESCE(cardinality($13::text[]), 0) = 0 AND COALESCE(cardinality($14::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($13::text[])
    OR fact.payload->'candidate_repository_ids' ?| $13::text[]
    OR fact.scope_id = ANY($14::text[])
  )`
	// Not the plain negation of the clause above: a row is inside only when
	// the grant covers some of its ownership evidence AND no candidate falls
	// outside the grant (#6472 review, P1-B). The containment test is the
	// half that separates an ambiguous row the caller wholly owns from one
	// that also names a repository it does not.
	const inverseGrantClause = `  AND NOT (
    (
      (
        COALESCE(fact.payload->>'repository_id' = ANY($13::text[]), FALSE)
        OR COALESCE(fact.payload->'candidate_repository_ids' ?| $13::text[], FALSE)
      )
      AND COALESCE(fact.payload->'candidate_repository_ids' <@ to_jsonb($13::text[]), TRUE)
    )
    OR fact.scope_id = ANY($14::text[])
  )`

	if !strings.Contains(ListServiceCatalogCorrelationsQuery, grantClause) {
		t.Fatalf("ListServiceCatalogCorrelationsQuery no longer carries the grant clause:\n%s", ListServiceCatalogCorrelationsQuery)
	}
	if !strings.Contains(listServiceCatalogCorrelationsOutsideGrantQuery, inverseGrantClause) {
		t.Fatalf("listServiceCatalogCorrelationsOutsideGrantQuery no longer carries the inverted grant clause:\n%s",
			listServiceCatalogCorrelationsOutsideGrantQuery)
	}
	// The inverted clause drops the empty-arrays arm on purpose: an empty
	// grant makes the ordinary clause permissive, and its negation would
	// refuse every service. ListServiceCatalogCorrelations rejects that filter
	// before it reaches SQL instead.
	restored := strings.Replace(listServiceCatalogCorrelationsOutsideGrantQuery, inverseGrantClause, grantClause, 1)
	if restored != ListServiceCatalogCorrelationsQuery {
		t.Fatalf("the two statements differ outside the grant clause:\n--- ordinary ---\n%s\n--- outside-grant, grant clause restored ---\n%s",
			ListServiceCatalogCorrelationsQuery, restored)
	}
}

// TestPostgresServiceCatalogCorrelationsSelectTheStatementByOutsideGrant pins
// which statement each filter shape sends. The ordinary read must stay
// byte-identical to what it sent before the outside-grant probe existed: every
// other caller of this store shares its plan cache entry.
func TestPostgresServiceCatalogCorrelationsSelectTheStatementByOutsideGrant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		outsideGrant bool
		wantQuery    string
	}{
		{name: "ordinary read", wantQuery: ListServiceCatalogCorrelationsQuery},
		{name: "outside-grant read", outsideGrant: true, wantQuery: listServiceCatalogCorrelationsOutsideGrantQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, recorder := openServiceCatalogRecordingDB(t, []serviceCatalogRecordingResult{
				{columns: []string{"fact_id", "payload"}},
			})
			store := NewPostgresServiceCatalogCorrelationStore(db)

			if _, err := store.ListServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationFilter{
				ServiceID:            "component:default/api",
				AllowedRepositoryIDs: []string{"repository:r_alpha"},
				OutsideGrant:         tc.outsideGrant,
				Limit:                1,
			}); err != nil {
				t.Fatalf("ListServiceCatalogCorrelations() error = %v, want nil", err)
			}
			if got, want := len(recorder.queries), 1; got != want {
				t.Fatalf("len(queries) = %d, want %d", got, want)
			}
			if recorder.queries[0] != tc.wantQuery {
				t.Fatalf("statement sent:\n%s\nwant:\n%s", recorder.queries[0], tc.wantQuery)
			}
		})
	}
}

// TestPostgresServiceCatalogCorrelationsRejectAnOutsideGrantReadWithNoGrant
// pins the fail-loud guard. Negating the grant clause over two empty arrays
// matches every row, so a caller that lost its grant on the way here would
// refuse every service and read as ordinary tenant isolation. The store
// refuses to run instead, and issues no statement at all.
func TestPostgresServiceCatalogCorrelationsRejectAnOutsideGrantReadWithNoGrant(t *testing.T) {
	t.Parallel()

	db, recorder := openServiceCatalogRecordingDB(t, nil)
	store := NewPostgresServiceCatalogCorrelationStore(db)

	_, err := store.ListServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationFilter{
		ServiceID:    "component:default/api",
		OutsideGrant: true,
		Limit:        1,
	})
	if !errors.Is(err, ErrServiceCatalogOutsideGrantNeedsAGrant) {
		t.Fatalf("ListServiceCatalogCorrelations() error = %v, want %v", err, ErrServiceCatalogOutsideGrantNeedsAGrant)
	}
	if got, want := len(recorder.queries), 0; got != want {
		t.Fatalf("len(queries) = %d, want %d; the guard must refuse before any statement is sent", got, want)
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
