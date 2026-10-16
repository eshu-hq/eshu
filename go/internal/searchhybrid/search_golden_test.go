// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// searchGoldenDigest is the SHA-256 of searchGoldenTranscript as produced by
// Backend.Search before RankAll shared its ranking core (#7746), at the commit
// that sums BM25 terms in sorted order. It pins the candidate order, ranks,
// metadata, and failures exactly, and each score to searchGoldenScoreDigits
// significant digits, for every mode, limit, and overflow case below.
const searchGoldenDigest = "b3fa9fa441889008d0e3e8ac6650ea620ea00ae9a6b8f3ea12fe364d0288d413"

// searchGoldenScoreDigits is how many significant digits of each score the
// transcript keeps. Go may fuse a multiply and add into one FMA instruction on
// arm64 but not on amd64 (GOAMD64=v1), so the same score can differ by a few
// ulp across architectures (4 ulp observed, about 1e-15 relative). Eight
// digits ignore that last-bit noise but still catch a relative score change
// of about 1e-8 or more.
const searchGoldenScoreDigits = 8

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
						fmt.Fprintf(&out, "%s %.*g %s bm25=%s vector=%s overflow=%s failures=%v\n",
							c.Document.ID, searchGoldenScoreDigits, c.Score, c.Metadata["search_method"],
							c.Metadata["bm25_rank"], c.Metadata["vector_rank"], c.Metadata["index_overflow"], c.Failures)
					}
				}
			}
		}
	}
	return out.String()
}

// TestBackendSearchOutputIsPinned pins Backend.Search against the output it
// produced before the RankAll refactor: identical order, ranks, metadata, and
// failures, and scores equal to searchGoldenScoreDigits significant digits.
func TestBackendSearchOutputIsPinned(t *testing.T) {
	t.Parallel()

	transcript := searchGoldenTranscript(t)
	sum := sha256.Sum256([]byte(transcript))
	if got := hex.EncodeToString(sum[:]); got != searchGoldenDigest {
		t.Fatalf("Search transcript digest = %s, want %s\ntranscript:\n%s", got, searchGoldenDigest, transcript)
	}
}
