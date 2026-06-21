package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
)

// TestWriteEshuSearchDocumentsUsesBoundedSearchTermKeys verifies that the bulk
// term upsert query uses bounded term_key values (truncated by searchhybrid.TermKey)
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
		t.Fatalf("missing search index term upsert: %#v", db.execs)
	}
	// Bulk shape: scope($1), gen($2), docIDs($3), terms($4), termKeys($5), freqs($6).
	for _, fragment := range []string{
		"term_key",
		"unnest($3::text[], $4::text[], $5::text[], $6::int[])",
		"ON CONFLICT (scope_id, generation_id, term_key, document_id)",
	} {
		if !strings.Contains(termUpsert.query, fragment) {
			t.Fatalf("term upsert query missing %q:\n%s", fragment, termUpsert.query)
		}
	}
	if got, want := len(termUpsert.args), 6; got != want {
		t.Fatalf("term upsert args = %d, want %d", got, want)
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
