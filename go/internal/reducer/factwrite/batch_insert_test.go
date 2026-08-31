// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

import (
	"context"
	"database/sql"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// recordingExecer captures the statements and argument counts a batch produces,
// so the chunking and argument shape can be asserted without a live database.
type recordingExecer struct {
	queries   []string
	argCounts []int
	err       error
}

func (r *recordingExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.queries = append(r.queries, query)
	r.argCounts = append(r.argCounts, len(args))
	return nil, r.err
}

// TestDedupeRowsByFactIDKeepsTheLastOccurrence pins the rule that makes a batch
// safe: two rows sharing a fact ID collide on the ON CONFLICT target and fail
// the whole chunk, so the writer keeps the last one. Reversing this to
// first-wins would silently reinstate a stale value when a caller corrects a
// row later in the same batch.
func TestDedupeRowsByFactIDKeepsTheLastOccurrence(t *testing.T) {
	t.Parallel()

	type row struct{ id, payload string }
	key := func(r row) string { return r.id }

	got := DedupeRowsByFactID([]row{
		{id: "a", payload: "stale"},
		{id: "b", payload: "b1"},
		{id: "a", payload: "corrected"},
	}, key)

	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %v", len(got), got)
	}
	// order is preserved, and the surviving "a" is the corrected one
	if got[0].id != "b" || got[1].id != "a" {
		t.Fatalf("order not preserved: %v", got)
	}
	if got[1].payload != "corrected" {
		t.Errorf("kept the stale row: got %q, want %q", got[1].payload, "corrected")
	}
}

// TestDedupeRowsByFactIDPassesThroughWhenNothingCollides covers the fast paths:
// fewer than two rows, and all-distinct IDs, both return the input untouched.
func TestDedupeRowsByFactIDPassesThroughWhenNothingCollides(t *testing.T) {
	t.Parallel()

	type row struct{ id string }
	key := func(r row) string { return r.id }

	if got := DedupeRowsByFactID([]row{}, key); len(got) != 0 {
		t.Errorf("empty input = %v, want empty", got)
	}
	one := []row{{id: "a"}}
	if got := DedupeRowsByFactID(one, key); len(got) != 1 || got[0].id != "a" {
		t.Errorf("single row = %v, want it unchanged", got)
	}
	three := []row{{id: "a"}, {id: "b"}, {id: "c"}}
	if got := DedupeRowsByFactID(three, key); len(got) != 3 {
		t.Errorf("all-distinct = %v, want all three", got)
	}
}

// TestBatchInsertFactsChunksAtBatchSize pins that a batch larger than BatchSize
// is split, and that every chunk passes exactly one array argument per column.
// The writer binds arrays rather than per-row placeholders, so the invariant a
// column/array mismatch would break is "same argument count on every chunk,
// equal to the column count" — not an args-per-row multiple.
func TestBatchInsertFactsChunksAtBatchSize(t *testing.T) {
	t.Parallel()

	rows := make([]Row, BatchSize+7)
	for i := range rows {
		rows[i].FactID = "fact-" + itoa(i)
	}

	rec := &recordingExecer{}
	if err := BatchInsertFacts(context.Background(), rec, rows); err != nil {
		t.Fatalf("BatchInsertFacts returned %v", err)
	}
	if len(rec.queries) != 2 {
		t.Fatalf("got %d chunks for %d rows at BatchSize %d, want 2",
			len(rec.queries), len(rows), BatchSize)
	}
	placeholders := len(regexp.MustCompile(`\$\d+`).FindAllString(BatchInsertQuery, -1))
	if placeholders == 0 {
		t.Fatal("found no $N placeholders in BatchInsertQuery; the probe cannot discriminate")
	}
	for i, got := range rec.argCounts {
		if got != placeholders {
			t.Errorf("chunk %d bound %d arguments against %d placeholders; a dropped column passes a self-consistency check but not this one",
				i, got, placeholders)
		}
	}
	if got := len(ChunkArgs(rows[:3])); got != rec.argCounts[0] {
		t.Errorf("ChunkArgs binds %d arguments for 3 rows but a full chunk bound %d; the count must not depend on row count",
			got, rec.argCounts[0])
	}
	for i, q := range rec.queries {
		if !strings.Contains(q, "ON CONFLICT") {
			t.Errorf("chunk %d statement lost its ON CONFLICT clause", i)
		}
	}
}

