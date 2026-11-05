// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"cmp"
	"fmt"
	"slices"
)

// suppressionWalkMaxPairs is the most adjacent generation pairs one pass diffs
// one by one; past it the pass folds the older remainder into one endpoint diff.
// The value is provisional. The shim cost curve (one statement per pair, linear
// in the pair count) ran on a loaded host, so it could not pick the largest count
// whose median stays under 500 ms. The wiring PR fixes it from the quiet-host
// re-measure at the cap; see docs/internal/design/10019-suppression-fanout.md.
const suppressionWalkMaxPairs = 4

// suppressionWalkStep is one diff statement of a walk: the suppressions whose
// payload differs between two generations of the scope. Adjacent steps compare
// neighbouring activated generations. At most one step per plan is an endpoint
// step, which spans several generations.
type suppressionWalkStep struct {
	PriorGenerationID   string
	CurrentGenerationID string
}

// suppressionWalkPlan is the ordered statements a pass runs, oldest first.
// WalkTruncated counts the activated generations strictly inside the endpoint
// step, whose own changes the walk could not separate. It is zero when every
// adjacent pair is walked, and it never fails the pass: a failing pass would
// leave the baseline where it is, so each later pass would face a longer walk.
type suppressionWalkPlan struct {
	Steps         []suppressionWalkStep
	WalkTruncated int
}

// suppressionWalkResult is what a walk hands the writer. CVEIDs is the sorted
// union of every step's changed CVE ids. DiffRows counts rows read across all
// steps and Statements counts the steps run. NoCVEScopeRows counts distinct
// changed suppressions that named no CVE in any step, since the fan-out cannot
// reach their holders by id. WalkTruncated repeats the plan's count so the span,
// the log line and the counter read it from one place.
type suppressionWalkResult struct {
	CVEIDs         []string
	DiffRows       int
	NoCVEScopeRows int
	Statements     int
	WalkTruncated  int
}

// planSuppressionWalk lists the diff statements a pass on currentID runs. A
// single endpoint diff loses a change that cancels out inside the span: X added
// in B and removed in C leaves nothing between A and C, yet a holder that ran
// during B read X as suppressed. The set is therefore the union of the diff of
// every adjacent pair of activated generations from the baseline to the current
// one, in lifecycle order (ingested_at, generation_id). A never-activated or
// failed generation is not in the chain: the first was seen by no reader, and the
// second is excluded by rule even if it was once active (see the design's Known
// limits for the change that cancels out through it).
//
// At most maxPairs pairs are walked one by one, newest first. Past that, one
// endpoint step spans the older remainder, so a pass issues at most maxPairs+1
// statements. A remainder of a single pair is already an adjacent pair and loses
// nothing. A maxPairs below one is treated as one. With no baseline the plan is
// empty and the caller reads the current generation whole.
func planSuppressionWalk(currentID string, baseline suppressionDiffBaseline, candidates []suppressionGeneration, maxPairs int) suppressionWalkPlan {
	baseIdx := slices.IndexFunc(candidates, func(g suppressionGeneration) bool { return g.GenerationID == baseline.GenerationID })
	curIdx := slices.IndexFunc(candidates, func(g suppressionGeneration) bool { return g.GenerationID == currentID })
	if baseline.GenerationID == "" || baseIdx < 0 || curIdx < 0 {
		return suppressionWalkPlan{}
	}
	base, current := candidates[baseIdx], candidates[curIdx]

	var chain []suppressionGeneration
	for _, g := range candidates {
		inRange := g.GenerationID == base.GenerationID || g.GenerationID == current.GenerationID ||
			(suppressionGenerationNewer(g, base) && suppressionGenerationNewer(current, g))
		if inRange && suppressionGenerationReadable(g) {
			chain = append(chain, g)
		}
	}
	slices.SortFunc(chain, func(a, b suppressionGeneration) int {
		switch {
		case suppressionGenerationNewer(a, b):
			return 1
		case suppressionGenerationNewer(b, a):
			return -1
		}
		return cmp.Compare(a.GenerationID, b.GenerationID)
	})
	pairs := len(chain) - 1
	if pairs < 1 {
		return suppressionWalkPlan{}
	}
	maxPairs = max(maxPairs, 1)

	// split is the chain index where the pairwise walk starts. Everything before
	// it is one endpoint step, unless that would be a single adjacent pair.
	split := 0
	if pairs > maxPairs+1 {
		split = pairs - maxPairs
	}
	var plan suppressionWalkPlan
	if split > 0 {
		plan.Steps = append(plan.Steps, suppressionWalkStep{
			PriorGenerationID:   chain[0].GenerationID,
			CurrentGenerationID: chain[split].GenerationID,
		})
		plan.WalkTruncated = split - 1
	}
	for i := split; i < pairs; i++ {
		plan.Steps = append(plan.Steps, suppressionWalkStep{
			PriorGenerationID:   chain[i].GenerationID,
			CurrentGenerationID: chain[i+1].GenerationID,
		})
	}
	return plan
}

// walkSuppressionDiff runs the plan through read and unions the steps. read is
// the diff statement for one pair of generations. A read error fails the whole
// walk and returns no partial set: the pass retries as a unit, and a partial set
// would reopen fewer holders than the pass owes.
func walkSuppressionDiff(plan suppressionWalkPlan, read func(priorID, currentID string) ([]suppressionDiffRow, error)) (suppressionWalkResult, error) {
	var all []suppressionDiffRow
	result := suppressionWalkResult{WalkTruncated: plan.WalkTruncated}
	for _, step := range plan.Steps {
		rows, err := read(step.PriorGenerationID, step.CurrentGenerationID)
		if err != nil {
			return suppressionWalkResult{}, fmt.Errorf("suppression diff %s to %s: %w", step.PriorGenerationID, step.CurrentGenerationID, err)
		}
		result.Statements++
		result.DiffRows += len(rows)
		all = append(all, rows...)
	}
	result.CVEIDs = changedSuppressionCVEIDs(all)
	result.NoCVEScopeRows = countDistinctSuppressionsWithoutCVEScope(all)
	return result, nil
}

// countDistinctSuppressionsWithoutCVEScope counts the suppressions that never
// named a CVE in any row of the walk. A suppression that named one in another
// step reached its holders through that id and is not counted.
func countDistinctSuppressionsWithoutCVEScope(rows []suppressionDiffRow) int {
	scoped := map[string]struct{}{}
	unscoped := map[string]struct{}{}
	for _, row := range rows {
		if countSuppressionsWithoutCVEScope([]suppressionDiffRow{row}) == 1 {
			unscoped[row.SuppressionID] = struct{}{}
		} else {
			scoped[row.SuppressionID] = struct{}{}
		}
	}
	count := 0
	for id := range unscoped {
		if _, ok := scoped[id]; !ok {
			count++
		}
	}
	return count
}
