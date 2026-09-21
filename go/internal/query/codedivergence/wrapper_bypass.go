// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"fmt"
)

// KindWrapperBypass names findings where one thin wrapper is the canonical
// access point for a target while other packages call the target directly.
// Members are the wrapper plus its bypassers (the target's direct callers
// outside the wrapper's package); the Fingerprint carries the target entity
// id, like drifted carries the writer finding id. Candidates are nominated
// by wrapper-family exact groups (the per-service wrapper class from #6834
// §4), then qualified one target at a time over one-hop graph rows.
const KindWrapperBypass Kind = "parallel_implementation.wrapper_bypass"

// Wrapper-bypass reason codes. The canonical and bypass-copy reasons are
// value-bearing and decompose members × tokens without remainder; the
// ambiguity reason is a zero-weight signal.
const (
	ReasonWrapperCanonical   = "wrapper_canonical_selected"
	ReasonWrapperBypassCopy  = "additional_bypass_copy"
	ReasonCanonicalAmbiguous = "canonical_ambiguous"
)

// Wrapper-bypass suppression rules, reported per rule in every response.
const (
	RuleNotWrapperFamily        = "not_wrapper_family"
	RuleWrapperUnqualified      = "wrapper_not_qualified"
	RuleWrapperGraphUnavailable = "wrapper_graph_unavailable"
	// RuleWrapperGraphTimeout counts a wrapper-family track the bounded
	// graph-read budget cut short. Only the divergence report uses it: the
	// rollup degrades one slow family to a counted timeout instead of
	// failing the other four kinds with it. The findings page keeps the
	// loud deadline so a slow track stays visible as a defect.
	RuleWrapperGraphTimeout = "wrapper_graph_timeout"
)

// BypassSelection is the graph verdict for one target: the canonical
// wrapper, the weakest contributing CALLS-edge confidence behind it, and
// whether any contributing edge is inferred (admitted but flagged, never
// silent). Qualified false is the negative outcome: no canonical wrapper,
// so no finding.
type BypassSelection struct {
	CanonicalID   string
	CanonicalName string
	TargetID      string
	TargetName    string
	Confidence    float64
	Qualified     bool
	Ambiguous     bool
	AmbiguityNote string
}

// WrapperFamilySurvivors applies the member-level suppression catalogue
// minus the token floor — thinness is definitional for wrappers, so the
// floor must not eat the family — and reports whether the survivors form a
// wrapper family: at least WrapperFamilyMinMembers sharing one entity name.
func WrapperFamilySurvivors(members []Member, includeTests bool) ([]Member, map[string]int, bool) {
	suppressions := map[string]int{}
	survivors := make([]Member, 0, len(members))
	for _, member := range members {
		if SuppressGenerated(member) {
			suppressions[RuleGenerated]++
			continue
		}
		if SuppressVendored(member) {
			suppressions[RuleVendored]++
			continue
		}
		if SuppressTestFile(member, includeTests) {
			suppressions[RuleTestFile]++
			continue
		}
		if SuppressTrivialAccessor(member) {
			suppressions[RuleTrivialAccessor]++
			continue
		}
		survivors = append(survivors, member)
	}
	return survivors, suppressions, SuppressWrapperFamily(survivors)
}

// AssembleWrapperBypassFinding builds the finding for one qualified target.
// Members arrive as the wrapper plus its bypassers with content details
// already resolved; the selection carries the graph verdict. Member-level
// suppression applies the shared catalogue minus the token floor (thinness
// is definitional for wrappers), so test-file and trivial-accessor members
// still suppress here. It reports false when fewer than two members survive
// or the selection is negative, counted under wrapper_not_qualified. The
// wrapper-family shape gate is the caller's job (WrapperFamilySurvivors on
// the nominating exact group): members mix the wrapper and bypasser names
// by construction, so a same-name check here would always fail. An ambiguous
// (inferred-edge) selection still assembles, carrying the zero-weight
// canonical_ambiguous signal with the evidence note.
func AssembleWrapperBypassFinding(repoID, targetID string, members []Member, includeTests bool, sel BypassSelection) (Finding, bool) {
	survivors, suppressions := suppressWrapperBypassMembers(members, includeTests)
	partial := Finding{
		ID:           findingID(repoID, KindWrapperBypass, targetID),
		RepoID:       repoID,
		Kind:         KindWrapperBypass,
		Fingerprint:  targetID,
		Suppressions: suppressions,
	}
	if len(survivors) < 2 {
		partial.Suppressions[RuleWrapperUnqualified] += len(survivors)
		return partial, false
	}
	if !sel.Qualified {
		partial.Suppressions[RuleWrapperUnqualified] += len(survivors)
		return partial, false
	}
	tokenCount := 0
	for _, member := range survivors {
		if member.TokenCount > tokenCount {
			tokenCount = member.TokenCount
		}
	}
	ordered := orderWrapperMembers(survivors, sel.CanonicalID)
	return Finding{
		ID:           partial.ID,
		RepoID:       repoID,
		Kind:         KindWrapperBypass,
		Fingerprint:  targetID,
		Members:      ordered,
		Reasons:      buildWrapperBypassReasons(sel, len(ordered), tokenCount, ordered),
		Score:        len(ordered) * tokenCount,
		Confidence:   sel.Confidence,
		Suppressions: partial.Suppressions,
	}, true
}

