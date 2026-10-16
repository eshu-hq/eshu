// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// scoreOrderRuns is how many times each scoring path runs. Go randomizes map
// iteration per range statement, so a sum taken in map order lands on more
// than one summation order across this many runs.
const scoreOrderRuns = 200

// scoreOrderTerms are the query terms. Each one appears in the target document
// with a different term frequency and in a different number of filler
// documents, so every term contributes a distinct float and the sum's bits
// depend on the order the contributions are added (#7746).
var scoreOrderTerms = []string{
	"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
}

// scoreOrderIndex builds an index whose target document "target" holds every
// scoreOrderTerms term, and whose filler documents give each term its own
// document frequency.
func scoreOrderIndex(t *testing.T) *Index {
	t.Helper()
	var target []string
	for i, term := range scoreOrderTerms {
		for n := 0; n <= i; n++ {
			target = append(target, term)
		}
	}
	docs := []searchdocs.Document{doc("target", "repo-1", "target", strings.Join(target, " "))}
	for i := range scoreOrderTerms {
		body := strings.Join(scoreOrderTerms[:i+1], " ") + " filler padding words"
		docs = append(docs, doc(fmt.Sprintf("filler-%02d", i), "repo-1", "filler", body))
	}
	return mustIndex(t, docs, Options{})
}

// sortedTermReference sums the target document's term contributions in sorted
// term order, starting from zero, which is the stable rule the index applies.
func sortedTermReference(index *Index, entry indexedDocument) float64 {
	terms := append([]string(nil), scoreOrderTerms...)
	sort.Strings(terms)
	sum := 0.0
	for _, term := range terms {
		sum += index.termContribution(term, entry.termFrequency[term], entry.length)
	}
	return sum
}

// queryTermMaps returns the same query terms built by inserting the terms in
// several different orders, so the map layout differs between them.
func queryTermMaps() []map[string]int {
	orders := [][]string{
		scoreOrderTerms,
		reversed(scoreOrderTerms),
		append(append([]string(nil), scoreOrderTerms[4:]...), scoreOrderTerms[:4]...),
	}
	maps := make([]map[string]int, 0, len(orders)+1)
	for _, order := range orders {
		terms := make(map[string]int, len(order))
		for _, term := range order {
			terms[term] = 1
		}
		maps = append(maps, terms)
	}
	return append(maps, tokenCounts(strings.Join(scoreOrderTerms, " ")))
}

func reversed(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[len(values)-1-i] = value
	}
	return out
}

// TestBM25ScoreBytesStableAcrossTermOrder pins that a document's BM25 score is
// bit-identical on every request within one process and across query-term
// maps built in any insertion order. Float addition is not associative, so
// summing per-term contributions in map-iteration order changes the score bits
// from run to run (#7746, the in-process twin of #7737). Both the
// inverted-index path and the direct single-document path must sum in sorted
// term order.
//
// The test holds on every architecture: the reference is computed in the same
// binary, with the same termContribution and the same summation order, and no
// product feeds the summing add directly, so FMA fusion cannot treat the
// reference and production sums differently. It does not compare bits across
// architectures; see sortedQueryTerms for that limit.
func TestBM25ScoreBytesStableAcrossTermOrder(t *testing.T) {
	t.Parallel()

	index := scoreOrderIndex(t)
	targetIdx := -1
	for i := range index.documents {
		if index.documents[i].doc.ID == "target" {
			targetIdx = i
		}
	}
	if targetIdx < 0 {
		t.Fatal("target document not indexed")
	}
	entry := index.documents[targetIdx]
	want := math.Float64bits(sortedTermReference(index, entry))
	allInScope := func(int) bool { return true }

	directMismatch, postingsMismatch := 0, 0
	for _, terms := range queryTermMaps() {
		for run := 0; run < scoreOrderRuns; run++ {
			if got := math.Float64bits(index.bm25Score(terms, entry)); got != want {
				directMismatch++
			}
			scores := index.bm25ScoredInScope(terms, allInScope)
			if got := math.Float64bits(scores[targetIdx]); got != want {
				postingsMismatch++
			}
		}
	}
	total := len(queryTermMaps()) * scoreOrderRuns
	if directMismatch != 0 {
		t.Errorf("bm25Score bits differ from sorted-term reference in %d/%d runs", directMismatch, total)
	}
	if postingsMismatch != 0 {
		t.Errorf("bm25ScoredInScope bits differ from sorted-term reference in %d/%d runs", postingsMismatch, total)
	}

	// The keyword candidate score a caller reads is the same sum, so it must
	// carry the same bits on every request.
	backend := Backend{Index: index}
	query := strings.Join(scoreOrderTerms, " ")
	candidateMismatch := 0
	for run := 0; run < scoreOrderRuns; run++ {
		candidates, err := backend.Search(context.Background(), request(query, "repo-1", searchbench.ModeKeyword, 20))
		if err != nil {
			t.Fatalf("Search error = %v", err)
		}
		found := false
		for _, candidate := range candidates {
			if candidate.Document.ID != "target" {
				continue
			}
			found = true
			if math.Float64bits(candidate.Score) != want {
				candidateMismatch++
			}
		}
		if !found {
			t.Fatalf("run %d: target document missing from keyword candidates", run)
		}
	}
	if candidateMismatch != 0 {
		t.Errorf("keyword candidate score bits differ from sorted-term reference in %d/%d runs", candidateMismatch, scoreOrderRuns)
	}
}
