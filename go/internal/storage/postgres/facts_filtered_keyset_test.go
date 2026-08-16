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

// makeConstantObservedAtFactRows builds rows that all carry one observed_at,
// which is what a real generation looks like: every fact is stamped with the
// same collection timestamp, so fact_id is the only column that can order or
// bound a page.
func makeConstantObservedAtFactRows(count int, offset int) [][]any {
	observedAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	rows := make([][]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, []any{
			fmt.Sprintf("fact-%06d", offset+i),
			"scope-123",
			"generation-456",
			"content_entity",
			"content_entity:repo-1:entity",
			"1.0.0",
			"git",
			int64(0),
			"unknown",
			"git",
			"fact-key",
			"file:///repo/path/main.go",
			"record-123",
			observedAt,
			false,
			[]byte(`{"repo_id":"repo-1","entity_id":"entity-1"}`),
		})
	}
	return rows
}

// Every fact in a generation is stamped with the same observed_at, so the
// keyset cursor's leading column never advances and the page can only be
// bounded by fact_id. Postgres will only use the (scope_id, generation_id,
// observed_at, fact_id) index for that bound when the row comparison is a
// plain predicate: wrapping it in `$4 IS NULL OR ...` demotes it to a filter
// under a generic plan, which is where database/sql prepared statements land.
// Then every page scans the whole generation again. See issue #6154.
const keysetGuardFragment = "IS NULL"

func TestListFactsByKindCursorQueryKeepsRowCompareIndexable(t *testing.T) {
	t.Parallel()

	if strings.Contains(listFactsByKindCursorQuery, keysetGuardFragment) {
		t.Fatalf(
			"cursor query must not guard the row comparison with IS NULL, "+
				"or the generic plan demotes it to a filter:\n%s",
			listFactsByKindCursorQuery,
		)
	}
	if !strings.Contains(
		listFactsByKindCursorQuery,
		"(observed_at, fact_id) > ($4::timestamptz, $5::text)",
	) {
		t.Fatalf("cursor query missing bare row comparison:\n%s", listFactsByKindCursorQuery)
	}
	if !strings.Contains(listFactsByKindCursorQuery, "ORDER BY observed_at ASC, fact_id ASC") {
		t.Fatalf("cursor query missing stable ordering:\n%s", listFactsByKindCursorQuery)
	}
}

func TestListFactsByKindFirstPageQueryOmitsCursorPredicate(t *testing.T) {
	t.Parallel()

	if strings.Contains(listFactsByKindQuery, keysetGuardFragment) {
		t.Fatalf("first-page query must not carry a cursor guard:\n%s", listFactsByKindQuery)
	}
	if strings.Contains(listFactsByKindQuery, "observed_at, fact_id) >") {
		t.Fatalf("first-page query must not carry a cursor predicate:\n%s", listFactsByKindQuery)
	}
	if !strings.Contains(listFactsByKindQuery, "ORDER BY observed_at ASC, fact_id ASC") {
		t.Fatalf("first-page query missing stable ordering:\n%s", listFactsByKindQuery)
	}
}

func TestListFactsByKindAndPayloadValueCursorQueryKeepsRowCompareIndexable(t *testing.T) {
	t.Parallel()

	if strings.Contains(listFactsByKindAndPayloadValueCursorQuery, keysetGuardFragment) {
		t.Fatalf(
			"payload-value cursor query must not guard the row comparison with IS NULL:\n%s",
			listFactsByKindAndPayloadValueCursorQuery,
		)
	}
	if !strings.Contains(
		listFactsByKindAndPayloadValueCursorQuery,
		"(observed_at, fact_id) > ($6::timestamptz, $7::text)",
	) {
		t.Fatalf(
			"payload-value cursor query missing bare row comparison:\n%s",
			listFactsByKindAndPayloadValueCursorQuery,
		)
	}
}

func TestListFactsByKindAndPayloadValueFirstPageQueryOmitsCursorPredicate(t *testing.T) {
	t.Parallel()

	if strings.Contains(listFactsByKindAndPayloadValueQuery, keysetGuardFragment) {
		t.Fatalf(
			"payload-value first-page query must not carry a cursor guard:\n%s",
			listFactsByKindAndPayloadValueQuery,
		)
	}
	if strings.Contains(listFactsByKindAndPayloadValueQuery, "observed_at, fact_id) >") {
		t.Fatalf(
			"payload-value first-page query must not carry a cursor predicate:\n%s",
			listFactsByKindAndPayloadValueQuery,
		)
	}
}

