// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestWriteEshuSearchDocumentsCancelsOnInsertPageError(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{failOn: "INSERT INTO fact_records"}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			sampleSearchDoc("searchdoc:content_entity:e-1"),
		},
	})
	if err == nil {
		t.Fatal("WriteEshuSearchDocuments error = nil, want fact insert error")
	}

	var factDeletes, indexDocDeletes, indexTermClears int
	for _, exec := range db.execs {
		switch {
		case strings.Contains(exec.query, "DELETE FROM fact_records"):
			factDeletes++
			ids, ok := exec.args[3].([]string)
			if !ok || len(ids) != 0 {
				t.Fatalf("cancel fact delete keep-set = %v, want empty []string", exec.args[3])
			}
		case strings.Contains(exec.query, "DELETE FROM eshu_search_index_documents"):
			indexDocDeletes++
		case strings.Contains(exec.query, "DELETE FROM eshu_search_index_terms"):
			indexTermClears++
		}
	}
	if factDeletes != 1 {
		t.Fatalf("fact deletes after failed one-shot insert = %d, want 1", factDeletes)
	}
	if indexDocDeletes != 1 {
		t.Fatalf("index-doc deletes after failed one-shot insert = %d, want 1", indexDocDeletes)
	}
	if indexTermClears != 2 {
		t.Fatalf("index-term clears after failed one-shot insert = %d, want 2 (pre-insert clear plus cancel clear)", indexTermClears)
	}
}
