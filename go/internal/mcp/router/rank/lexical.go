// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"sync/atomic"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// BM25 parameters. These are the standard defaults; the field weights in
// index.go are where this ranker is tuned.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// anchorBoost is the multiplicative score bonus per supplied anchor a tool's
// input schema accepts. It reorders near-ties toward tools that take the ids
// the caller already has; it never creates a match on its own.
const anchorBoost = 0.25

// scoreQuantum rounds scores before comparing so float noise below 1e-9 cannot
// split a tie that the Canonical, Cost, and name rules must decide.
const scoreQuantum = 1e9

// Query bounds. The intent is caller-supplied text, so Rank reads only its
// first maxIntentBytes and scores only the first maxQueryTerms distinct terms.
// A real intent is a sentence; the cap keeps a hostile one from costing more
// than a normal query.
const (
	maxIntentBytes = 2048
	maxQueryTerms  = 32
)

// ErrNilAllowed is returned when Rank receives a nil allowed func. Rank fails
// closed: with no visibility rule it cannot know which tools the caller may see.
var ErrNilAllowed = errors.New("rank: allowed func is nil")

// Lexical is a deterministic BM25-style router.Ranker over tool names,
// curated intents, and descriptions.
//
// The corpus index is built once by New and shared read-only by every Rank
// call, so a Lexical is safe for concurrent use. The index keeps the tools slice
// and entries map it was built from. Rank reuses it only when handed that same
// slice (same backing array and length) and that same map: a different slice or
// map, even one with equal names or size, is ranked from a fresh uncached index.
// Callers should therefore keep one tools slice and one entries map for the life
// of the server. Elements of the slice and entries of the map must not be edited
// in place after New; an in-place edit is not detected, and the index serves the
// text it saw at New.
type Lexical struct {
	idx    *index
	builds atomic.Int64
}

var _ router.Ranker = (*Lexical)(nil)

// New builds the ranker's index from the registry tools and catalog entries.
// Call it once per server start.
func New(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) *Lexical {
	l := &Lexical{}
	l.idx = l.build(tools, entries)
	return l
}

func (l *Lexical) build(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) *index {
	l.builds.Add(1)
	return buildIndex(tools, entries)
}

// scored is one matching tool before ordering.
type scored struct {
	d     *doc
	score float64
	key   int64
	pos   int
}

// Rank returns at most q.Limit candidates (capped at router.MaxLimit), best
// first. It first removes every tool allowed rejects, then scores only the
// rest: document frequencies and average length come from the allowed tools
// alone, so a hidden tool never shifts a visible tool's score. A blank intent,
// an intent that is all stopwords, an intent with no match, and a Limit below 1
// return no candidates and no error. Candidates with equal scores order by
// Canonical, then cheaper Cost, then name, then registry position.
func (l *Lexical) Rank(ctx context.Context, q router.Query, tools []toolcontract.ToolDefinition,
	entries map[string]catalog.Entry, allowed func(name string) bool,
) ([]router.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if allowed == nil {
		return nil, ErrNilAllowed
	}
	terms := queryTerms(q.Intent)
	limit := min(q.Limit, router.MaxLimit)
	if len(terms) == 0 || limit < 1 {
		return nil, nil
	}
	idx := l.idx
	if !idx.matches(tools, entries) {
		idx = l.build(tools, entries)
	}

	live := make([]*doc, 0, len(idx.docs))
	var totalLen float64
	for _, d := range idx.docs {
		if allowed(d.name) {
			live = append(live, d)
			totalLen += d.length
		}
	}
	if len(live) == 0 {
		return nil, nil
	}
	avgLen := totalLen / float64(len(live))
	idf := inverseDocFrequency(live, terms)
	anchors := anchorProps(q.Anchors)

	hits := make([]scored, 0, len(live))
	for pos, d := range live {
		s := bm25(d, terms, idf, avgLen)
		if s <= 0 {
			continue
		}
		for _, prop := range anchors {
			if _, ok := d.props[prop]; ok {
				s *= 1 + anchorBoost
			}
		}
		hits = append(hits, scored{d: d, score: s, key: int64(math.Round(s * scoreQuantum)), pos: pos})
	}
	slices.SortFunc(hits, compareScored)
	if len(hits) > limit {
		hits = hits[:limit]
	}

	out := make([]router.Candidate, 0, len(hits))
	for _, h := range hits {
		out = append(out, router.Candidate{
			Name:        h.d.name,
			Why:         why(h.d, terms, idf),
			Cost:        h.d.cost,
			InputSchema: h.d.schema,
			Example:     example(h.d.schema, q.Anchors),
			Score:       h.score,
		})
	}
	return out, nil
}

