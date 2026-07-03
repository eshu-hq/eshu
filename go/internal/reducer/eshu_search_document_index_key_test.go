// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
)

// TestWriteEshuSearchDocumentsUsesBoundedSearchTermKeys verifies that the bulk
// term insert query uses bounded term_key values (truncated by searchhybrid.TermKey)
// and that the query shape satisfies the bulk unnest contract introduced by the
// #3430 batched-write fix: $3=document_ids, $4=terms, $5=term_keys, $6=frequencies.
func TestWriteEshuSearchDocumentsUsesBoundedSearchTermKeys(t *testing.T) {
	t.Parallel()

	longTerm := strings.Repeat("a", 4096)
	doc := searchdocs.Document{
		ID:          "searchdoc:content_entity:long-term",
		RepoID:      "repo-1",
		SourceKind:  searchdocs.SourceKindCodeEntity,
		ContextText: longTerm,
		TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    []searchdocs.Document{doc},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	var termUpsert fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO eshu_search_index_terms") {
			termUpsert = exec
			break
		}
	}
	if termUpsert.query == "" {
		t.Fatalf("missing search index term insert: %#v", db.execs)
	}
	// Bulk shape: scope($1), gen($2), docIDs($3), terms($4), termKeys($5), freqs($6).
	for _, fragment := range []string{
		"term_key",
		"unnest($3::text[], $4::text[], $5::text[], $6::int[])",
	} {
		if !strings.Contains(termUpsert.query, fragment) {
			t.Fatalf("term insert query missing %q:\n%s", fragment, termUpsert.query)
		}
	}
	if strings.Contains(termUpsert.query, "ON CONFLICT") {
		t.Fatalf("term insert query must not use conflict-update path after page refresh:\n%s", termUpsert.query)
	}
	if !strings.Contains(termUpsert.query, "ORDER BY term_key, document_id") {
		t.Fatalf("term insert query must order rows by the primary-key suffix for index locality:\n%s", termUpsert.query)
	}
	if got, want := len(termUpsert.args), 6; got != want {
		t.Fatalf("term insert args = %d, want %d", got, want)
	}
	// args[2] = document_id slice (one entry per term), args[3] = terms,
	// args[4] = term_keys, args[5] = frequencies.
	terms, ok := termUpsert.args[3].([]string)
	if !ok {
		t.Fatalf("term arg type = %T, want []string", termUpsert.args[3])
	}
	termKeys, ok := termUpsert.args[4].([]string)
	if !ok {
		t.Fatalf("term key arg type = %T, want []string", termUpsert.args[4])
	}
	frequencies, ok := termUpsert.args[5].([]int)
	if !ok {
		t.Fatalf("frequency arg type = %T, want []int", termUpsert.args[5])
	}
	if len(terms) != len(termKeys) || len(terms) != len(frequencies) {
		t.Fatalf("term arrays are misaligned: terms=%d keys=%d freqs=%d", len(terms), len(termKeys), len(frequencies))
	}
	for i, term := range terms {
		if term != longTerm {
			continue
		}
		if got, want := termKeys[i], searchhybrid.TermKey(longTerm); got != want {
			t.Fatalf("long term key = %q, want %q", got, want)
		}
		if frequencies[i] != 1 {
			t.Fatalf("long term frequency = %d, want 1", frequencies[i])
		}
		return
	}
	t.Fatalf("long term missing from persisted terms: %v", terms)
}

func TestWriteEshuSearchDocumentsUsesTermCopierWhenAvailable(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	copier := &fakeSearchIndexTermCopier{}
	writer := PostgresEshuSearchDocumentWriter{
		DB:               db,
		SearchTermCopier: copier,
	}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			{
				ID:          "searchdoc:content_entity:e-1",
				RepoID:      "repo-1",
				SourceKind:  searchdocs.SourceKindCodeEntity,
				ContextText: "zeta alpha",
				TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
				Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
			},
			{
				ID:          "searchdoc:content_entity:e-2",
				RepoID:      "repo-1",
				SourceKind:  searchdocs.SourceKindCodeEntity,
				ContextText: "alpha beta",
				TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
				Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if got := len(copier.calls); got != 1 {
		t.Fatalf("copy calls = %d, want 1", got)
	}
	call := copier.calls[0]
	if call.scopeID != "scope-1" || call.generationID != "gen-1" {
		t.Fatalf("copy scope/generation = %q/%q, want scope-1/gen-1", call.scopeID, call.generationID)
	}
	if len(call.terms) == 0 || len(call.terms) != len(call.termKeys) || len(call.terms) != len(call.documentIDs) {
		t.Fatalf("copy arrays misaligned: docs=%d terms=%d keys=%d", len(call.documentIDs), len(call.terms), len(call.termKeys))
	}
	for i := 1; i < len(call.terms); i++ {
		prevKey, key := call.termKeys[i-1], call.termKeys[i]
		prevDoc, doc := call.documentIDs[i-1], call.documentIDs[i]
		if prevKey > key || (prevKey == key && prevDoc > doc) {
			t.Fatalf("copy rows not sorted by term_key, document_id at %d: (%q,%q) before (%q,%q)",
				i, prevKey, prevDoc, key, doc)
		}
	}
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO eshu_search_index_terms") {
			t.Fatalf("term insert ExecContext ran despite copy fast path:\n%s", exec.query)
		}
	}
}

