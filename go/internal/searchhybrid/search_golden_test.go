// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// searchGoldenDigest is the SHA-256 of searchGoldenTranscript as produced by
// Backend.Search before RankAll shared its ranking core (#7746). It pins the
// candidate order, score bits, and metadata of every mode, limit, and overflow
// case below, so the refactor must leave Search byte-identical.
const searchGoldenDigest = "bff1157a400094633bc62e1abbd6f8f30815c20028997e2f0de20ae5db192ed5"

// searchGoldenCorpus mixes the scoped fixture corpus with the multi-term
// score-order documents, so ranking covers cross-repo scoping, ties, and
// multi-term BM25 sums.
func searchGoldenCorpus(t *testing.T) []searchdocs.Document {
	t.Helper()
	docs := corpus()
	for _, entry := range scoreOrderIndex(t).documents {
		docs = append(docs, entry.doc)
	}
	return docs
}

// searchGoldenTranscript renders Search output for a fixed matrix of queries,
// modes, limits, and index caps into one line per candidate.
func searchGoldenTranscript(t *testing.T) string {
	t.Helper()
	queries := []string{
		"payment refund",
		"auth token login",
		"invoice totals",
		strings.Join(scoreOrderTerms, " "),
		"alpha filler padding",
	}
	modes := []searchbench.Mode{searchbench.ModeKeyword, searchbench.ModeSemantic, searchbench.ModeHybrid}
	var out strings.Builder
	for _, maxDocs := range []int{0, 5} {
		index := mustIndex(t, searchGoldenCorpus(t), Options{
			MaxDocuments: maxDocs,
			Embedder:     &bagOfWordsEmbedder{dims: 32},
		})
		backend := Backend{Index: index}
		for _, query := range queries {
			for _, mode := range modes {
				for _, limit := range []int{1, 3, 50} {
					candidates, err := backend.Search(context.Background(), request(query, "repo-1", mode, limit))
					if err != nil {
						t.Fatalf("Search(%q, %s, %d) error = %v", query, mode, limit, err)
					}
					fmt.Fprintf(&out, "# cap=%d query=%q mode=%s limit=%d n=%d\n", maxDocs, query, mode, limit, len(candidates))
					for _, c := range candidates {
						fmt.Fprintf(&out, "%s %016x %s bm25=%s vector=%s overflow=%s failures=%v\n",
							c.Document.ID, math.Float64bits(c.Score), c.Metadata["search_method"],
							c.Metadata["bm25_rank"], c.Metadata["vector_rank"], c.Metadata["index_overflow"], c.Failures)
					}
				}
			}
		}
	}
	return out.String()
}

// TestBackendSearchOutputIsPinned pins Backend.Search byte-for-byte against the
// output it produced before the RankAll refactor.
func TestBackendSearchOutputIsPinned(t *testing.T) {
	t.Parallel()

	transcript := searchGoldenTranscript(t)
	sum := sha256.Sum256([]byte(transcript))
	if got := hex.EncodeToString(sum[:]); got != searchGoldenDigest {
		t.Fatalf("Search transcript digest = %s, want %s\ntranscript:\n%s", got, searchGoldenDigest, transcript)
	}
}