// TestBatchInsertFactsEmptyIsANoOp pins that an empty batch issues no statement.
func TestBatchInsertFactsEmptyIsANoOp(t *testing.T) {
	t.Parallel()

	rec := &recordingExecer{}
	if err := BatchInsertFacts(context.Background(), rec, nil); err != nil {
		t.Fatalf("empty batch returned %v", err)
	}
	if len(rec.queries) != 0 {
		t.Errorf("empty batch issued %d statements, want 0", len(rec.queries))
	}
}

// captureExecer records the exact positional arguments passed to each
// ExecContext call, so a test can decode the column<->bind-index mapping the
// way the Postgres driver would receive it.
type captureExecer struct {
	calls [][]any
}

func (c *captureExecer) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	c.calls = append(c.calls, args)
	return nil, nil
}

// insertColumnNames parses the column list out of an `INSERT INTO
// fact_records (...)` statement, in declaration order. It lets a test assert
// an expected argument order against the SQL Postgres actually receives,
// rather than against a second hand-written list that could silently drift
// from the statement if a column were reordered.
func insertColumnNames(t *testing.T, query string) []string {
	t.Helper()

	re := regexp.MustCompile(`(?s)INSERT INTO fact_records \(\s*(.*?)\s*\)`)
	m := re.FindStringSubmatch(query)
	if m == nil {
		t.Fatalf("no INSERT INTO fact_records column list found in query:\n%s", query)
	}

	var names []string
	for _, part := range strings.Split(m[1], ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// stageColumnNames extracts a comma-separated identifier list matched by
// pattern's single capture group, trimming whitespace and any trailing
// "::type" cast so a projection column such as "payload::jsonb" compares
// equal to the plain "payload" name the INSERT column list and the AS t(...)
// alias list carry. It fails loudly (never returns an empty slice) so a
// pattern that stops matching cannot make the stage-agreement proof below
// silently vacuous.
func stageColumnNames(t *testing.T, label, query, pattern string) []string {
	t.Helper()

	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(query)
	if m == nil {
		t.Fatalf("%s: pattern %s matched nothing in query:\n%s", label, pattern, query)
	}

	identRe := regexp.MustCompile(`^\w+`)
	var names []string
	for _, part := range strings.Split(m[1], ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ident := identRe.FindString(part)
		if ident == "" {
			t.Fatalf("%s: could not extract a leading identifier from column fragment %q", label, part)
		}
		names = append(names, ident)
	}
	if len(names) == 0 {
		t.Fatalf("%s: parsed zero column names out of:\n%s", label, m[1])
	}
	return names
}

// selectProjectionColumnNames parses the SELECT list between "SELECT" and
// "FROM unnest(" -- the middle of the three positional stages a batch insert
// depends on agreeing.
func selectProjectionColumnNames(t *testing.T, query string) []string {
	t.Helper()
	return stageColumnNames(t, "SELECT projection", query, `(?s)SELECT\s*(.*?)\s*FROM unnest\(`)
}

// unnestAliasColumnNames parses the "AS t(...)" alias list. This is what
// actually BINDS each unnest(...) argument to a NAME: the aliases are
// positional against the unnest(...) argument order, and the SELECT
// projection above reads those names back. Reordering this list alone
// silently re-routes bind parameters into different columns without
// touching the INSERT column list or the Go arrays that feed it.
func unnestAliasColumnNames(t *testing.T, query string) []string {
	t.Helper()
	return stageColumnNames(t, "unnest AS t(...) alias list", query, `(?s)\)\s*AS t\(\s*(.*?)\s*\)`)
}

// unnestBindPlaceholders extracts the ordered "$N" placeholders bound inside
// the unnest(...) argument list, in the order they are written.
func unnestBindPlaceholders(t *testing.T, query string) []string {
	t.Helper()

	re := regexp.MustCompile(`(?s)FROM unnest\(\s*(.*?)\s*\)\s*AS t\(`)
	m := re.FindStringSubmatch(query)
	if m == nil {
		t.Fatalf("no FROM unnest(...) AS t(...) call found in query:\n%s", query)
	}

	placeholders := regexp.MustCompile(`\$\d+`).FindAllString(m[1], -1)
	if len(placeholders) == 0 {
		t.Fatalf("found no $N placeholders inside unnest(...) in query:\n%s", query)
	}
	return placeholders
}

// assertStagesAgree proves the three positional statement stages a batch
// insert depends on -- the INSERT column list, the SELECT projection, and the
// unnest(...) AS t(...) alias list -- name the same columns in the same
// order, and that the unnest bind placeholders form a strict, gapless
// $1..$N sequence matching the alias count.
//
// Postgres binds $N to unnest's Nth argument; the AS t(...) alias at that
// position is what names it, and the SELECT projection then reads that name
// back. A reorder confined to any ONE stage -- for example swapping two AS
// t(...) aliases without touching the INSERT column list or the Go arrays --
// silently re-routes bind parameters into the wrong columns while a check
// against the INSERT list alone stays green. Confirmed by mutation: swapping
// source_system and source_fact_key in BatchInsertSource's AS t(...) list
// alone turns this red (codex, P1, PR #6357).
func assertStagesAgree(t *testing.T, label, query string) {
	t.Helper()

	insertCols := insertColumnNames(t, query)
	selectCols := selectProjectionColumnNames(t, query)
	aliasCols := unnestAliasColumnNames(t, query)

	if len(selectCols) != len(insertCols) {
		t.Fatalf("%s: SELECT projection has %d columns, INSERT list has %d: %v vs %v",
			label, len(selectCols), len(insertCols), selectCols, insertCols)
	}
	if len(aliasCols) != len(insertCols) {
		t.Fatalf("%s: AS t(...) alias list has %d columns, INSERT list has %d: %v vs %v",
			label, len(aliasCols), len(insertCols), aliasCols, insertCols)
	}
	for i := range insertCols {
		if selectCols[i] != insertCols[i] {
			t.Errorf("%s: position %d: SELECT projection column %q != INSERT column %q",
				label, i, selectCols[i], insertCols[i])
		}
		if aliasCols[i] != insertCols[i] {
			t.Errorf("%s: position %d: AS t(...) alias %q != INSERT column %q",
				label, i, aliasCols[i], insertCols[i])
		}
	}

	placeholders := unnestBindPlaceholders(t, query)
	if len(placeholders) != len(aliasCols) {
		t.Fatalf("%s: unnest(...) binds %d placeholders but AS t(...) names %d aliases",
			label, len(placeholders), len(aliasCols))
	}
	for i, ph := range placeholders {
		want := "$" + itoa(i+1)
		if ph != want {
			t.Errorf("%s: unnest(...) placeholder %d = %q, want %q (bind order must be strictly ascending $1..$N with no gaps or repeats)",
				label, i, ph, want)
		}
	}
}

// TestChunkArgsColumnOrderMatchesTheStatement pins the unversioned
// column<->bind-index mapping ChunkArgs and BatchInsertSource's unnest both
// depend on. Every Row field carries a distinct sentinel, so a mismatched or
// swapped array lands at the wrong index and fails here even when both sides
// share a type. A mutation proven to leave the whole
// ./internal/reducer/... tree green without this test: swapping
// observedAts<->ingestedAts in ChunkArgs.
func TestChunkArgsColumnOrderMatchesTheStatement(t *testing.T) {
	t.Parallel()

	sourceURI := "sentinel-source-uri"
	sourceRecordID := "sentinel-source-record-id"
	row := Row{
		FactID:           "sentinel-fact-id",
		ScopeID:          "sentinel-scope-id",
		GenerationID:     "sentinel-generation-id",
		FactKind:         "sentinel-fact-kind",
		StableFactKey:    "sentinel-stable-fact-key",
		CollectorKind:    "sentinel-collector-kind",
		SourceConfidence: "sentinel-source-confidence",
		SourceSystem:     "sentinel-source-system",
		SourceFactKey:    "sentinel-source-fact-key",
		SourceURI:        &sourceURI,
		SourceRecordID:   &sourceRecordID,
		ObservedAt:       time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		IngestedAt:       time.Date(2021, 6, 7, 8, 9, 10, 0, time.UTC),
		IsTombstone:      true,
		Payload:          "sentinel-payload",
		FencingToken:     42,
	}

	args := ChunkArgs([]Row{row})

	want := []struct {
		name string
		want any
	}{
		{"fact_id", []string{row.FactID}},
		{"scope_id", []string{row.ScopeID}},
		{"generation_id", []string{row.GenerationID}},
		{"fact_kind", []string{row.FactKind}},
		{"stable_fact_key", []string{row.StableFactKey}},
		{"collector_kind", []string{row.CollectorKind}},
		{"source_confidence", []string{row.SourceConfidence}},
		{"source_system", []string{row.SourceSystem}},
		{"source_fact_key", []string{row.SourceFactKey}},
		{"source_uri", []*string{row.SourceURI}},
		{"source_record_id", []*string{row.SourceRecordID}},
		{"observed_at", []time.Time{row.ObservedAt}},
		{"ingested_at", []time.Time{row.IngestedAt}},
		{"is_tombstone", []bool{row.IsTombstone}},
		{"payload", []string{row.Payload}},
		{"fencing_token", []int64{row.FencingToken}},
	}
	if len(args) != len(want) {
		t.Fatalf("ChunkArgs returned %d arrays, want %d", len(args), len(want))
	}
	for i, w := range want {
		if !reflect.DeepEqual(args[i], w.want) {
			t.Errorf("arg %d (%s) = %#v, want %#v", i, w.name, args[i], w.want)
		}
	}

	// Cross-check want's order against the shipped statement so a reordered
	// INSERT column list (without a matching ChunkArgs reorder) fails here,
	// rather than binding the wrong array to the wrong column silently.
	sqlColumns := insertColumnNames(t, BatchInsertQuery)
	if len(sqlColumns) != len(want) {
		t.Fatalf("BatchInsertQuery declares %d columns, want list has %d", len(sqlColumns), len(want))
	}
	for i, w := range want {
		if sqlColumns[i] != w.name {
			t.Errorf("BatchInsertQuery column %d = %q, want %q (want list order has drifted from the statement)", i, sqlColumns[i], w.name)
		}
	}

	// The checks above only pin the INSERT column list against the Go bind
	// order; they say nothing about the SELECT projection or the AS t(...)
	// alias list that actually routes each $N bind parameter to a column.
	// assertStagesAgree closes that hole for all three positional stages.
	assertStagesAgree(t, "BatchInsertQuery", BatchInsertQuery)
}

// TestBatchInsertVersionedFactsColumnOrderMatchesTheStatement pins the
// versioned column<->bind-index mapping execVersionedChunk and
// BatchInsertVersionedQuery's unnest both depend on. Every VersionedRow field
// carries a distinct sentinel, so a mismatched or swapped array lands at the
// wrong index and fails here even when both sides share a type. A
// mutation proven to leave the whole ./internal/reducer/... tree green
// without this test: swapping sourceSystems<->sourceFactKeys in
// execVersionedChunk.
func TestBatchInsertVersionedFactsColumnOrderMatchesTheStatement(t *testing.T) {
	t.Parallel()

	sourceURI := "sentinel-source-uri"
	sourceRecordID := "sentinel-source-record-id"
	row := VersionedRow{
		FactID:           "sentinel-fact-id",
		ScopeID:          "sentinel-scope-id",
		GenerationID:     "sentinel-generation-id",
		FactKind:         "sentinel-fact-kind",
		StableFactKey:    "sentinel-stable-fact-key",
		SchemaVersion:    "sentinel-schema-version",
		CollectorKind:    "sentinel-collector-kind",
		SourceConfidence: "sentinel-source-confidence",
		SourceSystem:     "sentinel-source-system",
		SourceFactKey:    "sentinel-source-fact-key",
		SourceURI:        &sourceURI,
		SourceRecordID:   &sourceRecordID,
		ObservedAt:       time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		IngestedAt:       time.Date(2021, 6, 7, 8, 9, 10, 0, time.UTC),
		IsTombstone:      true,
		Payload:          "sentinel-payload",
		FencingToken:     42,
	}

	rec := &captureExecer{}
	if err := BatchInsertVersionedFacts(context.Background(), rec, []VersionedRow{row}); err != nil {
		t.Fatalf("BatchInsertVersionedFacts returned %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("got %d ExecContext calls, want 1", len(rec.calls))
	}
	args := rec.calls[0]

	want := []struct {
		name string
		want any
	}{
		{"fact_id", []string{row.FactID}},
		{"scope_id", []string{row.ScopeID}},
		{"generation_id", []string{row.GenerationID}},
		{"fact_kind", []string{row.FactKind}},
		{"stable_fact_key", []string{row.StableFactKey}},
		{"schema_version", []string{row.SchemaVersion}},
		{"collector_kind", []string{row.CollectorKind}},
		{"source_confidence", []string{row.SourceConfidence}},
		{"source_system", []string{row.SourceSystem}},
		{"source_fact_key", []string{row.SourceFactKey}},
		{"source_uri", []*string{row.SourceURI}},
		{"source_record_id", []*string{row.SourceRecordID}},
		{"observed_at", []time.Time{row.ObservedAt}},
		{"ingested_at", []time.Time{row.IngestedAt}},
		{"is_tombstone", []bool{row.IsTombstone}},
		{"payload", []string{row.Payload}},
		{"fencing_token", []int64{row.FencingToken}},
	}
	if len(args) != len(want) {
		t.Fatalf("versioned exec bound %d arguments, want %d", len(args), len(want))
	}
	for i, w := range want {
		if !reflect.DeepEqual(args[i], w.want) {
			t.Errorf("arg %d (%s) = %#v, want %#v", i, w.name, args[i], w.want)
		}
	}

	// Cross-check want's order against the shipped statement so a reordered
	// INSERT column list (without a matching execVersionedChunk reorder) fails
	// here, rather than binding the wrong array to the wrong column silently.
	sqlColumns := insertColumnNames(t, BatchInsertVersionedQuery)
	if len(sqlColumns) != len(want) {
		t.Fatalf("BatchInsertVersionedQuery declares %d columns, want list has %d", len(sqlColumns), len(want))
	}
	for i, w := range want {
		if sqlColumns[i] != w.name {
			t.Errorf("BatchInsertVersionedQuery column %d = %q, want %q (want list order has drifted from the statement)", i, sqlColumns[i], w.name)
		}
	}

	// The checks above only pin the INSERT column list against the Go bind
	// order; they say nothing about the SELECT projection or the AS t(...)
	// alias list that actually routes each $N bind parameter to a column.
	// assertStagesAgree closes that hole for all three positional stages.
	assertStagesAgree(t, "BatchInsertVersionedQuery", BatchInsertVersionedQuery)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
