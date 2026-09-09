// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// consumerSelectorProducerEntity is the single dead-code candidate
// deadCodeGrantContentStore returns for the granted producer repository.
const consumerSelectorProducerEntity = codeGrantGrantedRepo + "#unusedHelper"

// TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository is the
// case the consumer selector exists to answer and the one it used to lose.
//
// The caller is granted three repositories and asks about consumers in exactly
// one of them. Another granted repository consumes the same symbol a thousand
// times over. The page read was bound to the whole grant, so those thousand
// rows filled it, the requested consumer's own row fell off the end, and the
// candidate came back unknown_needs_evidence with consumer_evidence_truncated
// -- for a symbol the requested consumer proves live.
//
// Binding the page to the requested consumers puts the row cap where the
// question is. The reader here is the shipped ContentReader over a driver that
// filters on the repository array the statement actually binds, so a page read
// that stops binding the selector fails this test rather than passing on a fake
// that filters for it.
func TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository(t *testing.T) {
	t.Parallel()

	db, recorder := openFilteringCrossRepoDeadCodeDB(t, crossRepoDeadCodeConsumerRowSet(
		consumerSelectorProducerEntity,
		maxCrossRepoDeadCodeConsumerEvidenceRows+1,
	))
	store := &crossRepoDeadCodeSelectorStore{reader: NewContentReader(db)}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantConsumerRepo, codeGrantOtherRepo})
	req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code/cross-repo", map[string]any{
		"repo_id":           codeGrantGrantedRepo,
		"language":          "go",
		"consumer_repo_ids": []string{codeGrantConsumerRepo},
	}, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	buckets := decodeEnvelopeData(t, rec.Body.Bytes())["candidate_buckets"].(map[string]any)
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "unknown", consumerSelectorProducerEntity)
	live := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", consumerSelectorProducerEntity)
	if got, want := live["classification"], "live_by_consumer"; got != want {
		t.Fatalf("classification = %#v, want %#v; the requested consumer's own row must reach the page", got, want)
	}
	if strings.Contains(rec.Body.String(), "consumer_evidence_truncated") {
		t.Fatalf("the page was cut by a repository the caller did not ask about: %s", rec.Body.String())
	}

	if got, want := len(recorder.queries()), 1; got != want {
		t.Fatalf("consumer statements = %d, want %d; a request naming consumers gets nothing from the signal read", got, want)
	}
	bound := recorder.boundRepositories(0)
	if !strings.Contains(bound, codeGrantConsumerRepo) {
		t.Fatalf("page read bound %q, want the requested consumer %q", bound, codeGrantConsumerRepo)
	}
	if strings.Contains(bound, codeGrantOtherRepo) {
		t.Fatalf("page read bound %q, want only the requested consumers -- %q is what filled the page", bound, codeGrantOtherRepo)
	}
}

// TestCrossRepoDeadCodeConsumerReadPlan moved to
// codequery/code_dead_code_cross_repo_read_plan_test.go at the #6060 move:
// it exercises crossRepoDeadCodeConsumerReadPlan directly and has no
// dependency on the root *ContentReader the rest of this file needs.

// crossRepoDeadCodeConsumerRowSet builds the rows the database returns in its
// ORDER BY order for one producer entity: noise from a granted repository the
// caller did not ask about, then one strong row from the requested consumer.
func crossRepoDeadCodeConsumerRowSet(entityID string, noiseRows int) []filteringCrossRepoDeadCodeRow {
	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	row := func(consumerRepoID string, confidence float64) filteringCrossRepoDeadCodeRow {
		return filteringCrossRepoDeadCodeRow{
			consumerRepoID: consumerRepoID,
			values: []driver.Value{
				entityID, consumerRepoID, "", consumerRepoID + "#caller",
				int64(1), "reachable", confidence, codeprovenance.MethodImportBinding,
				[]byte(`["CALLS:` + consumerRepoID + `#caller->` + entityID + `"]`),
				[]byte(`["go.main_function"]`),
				"gen-a", "active", observedAt, observedAt,
			},
		}
	}
	rows := make([]filteringCrossRepoDeadCodeRow, 0, noiseRows+1)
	for i := 0; i < noiseRows; i++ {
		rows = append(rows, row(codeGrantOtherRepo, 0.99))
	}
	return append(rows, row(codeGrantConsumerRepo, 0.95))
}

// deadCodeGrantContentStore answers the producer candidate scan for the
// granted and other repositories. It is a local copy of the fake the
// CodeHandler-family grant suite uses (codequery/auth_scoped_code_dead_code_grant_test.go):
// a _test.go symbol is not importable across the package/codequery boundary,
// and this file needs the real *ContentReader for its consumer-side read
// (below), which cannot move to codequery without recreating ContentReader
// there. See #6060.
type deadCodeGrantContentStore struct {
	querytestutil.FakePortContentStore
	bound   []string
	queried bool
}

