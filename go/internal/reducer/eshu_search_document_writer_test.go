package reducer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

type fakeSearchDocExecCall struct {
	query string
	args  []any
}

type fakeSearchDocResult struct {
	affected int64
}

func (r fakeSearchDocResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeSearchDocResult) RowsAffected() (int64, error) { return r.affected, nil }

type fakeSearchDocExecer struct {
	execs          []fakeSearchDocExecCall
	retireAffected int64
	failOn         string
}

func (f *fakeSearchDocExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, fakeSearchDocExecCall{query: query, args: args})
	if f.failOn != "" && strings.Contains(query, f.failOn) {
		return nil, errors.New("boom")
	}
	if strings.Contains(query, "DELETE FROM fact_records") {
		return fakeSearchDocResult{affected: f.retireAffected}, nil
	}
	return fakeSearchDocResult{affected: 1}, nil
}

func sampleSearchDoc(id string) searchdocs.Document {
	return searchdocs.Document{
		ID:           id,
		RepoID:       "repo-1",
		SourceKind:   searchdocs.SourceKindCodeEntity,
		Title:        "Function Handle",
		GraphHandles: []searchdocs.GraphHandle{{Kind: "content_entity", ID: id}},
		TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
}

func TestWriteEshuSearchDocumentsUpsertsAndRetires(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeSearchDocExecer{retireAffected: 2}
	writer := PostgresEshuSearchDocumentWriter{DB: db, Now: func() time.Time { return now }}

	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    []searchdocs.Document{sampleSearchDoc("searchdoc:content_entity:e-1"), sampleSearchDoc("searchdoc:content_entity:e-2")},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if result.CanonicalWrites != 2 {
		t.Errorf("canonical writes = %d, want 2", result.CanonicalWrites)
	}
	if result.Retired != 2 {
		t.Errorf("retired = %d, want 2", result.Retired)
	}
	// Two fact upserts plus fact retirement and persisted-index maintenance.
	if got := len(db.execs); got < 6 {
		t.Fatalf("exec calls = %d, want fact writes plus search-index maintenance", got)
	}
	insert := db.execs[0]
	if !strings.Contains(insert.query, "INSERT INTO fact_records") {
		t.Fatalf("first exec is not an insert: %q", insert.query)
	}
	if got, want := len(insert.args), 15; got != want {
		t.Fatalf("insert arg count = %d, want %d", got, want)
	}
	if got, want := insert.args[3], EshuSearchDocumentFactKind; got != want {
		t.Errorf("fact_kind = %v, want %v", got, want)
	}
	if got, want := insert.args[6], facts.SourceConfidenceInferred; got != want {
		t.Errorf("source_confidence = %v, want %v", got, want)
	}
	if got, want := insert.args[1], "scope-1"; got != want {
		t.Errorf("scope_id = %v, want %v", got, want)
	}
	if got, want := insert.args[13], false; got != want {
		t.Errorf("is_tombstone = %v, want false", got)
	}
	var retire fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "DELETE FROM fact_records") {
			retire = exec
			break
		}
	}
	if !strings.Contains(retire.query, "DELETE FROM fact_records") {
		t.Fatalf("missing fact retirement delete: %#v", db.execs)
	}
	ids, ok := retire.args[3].([]string)
	if !ok || len(ids) != 2 {
		t.Fatalf("retire written-id arg = %v, want 2 ids", retire.args[3])
	}
}

func TestWriteEshuSearchDocumentsMaintainsPersistedSearchIndex(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			sampleSearchDoc("searchdoc:content_entity:e-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	var sawDocumentUpsert, sawTermRefresh, sawStatsUpsert bool
	for _, exec := range db.execs {
		sawDocumentUpsert = sawDocumentUpsert || strings.Contains(exec.query, "INSERT INTO eshu_search_index_documents")
		sawTermRefresh = sawTermRefresh || strings.Contains(exec.query, "INSERT INTO eshu_search_index_terms")
		sawStatsUpsert = sawStatsUpsert || strings.Contains(exec.query, "INSERT INTO eshu_search_index_stats")
	}
	if !sawDocumentUpsert {
		t.Fatal("missing persisted search-index document upsert")
	}
	if !sawTermRefresh {
		t.Fatal("missing persisted search-index term refresh")
	}
	if !sawStatsUpsert {
		t.Fatal("missing persisted search-index stats upsert")
	}
}

func TestWriteEshuSearchDocumentsEmptySetRetiresAll(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{retireAffected: 5}
	writer := PostgresEshuSearchDocumentWriter{DB: db}
	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Errorf("canonical writes = %d, want 0", result.CanonicalWrites)
	}
	if result.Retired != 5 {
		t.Errorf("retired = %d, want 5", result.Retired)
	}
	if got := len(db.execs); got < 4 {
		t.Fatalf("exec calls = %d, want fact retirement plus empty-index maintenance", got)
	}
	var retire fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "DELETE FROM fact_records") {
			retire = exec
			break
		}
	}
	if !strings.Contains(retire.query, "DELETE FROM fact_records") {
		t.Fatalf("missing fact retirement delete: %#v", db.execs)
	}
	ids, ok := retire.args[3].([]string)
	if !ok || len(ids) != 0 {
		t.Fatalf("retire id arg = %v, want empty slice", retire.args[3])
	}
}

func TestWriteEshuSearchDocumentsDeterministicFactID(t *testing.T) {
	t.Parallel()

	first := eshuSearchDocumentFactID("scope-1", "gen-1", "searchdoc:content_entity:e-1")
	second := eshuSearchDocumentFactID("scope-1", "gen-1", "searchdoc:content_entity:e-1")
	if first != second {
		t.Fatalf("fact id not deterministic: %q vs %q", first, second)
	}
	if other := eshuSearchDocumentFactID("scope-1", "gen-2", "searchdoc:content_entity:e-1"); other == first {
		t.Fatal("fact id must differ across generations")
	}
}

func TestWriteEshuSearchDocumentsRequiresDatabaseAndScope(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresEshuSearchDocumentWriter{}).WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{ScopeID: "s", GenerationID: "g"}); err == nil {
		t.Fatal("expected error for nil database")
	}
	db := &fakeSearchDocExecer{}
	if _, err := (PostgresEshuSearchDocumentWriter{DB: db}).WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{GenerationID: "g"}); err == nil {
		t.Fatal("expected error for missing scope")
	}
}