// suppressWrapperBypassMembers applies the member-level suppression
// catalogue minus the token floor: wrapper and bypasser bodies are small by
// nature, so the floor must not eat the finding. Generated, vendored,
// test-file (unless opted back in), and trivial-accessor members still
// suppress with per-rule counts.
func suppressWrapperBypassMembers(members []Member, includeTests bool) ([]Member, map[string]int) {
	suppressions := map[string]int{}
	survivors := make([]Member, 0, len(members))
	for _, member := range members {
		if SuppressGenerated(member) {
			suppressions[RuleGenerated]++
			continue
		}
		if SuppressVendored(member) {
			suppressions[RuleVendored]++
			continue
		}
		if SuppressTestFile(member, includeTests) {
			suppressions[RuleTestFile]++
			continue
		}
		if SuppressTrivialAccessor(member) {
			suppressions[RuleTrivialAccessor]++
			continue
		}
		survivors = append(survivors, member)
	}
	return survivors, suppressions
}

// orderWrapperMembers puts the canonical wrapper first so the finding reads
// wrapper-then-bypassers deterministically; unknown canonical ids keep the
// input order rather than dropping a member.
func orderWrapperMembers(members []Member, canonicalID string) []Member {
	ordered := make([]Member, 0, len(members))
	for _, member := range members {
		if member.EntityID == canonicalID {
			ordered = append(ordered, member)
		}
	}
	for _, member := range members {
		if member.EntityID != canonicalID {
			ordered = append(ordered, member)
		}
	}
	if len(ordered) != len(members) {
		return members
	}
	return ordered
}

// buildWrapperBypassReasons decomposes members × tokens without remainder:
// one canonical reason naming the wrapper, its target, the bypass count,
// and the weakest-edge confidence, plus one bypass-copy reason per extra
// member. Package span, large bodies, and inferred-edge ambiguity ride as
// zero-weight signals.
func buildWrapperBypassReasons(sel BypassSelection, memberCount, tokenCount int, members []Member) []Reason {
	reasons := []Reason{{
		Code: ReasonWrapperCanonical,
		Sentence: fmt.Sprintf(
			"canonical wrapper %s (%s) fronts target %s (%s) at weakest-edge confidence %.2f with %d cross-package bypassers (%d members, max %d tokens)",
			sel.CanonicalName, sel.CanonicalID, sel.TargetName, sel.TargetID,
			sel.Confidence, memberCount-1, memberCount, tokenCount,
		),
		Value: tokenCount,
	}}
	for i := 1; i < memberCount; i++ {
		reasons = append(reasons, Reason{
			Code:     ReasonWrapperBypassCopy,
			Sentence: fmt.Sprintf("additional direct caller %d of %d bypassing the canonical wrapper (%d tokens)", i+1, memberCount, tokenCount),
			Value:    tokenCount,
		})
	}
	if sel.Ambiguous {
		reasons = append(reasons, Reason{
			Code:     ReasonCanonicalAmbiguous,
			Sentence: fmt.Sprintf("canonical evidence is ambiguous: %s (judgment signal, no score weight)", sel.AmbiguityNote),
			Value:    0,
		})
	}
	packages := map[string]struct{}{}
	for _, member := range members {
		packages[PackageOf(member.RelativePath)] = struct{}{}
	}
	if len(packages) > 1 {
		reasons = append(reasons, Reason{
			Code:     ReasonSpanPackages,
			Sentence: fmt.Sprintf("members span %d packages (ranking signal, no score weight)", len(packages)),
			Value:    0,
		})
	}
	if tokenCount >= LargeBodyTokens {
		reasons = append(reasons, Reason{
			Code:     ReasonLargeBody,
			Sentence: fmt.Sprintf("body holds %d tokens at or above the %d-token large-body mark (judgment signal, no score weight)", tokenCount, LargeBodyTokens),
			Value:    0,
		})
	}
	return reasons
}
