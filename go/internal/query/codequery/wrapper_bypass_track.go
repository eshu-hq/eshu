// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// errWrapperFindingNotFound reports a wrapper-bypass point lookup with no
// qualified target: the nominating shape, the target's caller set, or the
// selection said no. Callers map it to 404.
var errWrapperFindingNotFound = errors.New("wrapper-bypass finding not found")

// wrapperGraphEvidence is one family's batched graph read: (wrapper,
// target) delegation pairs, caller rows per target, fan-in per candidate,
// and outgoing callee ids per wrapper. Three round trips bound the family
// no matter how many wrappers it holds.
type wrapperGraphEvidence struct {
	pairs    []wrapperDelegationPair
	byTarget map[string][]WrapperCallerRow
	names    map[string]string
	fanIn    map[string]int
	callees  map[string][]string
}

type wrapperDelegationPair struct {
	wrapper string
	target  string
}

// collectWrapperGraphEvidence runs the three batched one-hop reads for one
// nominated family: outgoing callees per wrapper (the delegation pairs),
// callers per distinct target, and fan-in per candidate caller. All three
// stay anchored on indexed entity ids with repo/grant in the anchoring
// WHERE on both backends.
func (h *CodeHandler) collectWrapperGraphEvidence(
	ctx context.Context,
	repoID string,
	wrapperIDs []string,
) (*wrapperGraphEvidence, error) {
	backend := h.graphBackend()
	access := codeGrantAccessFilter(ctx)
	evidence := &wrapperGraphEvidence{
		byTarget: map[string][]WrapperCallerRow{},
		names:    map[string]string{},
		fanIn:    map[string]int{},
		callees:  map[string][]string{},
	}
	calleesCypher, calleesParams := BuildWrapperFamilyCalleesCypher(wrapperIDs, repoID, backend, access)
	calleeRows, err := h.runWrapperGraphRows(ctx, calleesCypher, calleesParams)
	if err != nil {
		return nil, err
	}
	targetSet := map[string]struct{}{}
	for _, row := range calleeRows {
		source, target := StringVal(row, "source_id"), StringVal(row, "id")
		if source == "" || target == "" {
			continue
		}
		evidence.pairs = append(evidence.pairs, wrapperDelegationPair{wrapper: source, target: target})
		evidence.callees[source] = append(evidence.callees[source], target)
		targetSet[target] = struct{}{}
	}
	targetIDs := make([]string, 0, len(targetSet))
	for target := range targetSet {
		targetIDs = append(targetIDs, target)
	}
	sort.Strings(targetIDs)
	if len(targetIDs) == 0 {
		return evidence, nil
	}
	callersCypher, callersParams := BuildWrapperFamilyCallersCypher(targetIDs, repoID, backend, access)
	callerRows, err := h.runWrapperGraphRows(ctx, callersCypher, callersParams)
	if err != nil {
		return nil, err
	}
	candidateSet := map[string]struct{}{}
	for _, row := range callerRows {
		target := StringVal(row, "target_id")
		if target == "" {
			continue
		}
		caller := scanWrapperCallerRow(row)
		if caller.EntityID == "" {
			continue
		}
		evidence.byTarget[target] = append(evidence.byTarget[target], caller)
		if name := StringVal(row, "target_name"); name != "" {
			evidence.names[target] = name
		}
		candidateSet[caller.EntityID] = struct{}{}
	}
	candidateIDs := make([]string, 0, len(candidateSet))
	for id := range candidateSet {
		candidateIDs = append(candidateIDs, id)
	}
	sort.Strings(candidateIDs)
	fanInCypher, fanInParams := BuildWrapperFamilyFanInCypher(candidateIDs, repoID, backend, access)
	fanInRows, err := h.runWrapperGraphRows(ctx, fanInCypher, fanInParams)
	if err != nil {
		return nil, err
	}
	for _, row := range fanInRows {
		if id := StringVal(row, "id"); id != "" {
			evidence.fanIn[id] = IntVal(row, "fan_in")
		}
	}
	return evidence, nil
}

