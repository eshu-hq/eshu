// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The writer upserts supports from the rows it holds in memory (#10880 PR-C2 h).
// These tests pin that the in-memory statement and the fact-derived one (the
// backfill's) cannot drift: one projection, one upsert wrapper, differing only in
// the relation the rows come from.

// TestSupportRowsUpsertDiffersFromFactDerivedOnlyInTheSource is the hermetic half
// of the differential that lets the supports contract stay at version 1: the two
// statements carry byte-identical column expressions, SET list and write-on-change
// guard, and the rows one derives from fact_records the other derives from an
// unnest of the same six fact columns. The live differential (owed to the lab)
// proves the stored rows are byte-identical.
func TestSupportRowsUpsertDiffersFromFactDerivedOnlyInTheSource(t *testing.T) {
	t.Parallel()

	fromFacts := upsertSupplyChainImpactSupportsSQL
	fromRows := upsertSupplyChainImpactSupportRowsSQL

	splitSource := func(statement string) (head, source, tail string) {
		head, rest, ok := strings.Cut(statement, "\nFROM ")
		if !ok {
			t.Fatalf("statement has no FROM clause:\n%s", statement)
		}
		source, tail, ok = strings.Cut(rest, "\nON CONFLICT")
		if !ok {
			t.Fatalf("statement has no ON CONFLICT clause:\n%s", statement)
		}
		return head, source, tail
	}
	factHead, factSource, factTail := splitSource(fromFacts)
	rowHead, rowSource, rowTail := splitSource(fromRows)

	if factHead != rowHead {
		t.Errorf("the SELECT lists differ:\nfact-derived:\n%s\nin-memory:\n%s", factHead, rowHead)
	}
	if factTail != rowTail {
		t.Errorf("the conflict clauses differ:\nfact-derived:\n%s\nin-memory:\n%s", factTail, rowTail)
	}
	if !strings.Contains(factSource, "fact_records AS fact") {
		t.Errorf("the fact-derived source no longer reads fact_records: %s", factSource)
	}
	if strings.Contains(rowSource, "fact_records") {
		t.Errorf("the in-memory source still names fact_records: %s", rowSource)
	}
	for _, want := range []string{
		"unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])",
		"AS t(fact_id, scope_id, generation_id, source_confidence, stable_fact_key, payload)",
		"t.payload::jsonb AS payload",
		") AS fact",
	} {
		if !strings.Contains(rowSource, want) {
			t.Errorf("the in-memory source is missing %q:\n%s", want, rowSource)
		}
	}
	// Non-vacuity: the projection still derives every column from the shared list.
	for _, column := range supplyChainImpactSupportColumns {
		if !strings.Contains(rowHead, column.expr+" AS "+column.name) {
			t.Errorf("the in-memory statement lost the %s expression", column.name)
		}
	}
	if !strings.Contains(rowHead, "fact.stable_fact_key AS stable_fact_key") {
		t.Error("the in-memory statement does not fill stable_fact_key")
	}
}

// upsertRecordingTx records every statement and its arguments and answers each
// upsert with one inserted-or-updated flag per row the arrays carry.
type upsertRecordingTx struct {
	queries []string
	args    [][]any
	// updatedEvery marks every Nth returned row as an update (0 = all inserted).
	updatedEvery int
}

func (*upsertRecordingTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("upsertRecordingTx runs no exec")
}

func (t *upsertRecordingTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	t.queries = append(t.queries, query)
	t.args = append(t.args, args)
	n := len(upsertArgStrings(nil, args[0]))
	rows := make([]bool, n)
	for i := range rows {
		rows[i] = t.updatedEvery == 0 || (i+1)%t.updatedEvery != 0
	}
	return &flagRows{flags: rows}, nil
}

func (*upsertRecordingTx) Commit() error   { return nil }
func (*upsertRecordingTx) Rollback() error { return nil }

type flagRows struct {
	flags []bool
	at    int
}

func (r *flagRows) Next() bool { r.at++; return r.at <= len(r.flags) }
func (r *flagRows) Scan(dest ...any) error {
	*(dest[0].(*bool)) = r.flags[r.at-1]
	return nil
}
func (*flagRows) Err() error   { return nil }
func (*flagRows) Close() error { return nil }

func supportRow(factID, payload string) factwrite.VersionedRow {
	return factwrite.VersionedRow{
		FactID: factID, ScopeID: "scope-" + factID, GenerationID: "gen-" + factID,
		FactKind:         facts.ReducerSupplyChainImpactFindingFactKind,
		SourceConfidence: "inferred", StableFactKey: "key-" + factID, Payload: payload,
	}
}

