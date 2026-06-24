// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchrerank

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// defaultRRFK is the Reciprocal Rank Fusion constant used to blend the baseline
// ranked list with the graph-signal ranked list. It matches the fusion constant
// used by searchhybrid so the two stages compose predictably.
const defaultRRFK = 60

// State reports which reranking path produced the returned order. It is part of
// the search response so a client can distinguish measured graph reranking from
// a baseline fallback.
type State string

const (
	// StateDisabled means reranking was not requested; the baseline order is
	// returned unchanged with no graph signals computed.
	StateDisabled State = "disabled"
	// StateApplied means at least one graph signal fired and the results were
	// fused into a graph-aware order.
	StateApplied State = "applied"
	// StateInactive means reranking was requested but no graph signal fired for
	// any result, so the baseline order is returned unchanged.
	StateInactive State = "inactive"
	// StateStale means the supplied graph context was marked stale, so reranking
	// failed closed to the baseline order.
	StateStale State = "stale_skipped"
)

// Contribution is one named graph signal that fired for a result. Handle is the
// bounded "kind:id" form of the matched graph handle; it never carries document
// content, so a ranking basis is safe to return without leaking private text.
type Contribution struct {
	Kind   SignalKind `json:"kind"`
	Handle string     `json:"handle,omitempty"`
	Weight float64    `json:"weight"`
}

// RankingBasis explains how one result was ranked. It preserves the baseline
// rank and score so callers can always recover the lexical/vector ranking, and
// lists the graph contributions that moved the result.
type RankingBasis struct {
	// BaselineRank is the 1-based position before reranking.
	BaselineRank int `json:"baseline_rank"`
	// BaselineScore is the lexical/vector score from the baseline retrieval.
	BaselineScore float64 `json:"baseline_score"`
	// FinalRank is the 1-based position after reranking.
	FinalRank int `json:"final_rank"`
	// GraphBoost is the summed weight of the graph signals that fired.
	GraphBoost float64 `json:"graph_boost"`
	// Contributions lists the graph signals that fired, strongest first.
	Contributions []Contribution `json:"contributions,omitempty"`
}

// RankedResult pairs a baseline retrieval result with its reranking basis. The
// embedded result, including its truth scope and score, is unchanged.
type RankedResult struct {
	Result searchretrieval.Result `json:"result"`
	Basis  RankingBasis           `json:"ranking_basis"`
}

// Options configures one reranking pass. Zero values are safe: an empty Weights
// map selects the default signal weights and a zero RRFK selects the default
// fusion constant.
type Options struct {
	// Enabled turns reranking on. When false the baseline order is returned with
	// State StateDisabled.
	Enabled bool
	// Anchor is the smallest resolved request scope, used to score anchor-exact
	// handle matches more strongly than mere handle presence.
	Anchor searchretrieval.Anchor
	// Scope carries every request anchor so a result handle can be matched to the
	// service, workload, repository, or environment the caller bounded to.
	Scope searchretrieval.Scope
	// Weights overrides the default per-signal weights. Missing keys fall back to
	// the defaults; non-positive overrides disable that signal.
	Weights map[SignalKind]float64
	// GraphStale, when true, fails reranking closed to the baseline order with
	// State StateStale. The caller supplies this from a freshness signal; the
	// reranker never infers staleness on its own.
	GraphStale bool
	// RRFK overrides the Reciprocal Rank Fusion constant. Zero selects 60.
	RRFK int
}

// Outcome is the deterministic result of one reranking pass: the ordered
// results with their ranking basis and the state that records which path
// answered.
type Outcome struct {
	State   State          `json:"state"`
	Results []RankedResult `json:"results"`
}

// Rerank reorders results around graph anchors and returns the ordered results,
// their ranking basis, and the resolved state. It is referentially transparent:
// equal inputs always yield an equal outcome. It never changes the result set,
// only its order, so upstream scope and authorization filtering is preserved.
func Rerank(results []searchretrieval.Result, opts Options) Outcome {
	baseline := baselineRanked(results)
	if !opts.Enabled {
		return Outcome{State: StateDisabled, Results: baselineOnly(baseline)}
	}
	if opts.GraphStale {
		return Outcome{State: StateStale, Results: baselineOnly(baseline)}
	}

	weights := mergedWeights(opts.Weights)
	signals := make([]resultSignals, len(baseline))
	anySignal := false
	for i, entry := range baseline {
		signals[i] = extractSignals(entry.result, opts.Scope, weights)
		if signals[i].boost > 0 {
			anySignal = true
		}
	}
	if !anySignal {
		return Outcome{State: StateInactive, Results: baselineOnly(baseline)}
	}

	fused := fuseRanks(baseline, signals, orDefaultInt(opts.RRFK, defaultRRFK))
	return Outcome{State: StateApplied, Results: fused}
}