func TestWriteSearchIndexTermsCopyPathPreservesPreparedOrder(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	copier := &fakeSearchIndexTermCopier{}
	writer := PostgresEshuSearchDocumentWriter{
		DB:               db,
		SearchTermCopier: copier,
	}

	preparedDocumentIDs := []string{"doc-b", "doc-a", "doc-c"}
	preparedTerms := []string{"beta", "alpha", "gamma"}
	preparedTermKeys := []string{"beta", "alpha", "gamma"}
	preparedFrequencies := []int{2, 1, 3}

	if err := writer.writeSearchIndexTerms(
		context.Background(),
		"scope-1",
		"gen-1",
		preparedDocumentIDs,
		preparedTerms,
		preparedTermKeys,
		preparedFrequencies,
	); err != nil {
		t.Fatalf("writeSearchIndexTerms error = %v", err)
	}
	if got := len(copier.calls); got != 1 {
		t.Fatalf("copy calls = %d, want 1", got)
	}
	call := copier.calls[0]
	if !slices.Equal(call.documentIDs, preparedDocumentIDs) ||
		!slices.Equal(call.terms, preparedTerms) ||
		!slices.Equal(call.termKeys, preparedTermKeys) ||
		!slices.Equal(call.frequencies, preparedFrequencies) {
		t.Fatalf(
			"copy path reordered prepared columns: docs=%v terms=%v keys=%v freqs=%v; want docs=%v terms=%v keys=%v freqs=%v",
			call.documentIDs,
			call.terms,
			call.termKeys,
			call.frequencies,
			preparedDocumentIDs,
			preparedTerms,
			preparedTermKeys,
			preparedFrequencies,
		)
	}
}

func TestWriteSearchIndexTermsFallsBackWhenCopyUnsupported(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	copier := &fakeSearchIndexTermCopier{err: fakeSearchIndexTermCopyUnsupportedError{}}
	writer := PostgresEshuSearchDocumentWriter{
		DB:               db,
		SearchTermCopier: copier,
	}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			{
				ID:          "searchdoc:content_entity:e-1",
				RepoID:      "repo-1",
				SourceKind:  searchdocs.SourceKindCodeEntity,
				ContextText: "alpha beta",
				TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
				Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if got := len(copier.calls); got != 1 {
		t.Fatalf("copy calls = %d, want 1", got)
	}
	var sawFallback bool
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO eshu_search_index_terms") {
			sawFallback = true
			break
		}
	}
	if !sawFallback {
		t.Fatalf("missing unnest fallback insert after unsupported copy: %#v", db.execs)
	}
}

func TestWriteSearchIndexTermsReturnsCopyErrorWithoutFallback(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	copier := &fakeSearchIndexTermCopier{err: errors.New("copy failed")}
	writer := PostgresEshuSearchDocumentWriter{
		DB:               db,
		SearchTermCopier: copier,
	}

	err := writer.writeSearchIndexTerms(
		context.Background(),
		"scope-1",
		"gen-1",
		[]string{"doc-1"},
		[]string{"alpha"},
		[]string{"alpha"},
		[]int{1},
	)
	if err == nil {
		t.Fatal("writeSearchIndexTerms error = nil, want copy failure")
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("fallback execs = %d, want 0 for real copy failure", got)
	}
}

func TestWriteSearchIndexTermsRejectsMisalignedColumns(t *testing.T) {
	t.Parallel()

	writer := PostgresEshuSearchDocumentWriter{DB: &fakeSearchDocExecer{}}
	err := writer.writeSearchIndexTerms(
		context.Background(),
		"scope-1",
		"gen-1",
		[]string{"doc-1"},
		[]string{"alpha", "beta"},
		[]string{"alpha", "beta"},
		[]int{1, 1},
	)
	if err == nil {
		t.Fatal("writeSearchIndexTerms error = nil, want misaligned slice error")
	}
	if !strings.Contains(err.Error(), "requires aligned slices") {
		t.Fatalf("writeSearchIndexTerms error = %v, want aligned-slices message", err)
	}
}

func TestWriteEshuSearchDocumentsClearsGenerationTermsOnce(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			{
				ID:          "searchdoc:content_entity:e-1",
				RepoID:      "repo-1",
				SourceKind:  searchdocs.SourceKindCodeEntity,
				ContextText: "alpha beta",
				TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
				Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
			},
			{
				ID:          "searchdoc:content_entity:e-2",
				RepoID:      "repo-1",
				SourceKind:  searchdocs.SourceKindCodeEntity,
				ContextText: "gamma delta",
				TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
				Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	clearCount := 0
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "DELETE FROM eshu_search_index_terms") &&
			!strings.Contains(exec.query, "document_id") {
			clearCount++
		}
		if strings.Contains(exec.query, "DELETE FROM eshu_search_index_terms") &&
			strings.Contains(exec.query, "document_id") {
			t.Fatalf("term refresh must be generation-scoped, not document-keyed:\n%s", exec.query)
		}
	}
	if clearCount != 1 {
		t.Fatalf("generation term clear count = %d, want 1; execs=%#v", clearCount, db.execs)
	}
}
