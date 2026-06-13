package searchhybrid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

const (
	defaultMaxDocuments = 50000
	defaultK1           = 1.2
	defaultB            = 0.75
	defaultRRFK         = 60
)

// Options configures a hybrid index. Zero values select safe defaults.
type Options struct {
	// MaxDocuments caps the indexed document count. Documents beyond the cap
	// (after deterministic ordering by id) are dropped and counted as overflow.
	MaxDocuments int
	// Embedder, when set, enables semantic and hybrid retrieval. Without it the
	// index serves BM25 only.
	Embedder Embedder
	// K1 and B are BM25 parameters. Zero selects the standard 1.2 and 0.75.
	K1 float64
	B  float64
	// RRFK is the Reciprocal Rank Fusion constant. Zero selects 60.
	RRFK int
}

type indexedDocument struct {
	doc           searchdocs.Document
	termFrequency map[string]int
	length        int
	vector        []float64
}

// posting is one document's occurrence count of a term in the inverted index.
type posting struct {
	docIndex int
	termFreq int
}

// Index is a bounded in-memory retrieval index over curated search documents.
// BM25 retrieval is served from an inverted index (term -> postings) so a query
// visits only the documents that contain its terms, not the whole corpus.
type Index struct {
	documents  []indexedDocument
	docFreq    map[string]int
	postings   map[string][]posting
	averageLen float64
	count      int
	overflow   int
	embedder   Embedder
	embedCache map[string][]float64
	k1         float64
	b          float64
	rrfK       int
}

// NewIndex builds a bounded index over docs. Documents are ordered by id before
// the cap is applied so the indexed set and overflow count are deterministic for
// a fixed input regardless of the caller's ordering. When an embedder is
// configured each document is embedded once, cached by content hash.
func NewIndex(docs []searchdocs.Document, opts Options) (*Index, error) {
	maxDocuments := opts.MaxDocuments
	if maxDocuments <= 0 {
		maxDocuments = defaultMaxDocuments
	}
	index := &Index{
		docFreq:    make(map[string]int),
		postings:   make(map[string][]posting),
		embedder:   opts.Embedder,
		embedCache: make(map[string][]float64),
		k1:         orDefaultFloat(opts.K1, defaultK1),
		b:          orDefaultFloat(opts.B, defaultB),
		rrfK:       orDefaultInt(opts.RRFK, defaultRRFK),
	}

	ordered := append([]searchdocs.Document(nil), docs...)
	sort.Slice(ordered, func(i int, j int) bool { return ordered[i].ID < ordered[j].ID })
	if len(ordered) > maxDocuments {
		index.overflow = len(ordered) - maxDocuments
		ordered = ordered[:maxDocuments]
	}

	totalLen := 0
	for _, doc := range ordered {
		tokens := tokenize(documentText(doc))
		tf := make(map[string]int, len(tokens))
		for _, token := range tokens {
			tf[token]++
		}
		docIndex := len(index.documents)
		for term, count := range tf {
			index.docFreq[term]++
			index.postings[term] = append(index.postings[term], posting{docIndex: docIndex, termFreq: count})
		}
		entry := indexedDocument{doc: doc, termFrequency: tf, length: len(tokens)}
		if index.embedder != nil {
			vector, err := index.embedDocument(doc)
			if err != nil {
				return nil, fmt.Errorf("embed document %q: %w", doc.ID, err)
			}
			entry.vector = vector
		}
		index.documents = append(index.documents, entry)
		totalLen += len(tokens)
	}
	index.count = len(index.documents)
	if index.count > 0 {
		index.averageLen = float64(totalLen) / float64(index.count)
	}
	return index, nil
}

// Overflow reports how many documents were dropped because the index was full.
func (index *Index) Overflow() int { return index.overflow }

// Size reports how many documents are indexed.
func (index *Index) Size() int { return index.count }

// HasEmbedder reports whether semantic and hybrid retrieval are available.
func (index *Index) HasEmbedder() bool { return index.embedder != nil }

func (index *Index) embedDocument(doc searchdocs.Document) ([]float64, error) {
	text := documentText(doc)
	hash := contentHash(text)
	if cached, ok := index.embedCache[hash]; ok {
		return cached, nil
	}
	vector, err := index.embedder.Embed(text)
	if err != nil {
		return nil, err
	}
	index.embedCache[hash] = vector
	return vector, nil
}

// bm25Score scores one document against the query terms using the precomputed
// corpus statistics. It is kept for direct single-document scoring and tests;
// retrieval uses bm25ScoredInScope, which is driven by the inverted index.
func (index *Index) bm25Score(queryTerms map[string]int, entry indexedDocument) float64 {
	if index.count == 0 || index.averageLen == 0 {
		return 0
	}
	score := 0.0
	for term := range queryTerms {
		tf, ok := entry.termFrequency[term]
		if !ok {
			continue
		}
		score += index.termContribution(term, tf, entry.length)
	}
	return score
}

// bm25ScoredInScope ranks documents for the query using the inverted index,
// visiting only postings of the query terms and accumulating a score per
// in-scope document. inScope reports whether a document index is inside the
// resolved request scope. The result contains only documents with at least one
// matching term, so zero-overlap documents never appear.
func (index *Index) bm25ScoredInScope(queryTerms map[string]int, inScope func(int) bool) map[int]float64 {
	scores := make(map[int]float64)
	if index.count == 0 || index.averageLen == 0 {
		return scores
	}
	for term := range queryTerms {
		for _, p := range index.postings[term] {
			if !inScope(p.docIndex) {
				continue
			}
			scores[p.docIndex] += index.termContribution(term, p.termFreq, index.documents[p.docIndex].length)
		}
	}
	return scores
}

// termContribution is one term's BM25 contribution to a document's score.
func (index *Index) termContribution(term string, termFreq int, docLength int) float64 {
	df := index.docFreq[term]
	idf := math.Log(1 + (float64(index.count)-float64(df)+0.5)/(float64(df)+0.5))
	norm := float64(termFreq) * (index.k1 + 1)
	denom := float64(termFreq) + index.k1*(1-index.b+index.b*float64(docLength)/index.averageLen)
	return idf * norm / denom
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func tokenCounts(text string) map[string]int {
	counts := make(map[string]int)
	for _, token := range tokenize(text) {
		counts[token]++
	}
	return counts
}

// documentText is the searchable text projected from a curated document.
func documentText(doc searchdocs.Document) string {
	parts := []string{doc.Title, doc.ContextText, doc.Path}
	parts = append(parts, doc.Labels...)
	return strings.Join(parts, " ")
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func cosineSimilarity(a []float64, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	dot, normA, normB := 0.0, 0.0, 0.0
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func orDefaultFloat(value float64, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func orDefaultInt(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
