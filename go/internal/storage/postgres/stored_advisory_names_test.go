// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// storedNamesFakeDB records each statement's id bind and answers with rows when
// set, otherwise with one (advisory_id, ecosystem, package_name, package_id)
// row per id it was asked about.
type storedNamesFakeDB struct {
	binds [][]string
	rows  [][]string
	err   error
}

func (f *storedNamesFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("storedNamesFakeDB: unexpected exec")
}

func (f *storedNamesFakeDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if f.err != nil {
		return nil, f.err
	}
	if query != storedAdvisoryNamesQuery {
		return nil, fmt.Errorf("storedNamesFakeDB: unexpected statement %q", query)
	}
	ids, _ := args[0].([]string)
	f.binds = append(f.binds, slices.Clone(ids))
	if f.rows != nil {
		return &storedNamesFakeRows{rows: f.rows}, nil
	}
	rows := make([][]string, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, []string{id, "npm", "name-of-" + id, "npm://registry.npmjs.org/name-of-" + id})
	}
	return &storedNamesFakeRows{rows: rows}, nil
}

type storedNamesFakeRows struct {
	rows [][]string
	at   int
}

func (r *storedNamesFakeRows) Next() bool { r.at++; return r.at <= len(r.rows) }
func (r *storedNamesFakeRows) Err() error { return nil }
func (r *storedNamesFakeRows) Close() error {
	return nil
}

func (r *storedNamesFakeRows) Scan(dest ...any) error {
	for i, value := range r.rows[r.at-1] {
		*dest[i].(*string) = value
	}
	return nil
}

func TestStoredAdvisoryNamesQueryIsActiveAndIndexShaped(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		// The anchor is a fence the planner cannot fold into the scope join.
		"WITH candidates AS MATERIALIZED (",
		// The index key expression.
		"fact.payload->>'advisory_id' = ANY($1::text[])",
		"AND fact.is_tombstone = FALSE",
		// The active-generation joins every active reader uses, on candidates.
		"scope.active_generation_id = candidate.generation_id",
		"generation.status = 'active'",
		// The narrowing happens after the index read.
		"candidate.fact_kind = 'vulnerability.affected_package'",
	} {
		if !strings.Contains(storedAdvisoryNamesQuery, want) {
			t.Errorf("storedAdvisoryNamesQuery missing %q:\n%s", want, storedAdvisoryNamesQuery)
		}
	}
}

// TestStoredAdvisoryNamesQueryKindListMatchesTheIndexPredicate derives the
// advisory lookup index's fact_kind list from the shipped DDL and requires the
// candidates anchor to carry the same list. A list that drifts from the index
// predicate stops the planner proving the partial predicate and falls back to
// the purl index or the scope-led plan measured in the #9777 evidence note.
func TestStoredAdvisoryNamesQueryKindListMatchesTheIndexPredicate(t *testing.T) {
	t.Parallel()

	const indexName = "fact_records_vulnerability_active_advisory_lookup_v2_idx"
	ddl := vulnerabilityFactRecordReadIndexesSQL
	start := strings.Index(ddl, indexName)
	if start < 0 {
		t.Fatalf("index %s is not in vulnerabilityFactRecordReadIndexesSQL", indexName)
	}
	statement, _, _ := strings.Cut(ddl[start:], ";")
	_, predicate, ok := strings.Cut(statement, "WHERE ")
	if !ok {
		t.Fatalf("index %s has no partial predicate: %q", indexName, statement)
	}
	kindList, _, ok := strings.Cut(predicate, " AND is_tombstone = FALSE")
	if !ok || !strings.HasPrefix(kindList, "fact_kind IN (") {
		t.Fatalf("index %s predicate is not a fact_kind list: %q", indexName, predicate)
	}
	if want := "fact." + kindList; !strings.Contains(storedAdvisoryNamesQuery, want) {
		t.Fatalf("storedAdvisoryNamesQuery anchor does not carry the index predicate %q:\n%s", want, storedAdvisoryNamesQuery)
	}
}

