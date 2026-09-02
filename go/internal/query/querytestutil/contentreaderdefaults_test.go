// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// queryColumns runs one query and returns the column names the fake answered
// with, which is how a test tells a default answer apart from a queued one.
func queryColumns(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("QueryContext(%q) error = %v, want nil", query, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns() error = %v, want nil", err)
	}
	return columns
}

// TestOpenContentReaderTestDBAnswersIncidentalReadsWithoutConsumingAQueuedResult
// pins the reason the default answers exist. A handler under test issues
// incidental reads -- a readiness probe, a language rollup -- that the test did
// not queue a result for. Those must be answered with an empty row set of the
// right shape AND must leave the queue untouched, or every queued expectation
// after them lines up against the wrong query.
func TestOpenContentReaderTestDBAnswersIncidentalReadsWithoutConsumingAQueuedResult(t *testing.T) {
	t.Parallel()

	db := querytestutil.OpenContentReaderTestDB(t, []querytestutil.ContentReaderQueryResult{
		{Columns: []string{"label"}, Rows: [][]driver.Value{{"queued"}}},
	})

	got := queryColumns(t, db, "SELECT count(*) FROM content_files WHERE repo_id = $1", "repo-1")
	if len(got) != 1 || got[0] != "count" {
		t.Fatalf("incidental read columns = %v, want [count]", got)
	}

	if label := scanSingleString(t, db, "SELECT label FROM widgets"); label != "queued" {
		t.Fatalf("label = %q, want the queued result still at the head of the queue", label)
	}
}

// TestOpenContentReaderTestDBPrefersAQueuedResultShapedLikeTheDefault covers the
// other half of that rule. A test that genuinely asserts on one of the default
// queries declares the default's own columns, and the queued rows must win --
// otherwise the assertion would be answered by the empty default and pass
// against nothing.
func TestOpenContentReaderTestDBPrefersAQueuedResultShapedLikeTheDefault(t *testing.T) {
	t.Parallel()

	db := querytestutil.OpenContentReaderTestDB(t, []querytestutil.ContentReaderQueryResult{
		{Columns: []string{"count"}, Rows: [][]driver.Value{{int64(7)}}},
	})

	rows, err := db.QueryContext(context.Background(), "SELECT count(*) FROM content_files WHERE repo_id = $1", "repo-1")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		t.Fatalf("QueryContext() returned no rows, want the queued count row")
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if count != 7 {
		t.Fatalf("count = %d, want the queued 7 rather than the default 0", count)
	}
}

// TestOpenContentReaderTestDBDefaultsMatchTheirColumnHelpers ties each read
// model's default answer to the exported column helper tests build their queued
// results from. If the two drift, a test declares one shape and the fake answers
// with another, and the mismatch surfaces as a scan error inside a handler
// rather than as a harness defect.
func TestOpenContentReaderTestDBDefaultsMatchTheirColumnHelpers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		query string
		args  []any
		want  []string
	}{
		{
			name:  "relationship read model",
			query: "WITH scoped_relationships AS (SELECT 1) SELECT r.evidence_count FROM scoped_relationships r",
			want:  querytestutil.ContentReaderRelationshipReadModelColumns(),
		},
		{
			name:  "deployment evidence",
			query: "WITH scoped_relationships AS (SELECT 1) SELECT r.details FROM scoped_relationships r",
			want:  querytestutil.ContentReaderDeploymentEvidenceColumns(),
		},
		{
			name:  "relationship evidence",
			query: "SELECT r.resolved_id FROM resolved_relationships r WHERE r.resolved_id = $1",
			args:  []any{"resolved-1"},
			want:  querytestutil.ContentReaderRelationshipEvidenceColumns(),
		},
		{
			name: "dead code candidates",
			query: "SELECT entity_id FROM content_entities WHERE repo_id = $1 AND entity_type = $2 " +
				"ORDER BY entity_id LIMIT $3 OFFSET $4",
			args: []any{"repo-1", "Class", 10, 0},
			want: querytestutil.ContentReaderDeadCodeCandidateColumns(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			db := querytestutil.OpenContentReaderTestDB(t, nil)
			got := queryColumns(t, db, testCase.query, testCase.args...)
			if len(got) != len(testCase.want) {
				t.Fatalf("columns = %v, want %v", got, testCase.want)
			}
			for i, want := range testCase.want {
				if got[i] != want {
					t.Fatalf("columns[%d] = %q, want %q (full set %v)", i, got[i], want, got)
				}
			}
		})
	}
}