// winnerStatsFor closes over batched evidence into the thinness inputs
// Slice A reads for the fan-in winner: complexity rides the caller row,
// distinct-callee and target-call counts derive from the winner's batched
// outgoing ids.
func (evidence *wrapperGraphEvidence) winnerStatsFor(targetID string) func(WrapperCallerRow) (WrapperCandidateStats, error) {
	return func(winner WrapperCallerRow) (WrapperCandidateStats, error) {
		seen := map[string]struct{}{}
		targetCalls := 0
		for _, callee := range evidence.callees[winner.EntityID] {
			seen[callee] = struct{}{}
			if callee == targetID {
				targetCalls++
			}
		}
		return WrapperCandidateStats{
			Complexity:  winner.Complexity,
			CalleeCount: len(seen),
			TargetCalls: targetCalls,
		}, nil
	}
}

// assembleWrapperTrack qualifies every distinct target behind one nominated
// wrapper family and assembles the positive ones. Graph evidence is shared
// across the family's targets; content details resolve once per member id.
// A negative selection drops with counted suppressions, never silently.
func (h *CodeHandler) assembleWrapperTrack(
	ctx context.Context,
	repoID string,
	survivors []codedivergence.Member,
	includeTests bool,
) ([]codedivergence.Finding, map[string]int, error) {
	suppressions := map[string]int{}
	wrapperIDs := make([]string, 0, len(survivors))
	for _, member := range survivors {
		wrapperIDs = append(wrapperIDs, member.EntityID)
	}
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		return nil, nil, errDivergenceFindingsUnavailable
	}
	evidence, err := h.collectWrapperGraphEvidence(ctx, repoID, wrapperIDs)
	if err != nil {
		return nil, nil, err
	}
	targets := map[string]struct{}{}
	for _, pair := range evidence.pairs {
		targets[pair.target] = struct{}{}
	}
	sorted := make([]string, 0, len(targets))
	for target := range targets {
		sorted = append(sorted, target)
	}
	sort.Strings(sorted)
	findings := []codedivergence.Finding{}
	for _, target := range sorted {
		direct := evidence.byTarget[target]
		if len(direct) == 0 {
			suppressions[codedivergence.RuleWrapperUnqualified]++
			continue
		}
		canonical, bypassers, sel, err := qualifyWrapperTarget(
			target, evidence.names[target], direct, evidence.fanIn,
			evidence.winnerStatsFor(target),
		)
		if err != nil {
			return nil, nil, err
		}
		if !sel.Qualified {
			suppressions[codedivergence.RuleWrapperUnqualified] += len(direct)
			continue
		}
		memberIDs := make([]string, 0, len(bypassers)+1)
		memberIDs = append(memberIDs, canonical.EntityID)
		for _, bypasser := range bypassers {
			memberIDs = append(memberIDs, bypasser.EntityID)
		}
		byID, err := reader.DivergenceMembersByEntityID(ctx, repoID, memberIDs)
		if err != nil {
			return nil, nil, err
		}
		members := make([]codedivergence.Member, 0, len(memberIDs))
		for _, id := range memberIDs {
			if member, ok := byID[id]; ok {
				members = append(members, member)
			}
		}
		finding, ok := codedivergence.AssembleWrapperBypassFinding(repoID, target, members, includeTests, sel)
		for rule, count := range finding.Suppressions {
			suppressions[rule] += count
		}
		if !ok {
			continue
		}
		findings = append(findings, finding)
	}
	return findings, suppressions, nil
}

