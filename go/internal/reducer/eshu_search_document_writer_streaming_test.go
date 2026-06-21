package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// TestStreamingSearchDocumentWriteRetiresOnceWithUnionKeepSet proves the #3440
// streaming write contract: inserting documents across multiple pages issues
// insert-only statements per page (NO retire), and the authoritative retire
// runs exactly once at Finalize with the union of every page's written fact
// ids. A naive per-page retire would delete earlier pages' rows; this test
// fails if any retire (fact_records / index docs / index terms) runs before
// Finalize.
func TestStreamingSearchDocumentWriteRetiresOnceWithUnionKeepSet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeSearchDocExecer{retireAffected: 3}
	writer := PostgresEshuSearchDocumentWriter{DB: db, Now: func() time.Time { return now }}

	session, err := writer.BeginEshuSearchDocumentWrite(context.Background(), EshuSearchDocumentWriteBegin{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
	})
	if err != nil {
		t.Fatalf("BeginEshuSearchDocumentWrite error = %v", err)
	}

	page1 := []searchdocs.Document{sampleSearchDoc("searchdoc:content_entity:e-1")}
	page2 := []searchdocs.Document{
		sampleSearchDoc("searchdoc:content_entity:e-2"),
		sampleSearchDoc("searchdoc:content_entity:e-3"),
	}
	if err := session.InsertPage(context.Background(), page1); err != nil {
		t.Fatalf("InsertPage(page1) error = %v", err)
	}
	if err := session.InsertPage(context.Background(), page2); err != nil {
		t.Fatalf("InsertPage(page2) error = %v", err)
	}

	// Before Finalize, NO retire/delete-by-absence statement may have run.
	for _, exec := range db.execs {
		if isRetireByAbsence(exec.query) {
			t.Fatalf("retire ran during InsertPage (page retire would delete prior pages): %q", exec.query)
		}
	}

	result, err := session.Finalize(context.Background())
	if err != nil {
		t.Fatalf("Finalize error = %v", err)
	}
	if result.CanonicalWrites != 3 {
		t.Errorf("canonical writes = %d, want 3 (union of both pages)", result.CanonicalWrites)
	}
	if result.Retired != 3 {
		t.Errorf("retired = %d, want 3 (single finalize retire)", result.Retired)
	}

	// Exactly one fact-record retire ran, and its keep-set is the union of both
	// pages (3 fact ids).
	var factRetires []fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "DELETE FROM fact_records") {
			factRetires = append(factRetires, exec)
		}
	}
	if len(factRetires) != 1 {
		t.Fatalf("fact retire count = %d, want exactly 1", len(factRetires))
	}
	keepIDs, ok := factRetires[0].args[3].([]string)
	if !ok || len(keepIDs) != 3 {
		t.Fatalf("finalize keep-set = %v, want 3 union fact ids", factRetires[0].args[3])
	}
}

// TestStreamingSearchDocumentWriteEmptyFinalizeRetiresAll proves the
// empty-document edge: a session that inserts no page still clears stale
// documents at Finalize (retire-by-absence with an empty keep-set).
func TestStreamingSearchDocumentWriteEmptyFinalizeRetiresAll(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{retireAffected: 5}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	session, err := writer.BeginEshuSearchDocumentWrite(context.Background(), EshuSearchDocumentWriteBegin{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
	})
	if err != nil {
		t.Fatalf("BeginEshuSearchDocumentWrite error = %v", err)
	}
	result, err := session.Finalize(context.Background())
	if err != nil {
		t.Fatalf("Finalize error = %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Errorf("canonical writes = %d, want 0", result.CanonicalWrites)
	}
	if result.Retired != 5 {
		t.Errorf("retired = %d, want 5", result.Retired)
	}
	var retire fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "DELETE FROM fact_records") {
			retire = exec
			break
		}
	}
	if retire.query == "" {
		t.Fatalf("missing fact retirement delete: %#v", db.execs)
	}
	ids, ok := retire.args[3].([]string)
	if !ok || len(ids) != 0 {
		t.Fatalf("empty finalize keep-set = %v, want empty slice", retire.args[3])
	}
}

// TestWriteEshuSearchDocumentsEqualsStreamingOnePage proves the back-compat
// single-shot WriteEshuSearchDocuments is equivalent to Begin+InsertPage+Finalize
// for the same documents (same end-state queries issued).
func TestWriteEshuSearchDocumentsEqualsStreamingOnePage(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		sampleSearchDoc("searchdoc:content_entity:e-1"),
		sampleSearchDoc("searchdoc:content_entity:e-2"),
	}

	singleShotDB := &fakeSearchDocExecer{retireAffected: 1}
	writer := PostgresEshuSearchDocumentWriter{DB: singleShotDB}
	singleResult, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    docs,
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	streamDB := &fakeSearchDocExecer{retireAffected: 1}
	streamWriter := PostgresEshuSearchDocumentWriter{DB: streamDB}
	session, err := streamWriter.BeginEshuSearchDocumentWrite(context.Background(), EshuSearchDocumentWriteBegin{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
	})
	if err != nil {
		t.Fatalf("BeginEshuSearchDocumentWrite error = %v", err)
	}
	if err := session.InsertPage(context.Background(), docs); err != nil {
		t.Fatalf("InsertPage error = %v", err)
	}
	streamResult, err := session.Finalize(context.Background())
	if err != nil {
		t.Fatalf("Finalize error = %v", err)
	}

	if singleResult != streamResult {
		t.Fatalf("single-shot result %+v != streamed result %+v", singleResult, streamResult)
	}
	// Both paths must issue the same set of retire-by-absence statements once.
	if got := countRetireByAbsence(singleShotDB.execs); got != countRetireByAbsence(streamDB.execs) {
		t.Fatalf("retire statement count differs: single=%d stream=%d", got, countRetireByAbsence(streamDB.execs))
	}
}

func isRetireByAbsence(query string) bool {
	return strings.Contains(query, "DELETE FROM fact_records") ||
		strings.Contains(query, "DELETE FROM eshu_search_index_terms\nWHERE scope_id = $1\n  AND generation_id = $2\n  AND document_id <> ALL") ||
		strings.Contains(query, "DELETE FROM eshu_search_index_documents\nWHERE scope_id = $1")
}

func countRetireByAbsence(execs []fakeSearchDocExecCall) int {
	count := 0
	for _, exec := range execs {
		if isRetireByAbsence(exec.query) {
			count++
		}
	}
	return count
}
