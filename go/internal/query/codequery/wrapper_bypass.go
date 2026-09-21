// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// WrapperCallerRow is one direct caller of a wrapper-bypass target, as the
// one-hop callers builders return it: caller identity, the caller file's
// package (its directory), the resolving CALLS edge's provenance, and the
// caller's cyclomatic complexity (the thinness input, zero when unmapped).
type WrapperCallerRow struct {
	EntityID       string
	Name           string
	Package        string
	EdgeMethod     string
	EdgeConfidence float64
	Complexity     int
}

// WrapperCandidateStats carries the thinness inputs for one direct caller:
// its cyclomatic complexity and its distinct-callee counts.
type WrapperCandidateStats struct {
	Complexity  int
	CalleeCount int
	TargetCalls int
}

// WrapperBypassParams tunes wrapper qualification. The defaults are
// starters measured against the theory fixture and Eshu dogfood; the
// read surface exposes them, never silently applies them.
type WrapperBypassParams struct {
	// MinFanIn is the caller-count floor a canonical wrapper must clear.
	MinFanIn int
	// MaxComplexity is the cyclomatic-complexity ceiling for a thin wrapper.
	MaxComplexity int
	// MinTargetShare is the minimum fraction of the wrapper's distinct
	// callees the target must account for (T dominates its callees).
	MinTargetShare float64
	// PeerComparabilityShare suppresses when the runner-up fan-in reaches
	// this share of the winner: two peer wrappers, no canonical one.
	PeerComparabilityShare float64
}

// DefaultWrapperBypassParams returns the shipped qualification floors.
func DefaultWrapperBypassParams() WrapperBypassParams {
	return WrapperBypassParams{
		MinFanIn:               3,
		MaxComplexity:          5,
		MinTargetShare:         0.5,
		PeerComparabilityShare: 0.8,
	}
}

// WrapperBypassInput is everything SelectCanonicalWrapper needs: the
// target, its direct callers, each caller's fan-in, and thinness stats.
type WrapperBypassInput struct {
	TargetID string
	Direct   []WrapperCallerRow
	FanIn    map[string]int
	Stats    map[string]WrapperCandidateStats
	Params   WrapperBypassParams
}

// WrapperBypassVerdict is the qualification outcome: either a suppressed
// finding with a counted reason, or an admission carrying the weakest
// contributing CALLS-edge confidence and whether any contributing edge is
// inferred (surfaced, never silent).
type WrapperBypassVerdict struct {
	Suppressed bool
	Reason     string
	Confidence float64
	Inferred   bool
}

// InferredResolutionMethods is the set of ADR #2222 resolution methods that
// count as inferred evidence for wrapper-bypass findings: everything weaker
// than an explicit import binding. Unspecified legacy edges are not
// inferred, only unclassified. It delegates to codeprovenance.IsInferred,
// the single source of truth both graph-finding families share.
func InferredResolutionMethods() map[string]bool {
	return map[string]bool{
		string(codeprovenance.MethodTypeInferred):           codeprovenance.IsInferred(codeprovenance.MethodTypeInferred),
		string(codeprovenance.MethodScopeUniqueName):        codeprovenance.IsInferred(codeprovenance.MethodScopeUniqueName),
		string(codeprovenance.MethodCrossRepoExportPackage): codeprovenance.IsInferred(codeprovenance.MethodCrossRepoExportPackage),
		string(codeprovenance.MethodRepoUniqueName):         codeprovenance.IsInferred(codeprovenance.MethodRepoUniqueName),
	}
}

// rankWrapperCandidates orders direct callers by fan-in desc, entity id asc:
// the winner order SelectCanonicalWrapper and the read-surface pre-ranking
// share, so the stats fetch targets the same winner the verdict ranks.
func rankWrapperCandidates(direct []WrapperCallerRow, fanIn map[string]int) []WrapperCallerRow {
	ranked := append([]WrapperCallerRow(nil), direct...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if fanIn[ranked[i].EntityID] != fanIn[ranked[j].EntityID] {
			return fanIn[ranked[i].EntityID] > fanIn[ranked[j].EntityID]
		}
		return ranked[i].EntityID < ranked[j].EntityID
	})
	return ranked
}

// SelectCanonicalWrapper qualifies the canonical wrapper for a target from
// its direct callers: the fan-in winner above the floor, unique against its
// runner-up, and thin. Bypassers are the remaining direct callers outside
// the wrapper's package. Confidence is the weakest contributing CALLS edge
// (wrapper-to-target plus each bypasser-to-target edge).
func SelectCanonicalWrapper(in WrapperBypassInput) (WrapperCallerRow, []WrapperCallerRow, WrapperBypassVerdict) {
	suppress := func(reason string) (WrapperCallerRow, []WrapperCallerRow, WrapperBypassVerdict) {
		return WrapperCallerRow{}, nil, WrapperBypassVerdict{Suppressed: true, Reason: reason}
	}
	if len(in.Direct) == 0 {
		return suppress("no_direct_callers")
	}
	ranked := rankWrapperCandidates(in.Direct, in.FanIn)
	winner := ranked[0]
	if in.FanIn[winner.EntityID] < in.Params.MinFanIn {
		return suppress(fmt.Sprintf("no_caller_clears_fan_in_floor_%d", in.Params.MinFanIn))
	}
	if len(ranked) > 1 {
		runnerUp := in.FanIn[ranked[1].EntityID]
		if float64(runnerUp) >= in.Params.PeerComparabilityShare*float64(in.FanIn[winner.EntityID]) {
			return suppress("peer_wrappers_comparable_fan_in")
		}
	}
	stats, ok := in.Stats[winner.EntityID]
	if !ok {
		return suppress("missing_wrapper_stats")
	}
	if stats.Complexity > in.Params.MaxComplexity {
		return suppress(fmt.Sprintf("wrapper_complexity_%d_above_ceiling_%d", stats.Complexity, in.Params.MaxComplexity))
	}
	share := 0.0
	if stats.CalleeCount > 0 {
		share = float64(stats.TargetCalls) / float64(stats.CalleeCount)
	}
	if share < in.Params.MinTargetShare {
		return suppress(fmt.Sprintf("target_share_%.2f_below_floor_%.2f", share, in.Params.MinTargetShare))
	}
	var bypassers []WrapperCallerRow
	for _, caller := range ranked[1:] {
		if caller.Package == winner.Package {
			continue
		}
		bypassers = append(bypassers, caller)
	}
	if len(bypassers) == 0 {
		return suppress("no_cross_package_direct_callers")
	}
	contributing := append([]WrapperCallerRow{winner}, bypassers...)
	confidence := contributing[0].EdgeConfidence
	inferred := InferredResolutionMethods()
	isInferred := false
	for _, row := range contributing {
		if row.EdgeConfidence < confidence {
			confidence = row.EdgeConfidence
		}
		if inferred[row.EdgeMethod] {
			isInferred = true
		}
	}
	verdict := WrapperBypassVerdict{Confidence: confidence, Inferred: isInferred}
	if isInferred {
		verdict.Reason = "wrapper_or_bypass_rests_on_inferred_edge"
	}
	return winner, bypassers, verdict
}