// rankedEntry is one baseline result paired with its normalized 1-based rank.
type rankedEntry struct {
	result       searchretrieval.Result
	baselineRank int
}

// baselineRanked normalizes baseline ranks to a dense, deterministic 1-based
// order. A result's own Rank wins when set; otherwise input order is used. Ties
// break by document id so the baseline order is stable for fixed inputs.
func baselineRanked(results []searchretrieval.Result) []rankedEntry {
	entries := make([]rankedEntry, 0, len(results))
	for i, r := range results {
		rank := r.Rank
		if rank <= 0 {
			rank = i + 1
		}
		entries = append(entries, rankedEntry{result: r, baselineRank: rank})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].baselineRank != entries[j].baselineRank {
			return entries[i].baselineRank < entries[j].baselineRank
		}
		return entries[i].result.Document.ID < entries[j].result.Document.ID
	})
	for i := range entries {
		entries[i].baselineRank = i + 1
	}
	return entries
}

// baselineOnly returns the baseline entries as ranked results with a basis that
// records no graph contribution. It is the fail-closed shape shared by the
// disabled, stale, and inactive states.
func baselineOnly(entries []rankedEntry) []RankedResult {
	out := make([]RankedResult, 0, len(entries))
	for _, entry := range entries {
		out = append(out, RankedResult{
			Result: entry.result,
			Basis: RankingBasis{
				BaselineRank:  entry.baselineRank,
				BaselineScore: entry.result.Score,
				FinalRank:     entry.baselineRank,
			},
		})
	}
	return out
}

// fuseRanks blends the baseline ranked list with the graph-signal ranked list
// using Reciprocal Rank Fusion and returns the reranked results with their
// basis. Every baseline result contributes its baseline term; results whose
// graph signal fired contribute a second term, so a graph-anchored result can
// only move up, never drop out.
func fuseRanks(baseline []rankedEntry, signals []resultSignals, k int) []RankedResult {
	graphRank := graphSignalRanks(baseline, signals)
	type scored struct {
		index        int
		baselineRank int
		fused        float64
	}
	scoredEntries := make([]scored, len(baseline))
	for i, entry := range baseline {
		fused := 1.0 / float64(k+entry.baselineRank)
		if gr, ok := graphRank[i]; ok {
			fused += 1.0 / float64(k+gr)
		}
		scoredEntries[i] = scored{index: i, baselineRank: entry.baselineRank, fused: fused}
	}
	sort.SliceStable(scoredEntries, func(i, j int) bool {
		if scoredEntries[i].fused != scoredEntries[j].fused {
			return scoredEntries[i].fused > scoredEntries[j].fused
		}
		if scoredEntries[i].baselineRank != scoredEntries[j].baselineRank {
			return scoredEntries[i].baselineRank < scoredEntries[j].baselineRank
		}
		return baseline[scoredEntries[i].index].result.Document.ID <
			baseline[scoredEntries[j].index].result.Document.ID
	})

	out := make([]RankedResult, 0, len(scoredEntries))
	for finalPos, s := range scoredEntries {
		entry := baseline[s.index]
		sig := signals[s.index]
		out = append(out, RankedResult{
			Result: entry.result,
			Basis: RankingBasis{
				BaselineRank:  entry.baselineRank,
				BaselineScore: entry.result.Score,
				FinalRank:     finalPos + 1,
				GraphBoost:    sig.boost,
				Contributions: sig.contributions,
			},
		})
	}
	return out
}

// graphSignalRanks assigns 1-based ranks to the results whose graph signal
// fired, ordered by descending boost and breaking ties by baseline rank then
// document id so the fused order is deterministic.
func graphSignalRanks(baseline []rankedEntry, signals []resultSignals) map[int]int {
	fired := make([]int, 0, len(signals))
	for i, sig := range signals {
		if sig.boost > 0 {
			fired = append(fired, i)
		}
	}
	sort.SliceStable(fired, func(i, j int) bool {
		li, lj := fired[i], fired[j]
		if signals[li].boost != signals[lj].boost {
			return signals[li].boost > signals[lj].boost
		}
		if baseline[li].baselineRank != baseline[lj].baselineRank {
			return baseline[li].baselineRank < baseline[lj].baselineRank
		}
		return baseline[li].result.Document.ID < baseline[lj].result.Document.ID
	})
	ranks := make(map[int]int, len(fired))
	for position, index := range fired {
		ranks[index] = position + 1
	}
	return ranks
}

// orDefaultInt returns value when it is positive, otherwise fallback.
// Non-positive overrides, including negative ones, select the default so a bad
// RRFK can never invert or zero the fusion denominator.
func orDefaultInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

// handleKey renders a graph handle as the bounded "kind:id" contribution form.
func handleKey(kind, id string) string {
	return fmt.Sprintf("%s:%s", kind, id)
}
