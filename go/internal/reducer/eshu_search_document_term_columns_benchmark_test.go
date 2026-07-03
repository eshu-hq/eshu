// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
)

func BenchmarkBuildSearchIndexTermColumns(b *testing.B) {
	const (
		docCount  = 2000
		termCount = 150
	)
	documents := makeSearchIndexTermColumnBenchmarkDocuments(docCount, termCount)
	b.ReportMetric(float64(docCount*termCount), "rows/op")

	b.Run("global-row-sort", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			documentIDs, terms, termKeys, frequencies := buildSearchIndexTermColumnsWithGlobalSortForBenchmark(documents)
			if len(terms) != docCount*termCount || len(documentIDs) != len(terms) ||
				len(termKeys) != len(terms) || len(frequencies) != len(terms) {
				b.Fatalf("misaligned columns: docs=%d terms=%d keys=%d freqs=%d",
					len(documentIDs), len(terms), len(termKeys), len(frequencies))
			}
		}
	})
	b.Run("bucketed-by-term-key", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			documentIDs, terms, termKeys, frequencies := buildSearchIndexTermColumns(documents)
			if len(terms) != docCount*termCount || len(documentIDs) != len(terms) ||
				len(termKeys) != len(terms) || len(frequencies) != len(terms) {
				b.Fatalf("misaligned columns: docs=%d terms=%d keys=%d freqs=%d",
					len(documentIDs), len(terms), len(termKeys), len(frequencies))
			}
		}
	})
}

func makeSearchIndexTermColumnBenchmarkDocuments(docCount int, termCount int) []eshuSearchIndexDocumentWrite {
	terms := make(map[string]int, termCount)
	for term := 0; term < termCount; term++ {
		terms[fmt.Sprintf("term-%04d", term)] = 1
	}
	documents := make([]eshuSearchIndexDocumentWrite, docCount)
	for doc := 0; doc < docCount; doc++ {
		documents[doc] = eshuSearchIndexDocumentWrite{
			DocumentID: fmt.Sprintf("doc-%06d", doc),
			Terms:      terms,
		}
	}
	return documents
}

func buildSearchIndexTermColumnsWithGlobalSortForBenchmark(
	documents []eshuSearchIndexDocumentWrite,
) ([]string, []string, []string, []int) {
	var (
		documentIDs []string
		terms       []string
		termKeys    []string
		frequencies []int
	)
	for _, doc := range documents {
		if len(doc.Terms) == 0 {
			continue
		}
		sortedTerms, sortedTermKeys, sortedFrequencies := sortedSearchIndexTerms(doc.Terms)
		for j, term := range sortedTerms {
			documentIDs = append(documentIDs, doc.DocumentID)
			terms = append(terms, term)
			termKeys = append(termKeys, sortedTermKeys[j])
			frequencies = append(frequencies, sortedFrequencies[j])
		}
	}
	order := make([]int, len(terms))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i int, j int) bool {
		left, right := order[i], order[j]
		if termKeys[left] != termKeys[right] {
			return termKeys[left] < termKeys[right]
		}
		return documentIDs[left] < documentIDs[right]
	})
	sortedDocumentIDs := make([]string, len(documentIDs))
	sortedTerms := make([]string, len(terms))
	sortedTermKeys := make([]string, len(termKeys))
	sortedFrequencies := make([]int, len(frequencies))
	for out, in := range order {
		sortedDocumentIDs[out] = documentIDs[in]
		sortedTerms[out] = terms[in]
		sortedTermKeys[out] = termKeys[in]
		sortedFrequencies[out] = frequencies[in]
	}
	return sortedDocumentIDs, sortedTerms, sortedTermKeys, sortedFrequencies
}

func TestBuildSearchIndexTermColumnsMatchesGlobalSortOrdering(t *testing.T) {
	t.Parallel()

	documents := makeSearchIndexTermColumnBenchmarkDocuments(25, 17)
	wantDocumentIDs, wantTerms, wantTermKeys, wantFrequencies := buildSearchIndexTermColumnsWithGlobalSortForBenchmark(documents)
	gotDocumentIDs, gotTerms, gotTermKeys, gotFrequencies := buildSearchIndexTermColumns(documents)
	if fmt.Sprint(gotDocumentIDs) != fmt.Sprint(wantDocumentIDs) ||
		fmt.Sprint(gotTerms) != fmt.Sprint(wantTerms) ||
		fmt.Sprint(gotTermKeys) != fmt.Sprint(wantTermKeys) ||
		fmt.Sprint(gotFrequencies) != fmt.Sprint(wantFrequencies) {
		t.Fatalf(
			"bucketed columns differ from global sort\nwant docs=%v terms=%v keys=%v freqs=%v\ngot docs=%v terms=%v keys=%v freqs=%v",
			wantDocumentIDs,
			wantTerms,
			wantTermKeys,
			wantFrequencies,
			gotDocumentIDs,
			gotTerms,
			gotTermKeys,
			gotFrequencies,
		)
	}
}

func TestBuildSearchIndexTermColumnsSortsUnorderedDocuments(t *testing.T) {
	t.Parallel()

	terms := map[string]int{"beta": 2, "alpha": 1}
	documents := []eshuSearchIndexDocumentWrite{
		{DocumentID: "doc-b", Terms: terms},
		{DocumentID: "doc-a", Terms: terms},
	}
	gotDocumentIDs, gotTerms, gotTermKeys, gotFrequencies := buildSearchIndexTermColumns(documents)
	wantDocumentIDs := []string{"doc-a", "doc-b", "doc-a", "doc-b"}
	wantTerms := []string{"alpha", "alpha", "beta", "beta"}
	wantTermKeys := []string{searchhybrid.TermKey("alpha"), searchhybrid.TermKey("alpha"), searchhybrid.TermKey("beta"), searchhybrid.TermKey("beta")}
	wantFrequencies := []int{1, 1, 2, 2}
	if fmt.Sprint(gotDocumentIDs) != fmt.Sprint(wantDocumentIDs) ||
		fmt.Sprint(gotTerms) != fmt.Sprint(wantTerms) ||
		fmt.Sprint(gotTermKeys) != fmt.Sprint(wantTermKeys) ||
		fmt.Sprint(gotFrequencies) != fmt.Sprint(wantFrequencies) {
		t.Fatalf(
			"columns not sorted by primary-key suffix\nwant docs=%v terms=%v keys=%v freqs=%v\ngot docs=%v terms=%v keys=%v freqs=%v",
			wantDocumentIDs,
			wantTerms,
			wantTermKeys,
			wantFrequencies,
			gotDocumentIDs,
			gotTerms,
			gotTermKeys,
			gotFrequencies,
		)
	}
}
