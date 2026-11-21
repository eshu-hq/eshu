// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestFactStoreListActivePackageOwnershipFactsSplitsFirstAndCursorPages pins
// the #11082 statement shape: separate first-page and cursor statements with
// a plain row comparison, driven from fact_records through a MATERIALIZED CTE
// with no inner LIMIT. Every assertion derives from the shipped consts, never
// from a hand-copied query, so the test tracks the statements it guards.
//
// Folding both pages into one `$1 IS NULL OR ...` statement demotes the
// comparison to a filter under the generic plan database/sql prepares, and a
// pre-join LIMIT inside the CTE would silently drop active rows past the
// truncated pre-join set because the paging loop stops on a short post-join
// page (arm C measured 204 of 1200 rows before stopping).
func TestFactStoreListActivePackageOwnershipFactsSplitsFirstAndCursorPages(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"first":  listActivePackageOwnershipFactsFirstQuery,
		"cursor": listActivePackageOwnershipFactsCursorQuery,
	} {
		if strings.Contains(query, "IS NULL") {
			t.Fatalf("%s page folds a nullable cursor predicate; keep first and cursor statements split:\n%s", name, query)
		}
		if !strings.Contains(query, "AS MATERIALIZED") {
			t.Fatalf("%s page must stay MATERIALIZED: without it Postgres flattens the CTE into the generation-driven nested loop (60.7ms vs 7.5ms per page on the #11082 fixture):\n%s", name, query)
		}
		cte, _, found := strings.Cut(query, "FROM matching")
		if !found {
			t.Fatalf("%s page lost its drive-from-fact CTE shape:\n%s", name, query)
		}
		if strings.Contains(cte, "LIMIT") {
			t.Fatalf("%s page carries an inner LIMIT before the active-generation join; the paging loop stops on a short post-join page and would silently drop rows:\n%s", name, query)
		}
	}

	if strings.Contains(listActivePackageOwnershipFactsFirstQuery, "$2") {
		t.Fatalf("first page must bind only its limit:\n%s", listActivePackageOwnershipFactsFirstQuery)
	}
	if !strings.Contains(listActivePackageOwnershipFactsFirstQuery, "LIMIT $1") {
		t.Fatalf("first page missing page limit:\n%s", listActivePackageOwnershipFactsFirstQuery)
	}
	if !strings.Contains(listActivePackageOwnershipFactsCursorQuery, "(fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)") {
		t.Fatalf("cursor page CTE missing stable pre-join cursor:\n%s", listActivePackageOwnershipFactsCursorQuery)
	}
	if !strings.Contains(listActivePackageOwnershipFactsCursorQuery, "AND (matching.observed_at, matching.fact_id) > ($1::timestamptz, $2::text)") {
		t.Fatalf("cursor page missing stable post-join cursor:\n%s", listActivePackageOwnershipFactsCursorQuery)
	}
	if !strings.Contains(listActivePackageOwnershipFactsCursorQuery, "LIMIT $3") {
		t.Fatalf("cursor page missing page limit:\n%s", listActivePackageOwnershipFactsCursorQuery)
	}
}

func TestFactStoreListActivePackageOwnershipFactsPagesToExhaustion(t *testing.T) {
	t.Parallel()

	firstPage := makeActivePackageOwnershipRows(factBatchSize, 0)
	secondPage := makeActivePackageOwnershipRows(1, factBatchSize)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: firstPage},
			{rows: secondPage},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActivePackageOwnershipFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActivePackageOwnershipFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), factBatchSize+1; got != want {
		t.Fatalf("ListActivePackageOwnershipFacts() len = %d, want %d", got, want)
	}
	// Stable order survives pagination: rows arrive in (observed_at, fact_id)
	// order across the page boundary.
	for i := 1; i < len(loaded); i++ {
		prev, curr := loaded[i-1], loaded[i]
		if prev.ObservedAt.After(curr.ObservedAt) ||
			(prev.ObservedAt.Equal(curr.ObservedAt) && prev.FactID >= curr.FactID) {
			t.Fatalf("row %d out of order: (%v,%q) before (%v,%q)",
				i, prev.ObservedAt, prev.FactID, curr.ObservedAt, curr.FactID)
		}
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}

	// The first page binds no cursor, so it runs the cursor-free statement
	// with a single limit arg. Only later pages carry a cursor.
	if !strings.Contains(db.queries[0].query, "LIMIT $1") {
		t.Fatalf("first page missing page limit:\n%s", db.queries[0].query)
	}
	if got, want := len(db.queries[0].args), 1; got != want {
		t.Fatalf("first page arg count = %d, want %d", got, want)
	}
	if got, want := db.queries[0].args[0], factBatchSize; got != want {
		t.Fatalf("first page size arg = %v, want %d", got, want)
	}
	for _, call := range db.queries[1:] {
		if !strings.Contains(call.query, "LIMIT $3") {
			t.Fatalf("cursor page missing page limit:\n%s", call.query)
		}
		if !strings.Contains(call.query, "(fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)") {
			t.Fatalf("cursor page missing stable cursor:\n%s", call.query)
		}
		if got, want := call.args[2], factBatchSize; got != want {
			t.Fatalf("page size arg = %v, want %d", got, want)
		}
	}
	if got, want := db.queries[1].args[0], loaded[factBatchSize-1].ObservedAt; got != want {
		t.Fatalf("second page cursor timestamp = %v, want %v", got, want)
	}
	if got, want := db.queries[1].args[1], loaded[factBatchSize-1].FactID; got != want {
		t.Fatalf("second page cursor fact ID = %v, want %v", got, want)
	}
}

// makeActivePackageOwnershipRows builds count positional fact rows in stable
// (observed_at, fact_id) order starting at start, mirroring the projection
// order ScanFactEnvelope reads.
func makeActivePackageOwnershipRows(count int, start int) [][]any {
	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	rows := make([][]any, 0, count)
	for i := 0; i < count; i++ {
		n := start + i
		rows = append(rows, []any{
			fmt.Sprintf("fact-ownership-%06d", n),
			"scope-ownership",
			"generation-ownership",
			"package_registry.package",
			"package_registry.package:demo",
			"1.0.0",
			"package-registry",
			int64(0),
			"unknown",
			"package-registry",
			"ownership-demo",
			"",
			"",
			base.Add(time.Duration(n) * time.Second),
			false,
			[]byte(`{"ecosystem":"npm","name":"demo"}`),
		})
	}
	return rows
}
