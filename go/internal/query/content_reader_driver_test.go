// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// contentReaderQueryResult is this package's view of one queued answer for the
// shared fake SQL driver. The behavior lives in querytestutil; this type exists
// only so the 80-odd test files that build it with keyed literals over these
// lowercase field names keep compiling (#6060).
//
// A type alias to querytestutil.ContentReaderQueryResult would not do. Its
// fields have to be exported to be settable from another package, so an alias
// would carry the type and force every one of those literals to be renamed. The
// adapter converts instead, and the field names stay put.
type contentReaderQueryResult struct {
	columns              []string
	rows                 [][]driver.Value
	err                  error
	queryContains        []string
	queryContainsInOrder []string
	// wantArgs, when non-nil, asserts the exact positional bind values
	// QueryContext receives, in order, including type (e.g. int64 vs string
	// after driver.DefaultParameterConverter runs). Catches an argument-order
	// swap that leaves the query TEXT byte-identical -- queryContains and
	// queryContainsInOrder only inspect the SQL string, so a swapped
	// $1/$2/$3 binding is invisible to both (#5764 round-8 P2-2 review
	// follow-up).
	wantArgs []driver.Value
}

// shared converts this package's result into the shared one.
func (r contentReaderQueryResult) shared() querytestutil.ContentReaderQueryResult {
	return querytestutil.ContentReaderQueryResult{
		Columns:              r.columns,
		Rows:                 r.rows,
		Err:                  r.err,
		QueryContains:        r.queryContains,
		QueryContainsInOrder: r.queryContainsInOrder,
		WantArgs:             r.wantArgs,
	}
}

// openContentReaderTestDB opens a *sql.DB backed by the shared fake driver,
// answering from results in order.
//
// It converts the queued results and delegates. The dispatch rules -- which
// incidental reads get a default answer, when a queued result outranks one, how
// the query text and bind values are asserted -- are deliberately not repeated
// here. Two copies drift, and a fake that no longer matches the reader it
// stands in for keeps passing while guarding nothing.
func openContentReaderTestDB(t *testing.T, results []contentReaderQueryResult) *sql.DB {
	t.Helper()

	shared := make([]querytestutil.ContentReaderQueryResult, 0, len(results))
	for _, result := range results {
		shared = append(shared, result.shared())
	}
	return querytestutil.OpenContentReaderTestDB(t, shared)
}

// contentReaderQueryContainsInOrder asserts each fragment appears in query
// after the previous one, for a test holding a recorded query string rather
// than a queued result.
func contentReaderQueryContainsInOrder(query string, fragments []string) error {
	return querytestutil.ContentReaderQueryContainsInOrder(query, fragments)
}

// contentReaderRelationshipReadModelColumns returns the repository relationship
// read model's columns, in order.
func contentReaderRelationshipReadModelColumns() []string {
	return querytestutil.ContentReaderRelationshipReadModelColumns()
}

// contentReaderDeploymentEvidenceColumns returns the repository
// deployment-evidence read's columns, in order.
func contentReaderDeploymentEvidenceColumns() []string {
	return querytestutil.ContentReaderDeploymentEvidenceColumns()
}

// contentReaderRelationshipEvidenceColumns returns the relationship evidence
// read's columns, in order.
func contentReaderRelationshipEvidenceColumns() []string {
	return querytestutil.ContentReaderRelationshipEvidenceColumns()
}

// contentReaderDeadCodeCandidateColumns returns the dead-code candidate scan's
// columns, in order.
func contentReaderDeadCodeCandidateColumns() []string {
	return querytestutil.ContentReaderDeadCodeCandidateColumns()
}