func (s *deadCodeGrantContentStore) DeadCodeCandidateRows(
	_ context.Context,
	q codeshaping.DeadCodeCandidateQuery,
) ([]map[string]any, error) {
	s.queried = true
	s.bound = append([]string(nil), q.AllowedRepositoryIDs...)
	if q.Label != "Function" || q.Offset > 0 {
		return nil, nil
	}
	rows := make([]map[string]any, 0, 2)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(repoID, q.RepoID, q.AllowedRepositoryIDs) {
			continue
		}
		rows = append(rows, map[string]any{
			"entity_id":  repoID + "#unusedHelper",
			"name":       "unusedHelper",
			"labels":     []any{"Function"},
			"file_path":  "internal/legacy/helper.go",
			"repo_id":    repoID,
			"repo_name":  repoID,
			"language":   "go",
			"start_line": 4,
			"end_line":   9,
		})
	}
	return rows, nil
}

// crossRepoDeadCodeSelectorStore answers the producer candidate scan from the
// shared grant fake and the consumer read from the shipped ContentReader, so
// the route's statement, its LIMIT, and its truncation marker are the real ones.
type crossRepoDeadCodeSelectorStore struct {
	deadCodeGrantContentStore
	reader *ContentReader
}

func (s *crossRepoDeadCodeSelectorStore) CrossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	reads crossRepoDeadCodeConsumerReads,
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, crossRepoDeadCodeHiddenConsumers, error) {
	return s.reader.CrossRepoDeadCodeConsumerEvidence(ctx, producerRepoID, entityIDs, reads)
}

// filteringCrossRepoDeadCodeRow is one consumer-evidence row plus the consumer
// repository the statement's grant array is matched against.
type filteringCrossRepoDeadCodeRow struct {
	consumerRepoID string
	values         []driver.Value
}

// filteringCrossRepoDeadCodeRecorder stands in for Postgres for the
// consumer-evidence statement: it applies the `row.repository_id = ANY($n)`
// membership the statement binds and returns the surviving rows in the order
// given. It does not apply the statement's own LIMIT, because the reader drops
// every row past its 1,001-row sentinel itself, so the rows the caller observes
// are the same either way.
type filteringCrossRepoDeadCodeRecorder struct {
	mu          sync.Mutex
	rows        []filteringCrossRepoDeadCodeRow
	seenQueries []string
	boundArrays []string
}

func (r *filteringCrossRepoDeadCodeRecorder) queries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seenQueries...)
}

// boundRepositories returns the encoded repository array the nth recorded
// consumer statement bound, or "" when it bound none.
func (r *filteringCrossRepoDeadCodeRecorder) boundRepositories(index int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index >= len(r.boundArrays) {
		return ""
	}
	return r.boundArrays[index]
}

func openFilteringCrossRepoDeadCodeDB(
	t *testing.T,
	rows []filteringCrossRepoDeadCodeRow,
) (*sql.DB, *filteringCrossRepoDeadCodeRecorder) {
	t.Helper()

	recorder := &filteringCrossRepoDeadCodeRecorder{rows: rows}
	name := fmt.Sprintf("cross-repo-dead-code-filtering-%d", atomic.AddUint64(&filteringCrossRepoDeadCodeSeq, 1))
	sql.Register(name, &filteringCrossRepoDeadCodeDriver{recorder: recorder})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, recorder
}

var filteringCrossRepoDeadCodeSeq uint64

type filteringCrossRepoDeadCodeDriver struct {
	recorder *filteringCrossRepoDeadCodeRecorder
}

func (d *filteringCrossRepoDeadCodeDriver) Open(string) (driver.Conn, error) {
	return &filteringCrossRepoDeadCodeConn{recorder: d.recorder}, nil
}

type filteringCrossRepoDeadCodeConn struct {
	recorder *filteringCrossRepoDeadCodeRecorder
}

func (c *filteringCrossRepoDeadCodeConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *filteringCrossRepoDeadCodeConn) Close() error { return nil }

func (c *filteringCrossRepoDeadCodeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *filteringCrossRepoDeadCodeConn) QueryContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Rows, error) {
	if !strings.Contains(query, "FROM code_reachability_rows AS row") {
		return &filteringCrossRepoDeadCodeRows{columns: []string{"available"}, rows: [][]driver.Value{{false}}}, nil
	}
	bound := ""
	if strings.Contains(query, "row.repository_id = ANY(") && len(args) > 0 {
		bound = fmt.Sprintf("%s", args[len(args)-1].Value)
	}
	c.recorder.mu.Lock()
	c.recorder.seenQueries = append(c.recorder.seenQueries, query)
	c.recorder.boundArrays = append(c.recorder.boundArrays, bound)
	source := c.recorder.rows
	c.recorder.mu.Unlock()

	values := make([][]driver.Value, 0, len(source))
	for _, row := range source {
		if bound != "" && !strings.Contains(bound, row.consumerRepoID) {
			continue
		}
		values = append(values, row.values)
	}
	return &filteringCrossRepoDeadCodeRows{columns: crossRepoDeadCodeEvidenceColumns(), rows: values}, nil
}

type filteringCrossRepoDeadCodeRows struct {
	columns []string
	rows    [][]driver.Value
	cursor  int
}

func (r *filteringCrossRepoDeadCodeRows) Columns() []string { return r.columns }

func (r *filteringCrossRepoDeadCodeRows) Close() error { return nil }

func (r *filteringCrossRepoDeadCodeRows) Next(dest []driver.Value) error {
	if r.cursor >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.cursor])
	r.cursor++
	return nil
}