func TestListStoredAdvisoryPackageNamesChunksAndDedupes(t *testing.T) {
	t.Parallel()

	fake := &storedNamesFakeDB{}
	store := FactStore{database: fake}
	ids := []string{" ", "", "ADV-0"}
	for n := range 1003 {
		ids = append(ids, fmt.Sprintf("ADV-%04d", n))
	}
	ids = append(ids, "ADV-0000") // repeated, and "ADV-0" is a distinct id

	got, err := store.ListStoredAdvisoryPackageNames(context.Background(), ids)
	if err != nil {
		t.Fatalf("ListStoredAdvisoryPackageNames() error = %v", err)
	}
	if want := 1004; len(got) != want {
		t.Fatalf("ids resolved = %d, want %d", len(got), want)
	}
	// Literal sizes: 500 ids per statement is the bound the QA plans measured.
	sizes := make([]int, 0, len(fake.binds))
	for _, bind := range fake.binds {
		sizes = append(sizes, len(bind))
	}
	if want := []int{500, 500, 4}; !slices.Equal(sizes, want) {
		t.Fatalf("statement sizes = %v, want %v", sizes, want)
	}
	seen := make(map[string]int)
	for _, bind := range fake.binds {
		for _, id := range bind {
			seen[id]++
		}
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("id %q sent %d times, want once", id, count)
		}
	}
	if _, blank := seen[""]; blank {
		t.Error("blank id reached the database")
	}
}

// TestListStoredAdvisoryPackageNamesSortsTiesByPackageID pins the sort key that
// keeps a repeated result stable: two Maven packages can share an ecosystem and
// artifact name and differ only in the group inside PackageID.
func TestListStoredAdvisoryPackageNamesSortsTiesByPackageID(t *testing.T) {
	t.Parallel()

	fake := &storedNamesFakeDB{rows: [][]string{
		{"ADV-1", "pypi", "a-name", "pypi://pypi.org/simple/a-name"},
		{"ADV-1", "maven", "lib", "maven://repo.maven.apache.org/maven2/org.example:lib"},
		{"ADV-1", "maven", "lib", "maven://repo.maven.apache.org/maven2/com.other:lib"},
	}}
	got, err := FactStore{database: fake}.ListStoredAdvisoryPackageNames(context.Background(), []string{"ADV-1"})
	if err != nil {
		t.Fatalf("ListStoredAdvisoryPackageNames() error = %v", err)
	}
	want := []StoredAdvisoryPackageName{
		{Ecosystem: "maven", PackageName: "lib", PackageID: "maven://repo.maven.apache.org/maven2/com.other:lib"},
		{Ecosystem: "maven", PackageName: "lib", PackageID: "maven://repo.maven.apache.org/maven2/org.example:lib"},
		{Ecosystem: "pypi", PackageName: "a-name", PackageID: "pypi://pypi.org/simple/a-name"},
	}
	if !slices.Equal(got["ADV-1"], want) {
		t.Fatalf("names = %v, want %v", got["ADV-1"], want)
	}
}

func TestListStoredAdvisoryPackageNamesEmptyInputSkipsDatabase(t *testing.T) {
	t.Parallel()

	fake := &storedNamesFakeDB{}
	got, err := FactStore{database: fake}.ListStoredAdvisoryPackageNames(context.Background(), []string{"", "  "})
	if err != nil || len(got) != 0 || len(fake.binds) != 0 {
		t.Fatalf("got = %v, err = %v, statements = %d; want empty result, nil error, no statement", got, err, len(fake.binds))
	}
	// An empty request needs no database, so a store without one still answers.
	if got, err := (FactStore{}).ListStoredAdvisoryPackageNames(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("nil-database empty request = %v, %v; want empty, nil", got, err)
	}
	if _, err := (FactStore{}).ListStoredAdvisoryPackageNames(context.Background(), []string{"ADV-1"}); err == nil {
		t.Fatal("nil database with ids: want an error, got nil")
	}
}

func TestListStoredAdvisoryPackageNamesFailsClosedOnQueryError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	got, err := FactStore{database: &storedNamesFakeDB{err: boom}}.ListStoredAdvisoryPackageNames(
		context.Background(), []string{"ADV-1"})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
	if got != nil {
		t.Fatalf("result on error = %v, want nil: a partial name set would hide a dropped name", got)
	}
}

func TestListStoredAdvisoryPackageNamesRecordsCountsOnSpan(t *testing.T) {
	// Not parallel: it swaps the global tracer provider.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	_, err := FactStore{database: &storedNamesFakeDB{}}.ListStoredAdvisoryPackageNames(
		context.Background(), []string{"ADV-1", "ADV-2"})
	if err != nil {
		t.Fatalf("ListStoredAdvisoryPackageNames() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "stored_advisory_names.read" {
		t.Fatalf("spans = %v, want one stored_advisory_names.read", spans)
	}
	got := make(map[string]int64)
	for _, attr := range spans[0].Attributes() {
		got[string(attr.Key)] = attr.Value.AsInt64()
	}
	for key, want := range map[string]int64{
		"stored_advisory_names.ids_requested": 2,
		"stored_advisory_names.chunks":        1,
		"stored_advisory_names.ids_resolved":  2,
		"stored_advisory_names.packages":      2,
	} {
		if got[key] != want {
			t.Errorf("span attribute %s = %d, want %d", key, got[key], want)
		}
	}
}
