// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchrerank"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// maxRecommendedNextCalls bounds the recommended next-call list so the search
// response stays small and deterministic.
const maxRecommendedNextCalls = 6

// topResultsForNextCalls bounds how many reranked results contribute next-call
// suggestions, keeping the synthesis bounded.
const topResultsForNextCalls = 5

// semanticSearchRerank reports the reranking outcome for one search response.
type semanticSearchRerank struct {
	// State is the searchrerank state: disabled, applied, inactive, or
	// stale_skipped.
	State string `json:"state"`
	// Applied is true only when graph signals reordered the results.
	Applied bool `json:"applied"`
}

// semanticSearchRankingBasis explains how one result was ranked. It always keeps
// the baseline lexical/vector rank and score so the original ranking is
// recoverable.
type semanticSearchRankingBasis struct {
	BaselineRank  int                          `json:"baseline_rank"`
	BaselineScore float64                      `json:"baseline_score"`
	FinalRank     int                          `json:"final_rank"`
	GraphBoost    float64                      `json:"graph_boost"`
	Contributions []semanticSearchContribution `json:"contributions,omitempty"`
}

// semanticSearchContribution is one graph signal that moved a result. Handle is
// the bounded "kind:id" form only; it never carries document content.
type semanticSearchContribution struct {
	Kind   string  `json:"kind"`
	Handle string  `json:"handle,omitempty"`
	Weight float64 `json:"weight"`
}

// semanticSearchCall is one recommended bounded follow-up call. It names a
// first-class tool, the bounded arguments, and why the call helps.
type semanticSearchCall struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Reason    string         `json:"reason"`
}

// semanticSearchOrderedResult is one result in final response order with its
// resolved rank and optional ranking basis.
type semanticSearchOrderedResult struct {
	result searchretrieval.Result
	rank   int
	basis  *semanticSearchRankingBasis
}

// semanticSearchRerankResults applies graph-neighborhood reranking when
// requested and returns the response-ordered results, the reranking summary, and
// the recommended next calls. When reranking is off it returns the baseline
// order with no basis, summary, or next calls, keeping the default contract
// unchanged.
func semanticSearchRerankResults(
	req searchretrieval.Request,
	retrieval searchretrieval.Response,
	rerank bool,
) ([]semanticSearchOrderedResult, *semanticSearchRerank, []semanticSearchCall) {
	if !rerank {
		ordered := make([]semanticSearchOrderedResult, 0, len(retrieval.Results))
		for i, result := range retrieval.Results {
			rank := result.Rank
			if rank <= 0 {
				rank = i + 1
			}
			ordered = append(ordered, semanticSearchOrderedResult{result: result, rank: rank})
		}
		return ordered, nil, nil
	}

	outcome := searchrerank.Rerank(retrieval.Results, searchrerank.Options{
		Enabled:    true,
		Anchor:     retrieval.Anchor,
		Scope:      req.Scope,
		GraphStale: semanticSearchGraphStale(retrieval.Results),
	})
	ordered := make([]semanticSearchOrderedResult, 0, len(outcome.Results))
	for _, ranked := range outcome.Results {
		ordered = append(ordered, semanticSearchOrderedResult{
			result: ranked.Result,
			rank:   ranked.Basis.FinalRank,
			basis:  semanticSearchRankingBasisFrom(ranked.Basis),
		})
	}
	info := &semanticSearchRerank{
		State:   string(outcome.State),
		Applied: outcome.State == searchrerank.StateApplied,
	}
	return ordered, info, semanticSearchNextCalls(req, outcome.Results)
}

// semanticSearchGraphStale reports whether any retrieved result is non-fresh.
// Reranking fails closed to baseline when graph context is stale; today curated
// documents only carry the fresh state, so this is the freshness seam that turns
// reranking off the moment a non-fresh state appears.
func semanticSearchGraphStale(results []searchretrieval.Result) bool {
	for _, result := range results {
		if result.Freshness.State != searchdocs.FreshnessFresh {
			return true
		}
	}
	return false
}