// investigateWrapperTarget is the point lookup behind investigate
// kind=wrapper_bypass, where the fingerprint carries the target entity id.
// It runs the same qualification the findings track runs: callers, batched
// fan-in, winner callees, content members, assemble. Anything unqualified
// is a 404, never an ad-hoc shape.
func (h *CodeHandler) investigateWrapperTarget(
	ctx context.Context,
	repoID, targetID string,
	includeTests bool,
) (codedivergence.Finding, error) {
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		return codedivergence.Finding{}, errDivergenceFindingsUnavailable
	}
	backend := h.graphBackend()
	access := codeGrantAccessFilter(ctx)
	// The family callers read serves the point lookup too: same columns,
	// same anchoring, plus the target name for the reason sentence.
	callersCypher, callersParams := BuildWrapperFamilyCallersCypher([]string{targetID}, repoID, backend, access)
	callerRows, err := h.runWrapperGraphRows(ctx, callersCypher, callersParams)
	if err != nil {
		return codedivergence.Finding{}, err
	}
	direct := make([]WrapperCallerRow, 0, len(callerRows))
	candidateIDs := make([]string, 0, len(callerRows))
	targetName := ""
	for _, row := range callerRows {
		caller := scanWrapperCallerRow(row)
		if caller.EntityID == "" {
			continue
		}
		direct = append(direct, caller)
		candidateIDs = append(candidateIDs, caller.EntityID)
		if name := StringVal(row, "target_name"); name != "" {
			targetName = name
		}
	}
	if len(direct) == 0 {
		return codedivergence.Finding{}, errWrapperFindingNotFound
	}
	fanInCypher, fanInParams := BuildWrapperFamilyFanInCypher(candidateIDs, repoID, backend, access)
	fanInRows, err := h.runWrapperGraphRows(ctx, fanInCypher, fanInParams)
	if err != nil {
		return codedivergence.Finding{}, err
	}
	fanIn := make(map[string]int, len(fanInRows))
	for _, row := range fanInRows {
		if id := StringVal(row, "id"); id != "" {
			fanIn[id] = IntVal(row, "fan_in")
		}
	}
	calleesFor := func(winner WrapperCallerRow) (WrapperCandidateStats, error) {
		calleesCypher, calleesParams := BuildWrapperCalleesCypher(winner.EntityID, targetID, repoID, backend, access)
		rows, err := h.runWrapperGraphRows(ctx, calleesCypher, calleesParams)
		if err != nil {
			return WrapperCandidateStats{}, err
		}
		seen := map[string]struct{}{}
		targetCalls := 0
		for _, row := range rows {
			id := StringVal(row, "id")
			if id == "" {
				continue
			}
			seen[id] = struct{}{}
			if id == targetID {
				targetCalls++
			}
		}
		return WrapperCandidateStats{Complexity: winner.Complexity, CalleeCount: len(seen), TargetCalls: targetCalls}, nil
	}
	canonical, bypassers, sel, err := qualifyWrapperTarget(targetID, targetName, direct, fanIn, calleesFor)
	if err != nil {
		return codedivergence.Finding{}, err
	}
	if !sel.Qualified {
		return codedivergence.Finding{}, errWrapperFindingNotFound
	}
	memberIDs := make([]string, 0, len(bypassers)+1)
	memberIDs = append(memberIDs, canonical.EntityID)
	for _, bypasser := range bypassers {
		memberIDs = append(memberIDs, bypasser.EntityID)
	}
	byID, err := reader.DivergenceMembersByEntityID(ctx, repoID, memberIDs)
	if err != nil {
		return codedivergence.Finding{}, err
	}
	members := make([]codedivergence.Member, 0, len(memberIDs))
	for _, id := range memberIDs {
		if member, ok := byID[id]; ok {
			members = append(members, member)
		}
	}
	finding, ok := codedivergence.AssembleWrapperBypassFinding(repoID, targetID, members, includeTests, sel)
	if !ok {
		return codedivergence.Finding{}, errWrapperFindingNotFound
	}
	return finding, nil
}

// wrapperGraphUnavailable reports whether err is the degraded-backend
// signal the findings track absorbs into counted suppressions rather than
// failing the page.
func wrapperGraphUnavailable(err error) bool {
	return errors.Is(err, querycontract.ErrGraphUnavailable)
}