// The first page must not be charged for cursor parameters it cannot use, and
// the cursor page must receive exactly the cursor the previous page ended on.
func TestFactStoreListFactsByKindSplitsFirstAndCursorPages(t *testing.T) {
	t.Parallel()

	firstPage := makeFactRowsForListFactsByKind(factBatchSize, 0)
	secondPage := makeFactRowsForListFactsByKind(1, factBatchSize)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: firstPage},
			{rows: secondPage},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFactsByKind(
		context.Background(),
		"scope-123",
		"generation-456",
		[]string{"file"},
	)
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	if got, want := len(loaded), factBatchSize+1; got != want {
		t.Fatalf("ListFactsByKind() len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}

	first := db.queries[0]
	if strings.Contains(first.query, "observed_at, fact_id) >") {
		t.Fatalf("first page used the cursor query:\n%s", first.query)
	}
	if got, want := len(first.args), 4; got != want {
		t.Fatalf("first page arg count = %d, want %d", got, want)
	}
	if got, want := first.args[3], factBatchSize; got != want {
		t.Fatalf("first page size arg = %v, want %d", got, want)
	}

	second := db.queries[1]
	if !strings.Contains(second.query, "(observed_at, fact_id) > ($4::timestamptz, $5::text)") {
		t.Fatalf("cursor page missing row comparison:\n%s", second.query)
	}
	if got, want := len(second.args), 6; got != want {
		t.Fatalf("cursor page arg count = %d, want %d", got, want)
	}
	if got, want := second.args[3], loaded[factBatchSize-1].ObservedAt; got != want {
		t.Fatalf("cursor page timestamp = %v, want %v", got, want)
	}
	if got, want := second.args[4], loaded[factBatchSize-1].FactID; got != want {
		t.Fatalf("cursor page fact ID = %v, want %v", got, want)
	}
	if got, want := second.args[5], factBatchSize; got != want {
		t.Fatalf("cursor page size arg = %v, want %d", got, want)
	}
}

// The payload-value loader carries its own cursor parameters at different
// positions ($6/$7 cursor, $8 limit) than ListFactsByKind ($4/$5, $6). Its
// query text is asserted above, but text alone would not catch the arg slice
// being built in the wrong order at the call site: the statement would be
// valid, the types would line up, and the cursor would silently be wrong.
func TestFactStoreListFactsByKindAndPayloadValueSplitsFirstAndCursorPages(t *testing.T) {
	t.Parallel()

	firstPage := makeConstantObservedAtFactRows(factBatchSize, 0)
	secondPage := makeConstantObservedAtFactRows(3, factBatchSize)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: firstPage},
			{rows: secondPage},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFactsByKindAndPayloadValue(
		context.Background(),
		"scope-123",
		"generation-456",
		"content_entity",
		"repo_id",
		[]string{"repo-1"},
	)
	if err != nil {
		t.Fatalf("ListFactsByKindAndPayloadValue() error = %v, want nil", err)
	}
	if got, want := len(loaded), factBatchSize+3; got != want {
		t.Fatalf("ListFactsByKindAndPayloadValue() len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}

	first := db.queries[0]
	if strings.Contains(first.query, "observed_at, fact_id) >") {
		t.Fatalf("first page used the cursor query:\n%s", first.query)
	}
	if got, want := len(first.args), 6; got != want {
		t.Fatalf("first page arg count = %d, want %d", got, want)
	}
	if got, want := first.args[5], factBatchSize; got != want {
		t.Fatalf("first page limit arg = %v, want %d", got, want)
	}

	second := db.queries[1]
	if !strings.Contains(second.query, "(observed_at, fact_id) > ($6::timestamptz, $7::text)") {
		t.Fatalf("cursor page missing row comparison:\n%s", second.query)
	}
	if got, want := len(second.args), 8; got != want {
		t.Fatalf("cursor page arg count = %d, want %d", got, want)
	}
	// Positions matter as much as presence.
	if got, want := second.args[2], "content_entity"; got != want {
		t.Fatalf("cursor page fact kind arg = %v, want %v", got, want)
	}
	if got, want := second.args[3], "repo_id"; got != want {
		t.Fatalf("cursor page payload key arg = %v, want %v", got, want)
	}
	if got, want := second.args[5], loaded[factBatchSize-1].ObservedAt; got != want {
		t.Fatalf("cursor page timestamp arg = %v, want %v", got, want)
	}
	if got, want := second.args[6], loaded[factBatchSize-1].FactID; got != want {
		t.Fatalf("cursor page fact ID arg = %v, want %v", got, want)
	}
	if got, want := second.args[7], factBatchSize; got != want {
		t.Fatalf("cursor page limit arg = %v, want %d", got, want)
	}
}

// A generation whose facts all share one observed_at is the shape that made
// pagination quadratic; the split must still walk it to completion without
// dropping or repeating a row.
func TestFactStoreListFactsByKindPagesConstantObservedAtGeneration(t *testing.T) {
	t.Parallel()

	firstPage := makeConstantObservedAtFactRows(factBatchSize, 0)
	secondPage := makeConstantObservedAtFactRows(factBatchSize, factBatchSize)
	thirdPage := makeConstantObservedAtFactRows(7, 2*factBatchSize)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: firstPage},
			{rows: secondPage},
			{rows: thirdPage},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFactsByKind(
		context.Background(),
		"scope-123",
		"generation-456",
		[]string{"content_entity"},
	)
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	if got, want := len(loaded), 2*factBatchSize+7; got != want {
		t.Fatalf("ListFactsByKind() len = %d, want %d", got, want)
	}

	seen := make(map[string]struct{}, len(loaded))
	for _, envelope := range loaded {
		if _, duplicate := seen[envelope.FactID]; duplicate {
			t.Fatalf("fact %q returned twice", envelope.FactID)
		}
		seen[envelope.FactID] = struct{}{}
	}

	// Each cursor page must advance on fact_id, because observed_at cannot.
	for i := 1; i < len(db.queries); i++ {
		previousLast := loaded[i*factBatchSize-1]
		if got, want := db.queries[i].args[4], previousLast.FactID; got != want {
			t.Fatalf("page %d cursor fact ID = %v, want %v", i, got, want)
		}
		if got, want := db.queries[i].args[3], previousLast.ObservedAt; got != want {
			t.Fatalf("page %d cursor timestamp = %v, want %v", i, got, want)
		}
	}
}
