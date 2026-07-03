// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
)

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
	var documentUpsert fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO eshu_search_index_documents") {
			sawDocumentUpsert = true
			documentUpsert = exec
		}
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
	if !strings.Contains(documentUpsert.query, "content_hash") {
		t.Fatalf("search-index document upsert does not persist content_hash:\n%s", documentUpsert.query)
	}
	contentHashes, ok := documentUpsert.args[6].([]string)
	if !ok || len(contentHashes) != 1 {
		t.Fatalf("content_hash arg = %T, want one-element []string", documentUpsert.args[6])
	}
	if got, want := contentHashes[0], searchhybrid.DocumentContentHash(sampleSearchDoc("searchdoc:content_entity:e-1")); got != want {
		t.Fatalf("content_hash arg = %q, want %q", got, want)
	}
}