// TestUpsertImpactSupportRowsBindsTheWritersRowsInFactIDOrder pins the binding:
// six parallel arrays, one element per distinct fact id, in fact id order, the last
// row of a duplicated id winning, and rows in chunks of factwrite.BatchSize so one
// statement stays bounded.
func TestUpsertImpactSupportRowsBindsTheWritersRowsInFactIDOrder(t *testing.T) {
	t.Parallel()

	rec := &upsertRecordingTx{}
	tx := &supplyChainImpactTx{tx: rec}
	rows := []factwrite.VersionedRow{
		supportRow("fact-c", `{"cve_id":"CVE-3"}`),
		supportRow("fact-a", `{"cve_id":"CVE-1"}`),
		supportRow("fact-b", `{"cve_id":"CVE-2"}`),
		supportRow("fact-a", `{"cve_id":"CVE-1b"}`),
	}
	result, err := tx.UpsertImpactSupportRows(context.Background(), rows)
	if err != nil {
		t.Fatalf("UpsertImpactSupportRows() error = %v", err)
	}
	if len(rec.queries) != 1 || rec.queries[0] != upsertSupplyChainImpactSupportRowsSQL {
		t.Fatalf("statements = %d, want exactly the in-memory upsert", len(rec.queries))
	}
	args := rec.args[0]
	if len(args) != 6 {
		t.Fatalf("bound %d arguments, want 6 parallel arrays", len(args))
	}
	want := map[int][]string{
		0: {"fact-a", "fact-b", "fact-c"},
		1: {"scope-fact-a", "scope-fact-b", "scope-fact-c"},
		2: {"gen-fact-a", "gen-fact-b", "gen-fact-c"},
		3: {"inferred", "inferred", "inferred"},
		4: {"key-fact-a", "key-fact-b", "key-fact-c"},
		5: {`{"cve_id":"CVE-1b"}`, `{"cve_id":"CVE-2"}`, `{"cve_id":"CVE-3"}`},
	}
	for index, values := range want {
		if got := upsertArgStrings(t, args[index]); !reflect.DeepEqual(got, values) {
			t.Errorf("argument $%d = %v, want %v", index+1, got, values)
		}
	}
	if result.Inserted != 3 || result.Updated != 0 || result.Unchanged != 0 {
		t.Errorf("write = %+v, want 3 inserted", result)
	}
}

// TestUpsertImpactSupportRowsCountsUnchangedAndChunks pins the counts and the
// chunking: rows the write-on-change guard skipped return no flag and count as
// unchanged, and more rows than one batch go in more than one statement.
func TestUpsertImpactSupportRowsCountsUnchangedAndChunks(t *testing.T) {
	t.Parallel()

	rec := &upsertRecordingTx{updatedEvery: 2}
	tx := &supplyChainImpactTx{tx: rec}
	total := factwrite.BatchSize + 5
	rows := make([]factwrite.VersionedRow, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, supportRow(fmt.Sprintf("fact-%05d", i), `{}`))
	}
	if _, err := tx.UpsertImpactSupportRows(context.Background(), rows); err != nil {
		t.Fatalf("UpsertImpactSupportRows() error = %v", err)
	}
	if len(rec.queries) != 2 {
		t.Fatalf("statements = %d, want 2 for %d rows at batch size %d", len(rec.queries), total, factwrite.BatchSize)
	}
	if first, second := len(upsertArgStrings(t, rec.args[0][0])), len(upsertArgStrings(t, rec.args[1][0])); first != factwrite.BatchSize || second != 5 {
		t.Fatalf("chunk sizes = %d and %d, want %d and 5", first, second, factwrite.BatchSize)
	}

	empty := &upsertRecordingTx{}
	if result, err := (&supplyChainImpactTx{tx: empty}).UpsertImpactSupportRows(context.Background(), nil); err != nil || len(empty.queries) != 0 || result != (supplychaincore.SupplyChainImpactSupportsWrite{}) {
		t.Fatalf("no rows: result %+v, err %v, statements %d, want none", result, err, len(empty.queries))
	}
}

// upsertArgStrings returns the strings of a bound array argument. A nil t panics
// on a wrong type instead of failing the test, for use inside the fake.
func upsertArgStrings(t *testing.T, arg any) []string {
	switch values := arg.(type) {
	case array.StringArray:
		return []string(values)
	case *array.StringArray:
		return []string(*values)
	}
	if t != nil {
		t.Helper()
		t.Fatalf("argument %#v is not an array.StringArray", arg)
	}
	panic(fmt.Sprintf("argument %#v is not an array.StringArray", arg))
}

// TestUpsertImpactSupportRowsSkipsTombstonedAndForeignKindRows pins the parity the
// fact-derived statement has by its WHERE clause: it reads only live rows of the
// finding kind, so the in-memory path drops a tombstoned row and a row of another
// kind before binding (the advisory-group cell admits cassette rows that can carry
// a tombstone).
func TestUpsertImpactSupportRowsSkipsTombstonedAndForeignKindRows(t *testing.T) {
	t.Parallel()

	rec := &upsertRecordingTx{}
	tx := &supplyChainImpactTx{tx: rec}
	tombstone := supportRow("fact-b", `{}`)
	tombstone.IsTombstone = true
	foreign := supportRow("fact-c", `{}`)
	foreign.FactKind = "some_other_kind"
	rows := []factwrite.VersionedRow{supportRow("fact-a", `{}`), tombstone, foreign}
	result, err := tx.UpsertImpactSupportRows(context.Background(), rows)
	if err != nil {
		t.Fatalf("UpsertImpactSupportRows() error = %v", err)
	}
	if len(rec.queries) != 1 {
		t.Fatalf("statements = %d, want 1", len(rec.queries))
	}
	if got := upsertArgStrings(t, rec.args[0][0]); !reflect.DeepEqual(got, []string{"fact-a"}) {
		t.Fatalf("bound fact ids = %v, want only fact-a", got)
	}
	if result.Inserted != 1 {
		t.Fatalf("write = %+v, want 1 inserted", result)
	}

	onlyDropped := &upsertRecordingTx{}
	if _, err := (&supplyChainImpactTx{tx: onlyDropped}).UpsertImpactSupportRows(context.Background(), []factwrite.VersionedRow{tombstone, foreign}); err != nil || len(onlyDropped.queries) != 0 {
		t.Fatalf("only dropped rows: err %v, statements %d, want no statement", err, len(onlyDropped.queries))
	}
}