// semanticSearchRankingBasisFrom maps a rerank basis to the wire shape.
func semanticSearchRankingBasisFrom(basis searchrerank.RankingBasis) *semanticSearchRankingBasis {
	contributions := make([]semanticSearchContribution, 0, len(basis.Contributions))
	for _, c := range basis.Contributions {
		contributions = append(contributions, semanticSearchContribution{
			Kind:   string(c.Kind),
			Handle: c.Handle,
			Weight: c.Weight,
		})
	}
	if len(contributions) == 0 {
		contributions = nil
	}
	return &semanticSearchRankingBasis{
		BaselineRank:  basis.BaselineRank,
		BaselineScore: basis.BaselineScore,
		FinalRank:     basis.FinalRank,
		GraphBoost:    basis.GraphBoost,
		Contributions: contributions,
	}
}

// semanticSearchNextCalls synthesizes bounded, deterministic follow-up calls from
// the resolved anchor and the reranked results' graph handles. Every suggested
// tool is a first-class read tool; search stays read-only context discovery and
// never infers graph truth on its own.
func semanticSearchNextCalls(
	req searchretrieval.Request,
	ranked []searchrerank.RankedResult,
) []semanticSearchCall {
	calls := make([]semanticSearchCall, 0, maxRecommendedNextCalls)
	seen := make(map[string]bool)
	add := func(tool, reason string, args map[string]any) {
		if tool == "" || seen[tool] || len(calls) >= maxRecommendedNextCalls {
			return
		}
		seen[tool] = true
		calls = append(calls, semanticSearchCall{Tool: tool, Arguments: args, Reason: reason})
	}

	anchor := req.Scope.Anchor()
	if anchor.Kind == searchretrieval.ScopeKindService && anchor.ID != "" {
		add("get_service_story", "tell the story of the anchored service and cite its evidence",
			map[string]any{"workload_id": anchor.ID})
	}

	for i, item := range ranked {
		if i >= topResultsForNextCalls {
			break
		}
		for _, h := range item.Result.Handles {
			tool := handleSignalTool(h.Kind)
			if tool == "" {
				continue
			}
			add(tool, nextCallReason(tool), nextCallArguments(tool, h.ID))
		}
	}

	if len(ranked) > 0 {
		add("build_evidence_citation_packet", "cite the evidence handles the results return", nil)
	}
	if len(calls) == 0 {
		return nil
	}
	return calls
}

// nextCallArguments builds the executable arguments for a follow-up tool from a
// graph handle id, using the tool's actual dispatch schema keys so a client that
// follows the recommendation reaches the bounded read rather than a route error.
func nextCallArguments(tool, handleID string) map[string]any {
	switch tool {
	case "get_service_story":
		return map[string]any{"workload_id": handleID}
	case "trace_deployment_chain":
		return map[string]any{"service_name": handleID}
	case "get_incident_context":
		return map[string]any{"incident_id": handleID}
	case "explain_supply_chain_impact":
		return map[string]any{"subject_digest": handleID}
	default:
		return nil
	}
}

// nextCallReason explains why a follow-up tool advances the investigation.
func nextCallReason(tool string) string {
	switch tool {
	case "get_service_story":
		return "resolve the service story behind a top result"
	case "trace_deployment_chain":
		return "trace the deployment chain for a top result's service"
	case "get_incident_context":
		return "read the incident context a top result is tied to"
	case "explain_supply_chain_impact":
		return "explain the supply-chain impact for a top result's package"
	default:
		return "advance the investigation from a top result"
	}
}

// handleSignalTool maps a graph handle kind to the first-class read tool a
// follow-up call would use, or "" when no bounded next call applies.
func handleSignalTool(kind string) string {
	signal, _ := searchrerank.HandleSignal(kind)
	switch signal {
	case searchrerank.SignalService:
		return "get_service_story"
	case searchrerank.SignalWorkload, searchrerank.SignalDeployment:
		return "trace_deployment_chain"
	case searchrerank.SignalIncident:
		return "get_incident_context"
	case searchrerank.SignalPackage:
		return "explain_supply_chain_impact"
	default:
		return ""
	}
}
