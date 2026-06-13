package searchhybrid

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestNewIndexCapsAndSignalsOverflow(t *testing.T) {
	t.Parallel()

	docs := fiveDocs(t)
	index, err := NewIndex(docs, Options{MaxDocuments: 2})
	if err != nil {
		t.Fatalf("NewIndex error = %v", err)
	}
	if index.Size() != 2 {
		t.Errorf("size = %d, want 2", index.Size())
	}
	if index.Overflow() != len(docs)-2 {
		t.Errorf("overflow = %d, want %d", index.Overflow(), len(docs)-2)
	}
	// The cap keeps the lowest document ids deterministically.
	if index.documents[0].doc.ID != "d-1" || index.documents[1].doc.ID != "d-2" {
		t.Errorf("capped ids = %q,%q, want d-1,d-2", index.documents[0].doc.ID, index.documents[1].doc.ID)
	}
}

func TestOverflowSurfacesAsFailureClass(t *testing.T) {
	t.Parallel()

	backend := Backend{Index: mustIndex(t, corpus(), Options{MaxDocuments: 1})}
	candidates, err := backend.Search(context.Background(), request("payment", "repo-1", searchbench.ModeKeyword, 5))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(candidates) == 0 {
		t.Skip("capped corpus produced no in-scope candidate for this query")
	}
	for _, candidate := range candidates {
		if candidate.Metadata["index_overflow"] != "true" {
			t.Errorf("index_overflow metadata = %q, want true", candidate.Metadata["index_overflow"])
		}
		found := false
		for _, failure := range candidate.Failures {
			if failure == searchbench.FailureClassTruncation {
				found = true
			}
		}
		if !found {
			t.Errorf("expected truncation failure class on overflow, got %v", candidate.Failures)
		}
	}
}

func TestEmbeddingCacheReusesByContentHash(t *testing.T) {
	t.Parallel()

	embedder := &bagOfWordsEmbedder{dims: 16}
	// Two documents with identical searchable text must embed once.
	docs := identicalTextDocs(t)
	if _, err := NewIndex(docs, Options{Embedder: embedder}); err != nil {
		t.Fatalf("NewIndex error = %v", err)
	}
	if embedder.calls != 1 {
		t.Errorf("embedder calls = %d, want 1 (cached by content hash)", embedder.calls)
	}
}

func TestCosineSimilarityEdgeCases(t *testing.T) {
	t.Parallel()

	if got := cosineSimilarity(nil, nil); got != 0 {
		t.Errorf("cosine(nil,nil) = %v, want 0", got)
	}
	if got := cosineSimilarity([]float64{1, 0}, []float64{1, 0, 0}); got != 0 {
		t.Errorf("mismatched dims = %v, want 0", got)
	}
	if got := cosineSimilarity([]float64{0, 0}, []float64{1, 1}); got != 0 {
		t.Errorf("zero vector = %v, want 0", got)
	}
	if got := cosineSimilarity([]float64{1, 1}, []float64{1, 1}); got < 0.999 {
		t.Errorf("identical vectors = %v, want ~1", got)
	}
}

func TestBM25ScoreZeroWithoutOverlap(t *testing.T) {
	t.Parallel()

	index := mustIndex(t, corpus(), Options{})
	score := index.bm25Score(tokenCounts("nonexistentterm"), index.documents[0])
	if score != 0 {
		t.Errorf("bm25 score for non-matching term = %v, want 0", score)
	}
}

// fiveDocs returns five documents with ids d-1..d-5 for cap ordering tests.
func fiveDocs(t *testing.T) []searchdocs.Document {
	t.Helper()
	return []searchdocs.Document{
		doc("d-5", "repo-1", "five", "five"),
		doc("d-1", "repo-1", "one", "one"),
		doc("d-3", "repo-1", "three", "three"),
		doc("d-2", "repo-1", "two", "two"),
		doc("d-4", "repo-1", "four", "four"),
	}
}

func identicalTextDocs(t *testing.T) []searchdocs.Document {
	t.Helper()
	return []searchdocs.Document{
		doc("dup-1", "repo-1", "same title", "same body text"),
		doc("dup-2", "repo-1", "same title", "same body text"),
	}
}
