// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestStreamingSearchDocumentWriteCancelCleansUpPartialPages proves that
// Cancel after one or more InsertPage calls removes every fact and index row
// written for the generation so a mid-stream error does not leave partial
// search documents queryable. This is the #3450 review regression: Handle
// previously returned a stream error without cleaning up already-inserted pages.
func TestStreamingSearchDocumentWriteCancelCleansUpPartialPages(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{retireAffected: 2}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	session, err := writer.BeginEshuSearchDocumentWrite(context.Background(), EshuSearchDocumentWriteBegin{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
	})
	if err != nil {
		t.Fatalf("BeginEshuSearchDocumentWrite error = %v", err)
	}

	// Insert one page so partial state exists in the DB.
	if err := session.InsertPage(context.Background(), []searchdocs.Document{
		sampleSearchDoc("searchdoc:content_entity:e-1"),
	}); err != nil {
		t.Fatalf("InsertPage error = %v", err)
	}

	// Stream error mid-way: caller must Cancel (not Finalize).
	if err := session.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel error = %v", err)
	}

	// Cancel must issue the retire-by-absence statements with an empty keep-set,
	// deleting every row written for this generation.
	var factDeletes, indexDocDeletes, indexTermDeletes int
	for _, exec := range db.execs {
		switch {
		case strings.Contains(exec.query, "DELETE FROM fact_records"):
			factDeletes++
			ids, ok := exec.args[3].([]string)
			if !ok || len(ids) != 0 {
				t.Fatalf("Cancel fact delete keep-set = %v, want empty []string", exec.args[3])
			}
		case strings.Contains(exec.query, "DELETE FROM eshu_search_index_documents"):
			indexDocDeletes++
		case strings.Contains(exec.query, "DELETE FROM eshu_search_index_terms") &&
			strings.Contains(exec.query, "<> ALL"):
			indexTermDeletes++
		}
	}
	if factDeletes != 1 {
		t.Fatalf("fact deletes after Cancel = %d, want 1", factDeletes)
	}
	if indexDocDeletes != 1 {
		t.Fatalf("index-doc deletes after Cancel = %d, want 1", indexDocDeletes)
	}
	if indexTermDeletes != 1 {
		t.Fatalf("index-term deletes after Cancel = %d, want 1", indexTermDeletes)
	}
}

// TestHandlerCancelsSessionOnStreamError proves the handler calls Cancel on the
// write session when StreamSearchDocumentSources returns an error mid-stream
// (after at least one InsertPage has been issued), so no partial documents
// remain queryable for the scope.
func TestHandlerCancelsSessionOnStreamError(t *testing.T) {
	t.Parallel()

	// The loader delivers one page successfully then returns an error.
	loader := &fakePagedSearchDocLoader{
		pages: []SearchDocumentProjectionInput{{
			ContentEntities: []searchdocs.ContentEntity{
				{EntityID: "e-1", RepoID: "repo-1", EntityType: "Function", EntityName: "A", SourceCache: "func A(){}"},
			},
		}},
		// Returning an error after delivering all pages simulates a later-page
		// DB timeout; use errAfterPages to inject it at the callback level.
	}
	loader.errAfterPages = 1 // fail after delivering the first page

	writer := &capturingSearchDocWriter{}
	handler := EshuSearchDocumentHandler{Loader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), searchDocIntent())
	if err == nil {
		t.Fatal("expected stream error to propagate")
	}
	// Session must have been cancelled (not finalized).
	if writer.cancelCalls != 1 {
		t.Fatalf("cancel calls = %d, want 1 (mid-stream error must cancel)", writer.cancelCalls)
	}
	if writer.finalizeCalls != 0 {
		t.Fatalf("finalize calls = %d, want 0 (stream errored before finalize)", writer.finalizeCalls)
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