// compareScored orders by score descending, then Canonical, cheaper Cost, name,
// and finally registry position so equal names still order deterministically.
func compareScored(a, b scored) int {
	if c := cmp.Compare(b.key, a.key); c != 0 {
		return c
	}
	if a.d.canonical != b.d.canonical {
		if a.d.canonical {
			return -1
		}
		return 1
	}
	if c := cmp.Compare(a.d.costRank, b.d.costRank); c != 0 {
		return c
	}
	if c := cmp.Compare(a.d.name, b.d.name); c != 0 {
		return c
	}
	return cmp.Compare(a.pos, b.pos)
}

// queryTerms tokenizes a caller intent into at most maxQueryTerms distinct
// terms drawn from its first maxIntentBytes.
func queryTerms(intent string) []string {
	return uniqueTerms(tokens(clipIntent(intent)))
}

// clipIntent returns at most maxIntentBytes of s, cut on a rune boundary.
func clipIntent(s string) string {
	if len(s) <= maxIntentBytes {
		return s
	}
	cut := maxIntentBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// uniqueTerms drops repeated query terms, keeping first-occurrence order, so a
// word typed twice does not count twice, and stops at maxQueryTerms.
func uniqueTerms(toks []string) []string {
	seen := make(map[string]struct{}, min(len(toks), maxQueryTerms))
	out := make([]string, 0, min(len(toks), maxQueryTerms))
	for _, t := range toks {
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) == maxQueryTerms {
			break
		}
	}
	return out
}

// inverseDocFrequency computes the BM25 idf of each query term over the live
// (allowed) tools only. The ln(1+x) form is never negative, so a term present
// in every tool contributes little instead of subtracting.
func inverseDocFrequency(live []*doc, terms []string) []float64 {
	n := float64(len(live))
	idf := make([]float64, len(terms))
	for i, term := range terms {
		df := 0
		for _, d := range live {
			if _, ok := d.tf[term]; ok {
				df++
			}
		}
		idf[i] = math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
	}
	return idf
}

// bm25 scores one tool against the query terms using field-weighted term
// frequencies and field-weighted length.
func bm25(d *doc, terms []string, idf []float64, avgLen float64) float64 {
	norm := bm25K1 * (1 - bm25B + bm25B*d.length/avgLen)
	var s float64
	for i, term := range terms {
		tf := d.tf[term]
		if tf == 0 {
			continue
		}
		s += idf[i] * tf * (bm25K1 + 1) / (tf + norm)
	}
	return s
}

// anchorProps lists the input-schema property names for the anchors the caller
// supplied, in a fixed order.
func anchorProps(a router.Anchors) []string {
	var out []string
	for _, p := range anchorPairs(a) {
		out = append(out, p.prop)
	}
	return out
}

type anchorPair struct{ prop, value string }

// anchorPairs maps each non-empty anchor to the property name tools use for it.
func anchorPairs(a router.Anchors) []anchorPair {
	var out []anchorPair
	for _, p := range []anchorPair{
		{"repo_id", a.RepoID},
		{"service_id", a.ServiceID},
		{"workload_id", a.WorkloadID},
		{"environment", a.Environment},
	} {
		if p.value != "" {
			out = append(out, p)
		}
	}
	return out
}
