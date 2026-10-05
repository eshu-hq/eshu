// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

const (
	// coverageWaitNextStep is the advice for the two states a pipeline run is
	// expected to clear. It is a hint, as retryable is.
	coverageWaitNextStep = "Wait and ask again. This should clear by itself."

	// coverageNarrowAdvice is the way out of a gap that waiting does not clear
	// when the request did not name its consumers: judge only the repositories
	// the caller cares about.
	coverageNarrowAdvice = "Name the repositories you care about with `consumer_repo_ids`."

	// coverageModeledAdvice replaces it for a request that already named its
	// consumers: the gaps are the repositories it named, so the move left is to
	// check how their entry points are modeled.
	coverageModeledAdvice = "Check whether their framework entry points are modeled."
)

// coverageGapText returns the plain-language reason and next step for one
// coverage gap state (#7594). It is derived from the state and whether the
// request named its own consumers: no new data, no query. An unknown state returns empty strings, and the response then
// leaves both fields out.
//
// The truncated reason states what Eshu knows and what it does not: the
// watermark stores only a boolean, so it cannot say whether the cause was a
// missing root or a depth or size limit.
func coverageGapText(state string, named bool) (reason, nextStep string) {
	switch state {
	case code.CrossRepoDeadCodeCoverageStateNoSnapshotYet:
		return "Eshu has not finished building the call-graph snapshot for this repository (queued or running).",
			coverageWaitNextStep
	case code.CrossRepoDeadCodeCoverageStateOlderEpoch:
		return "The snapshot was built by an older version of the analysis and is being rebuilt.",
			coverageWaitNextStep
	case code.CrossRepoDeadCodeCoverageStateTruncated:
		return "The snapshot is current but Eshu cannot prove it is complete: no entry points (roots) were found for this repository, or the walk hit its depth or size limit.",
			truncatedNextStep(named)
	case code.CrossRepoDeadCodeCoverageStateNoActiveScope:
		return "This repository id is not an indexed repository.",
			"Check the id, or index the repository."
	}
	return "", ""
}

// coverageSummary is the one sentence consumer_coverage.coverage_summary
// carries (#7594). It counts only the listed gaps: the coverage statement stops
// at its cap and never counts the repositories it checked, so a cut list says
// "at least" and the sentence never claims a total or quotes the cap: the
// statement trims duplicate-hidden repositories after it sets the cut flag, so
// a cut list can hold fewer than the cap.
func coverageSummary(coverage code.CrossRepoDeadCodeCoverage, named bool) string {
	if coverage.Complete() {
		return "No repository checked has a coverage gap."
	}
	listed := len(coverage.Gaps)
	clears, unindexed := 0, 0
	for _, gap := range coverage.Gaps {
		if gap.Retryable {
			clears++
		}
		if gap.State == code.CrossRepoDeadCodeCoverageStateNoActiveScope {
			unindexed++
		}
	}
	sentence := strconv.Itoa(listed) + " repositories cannot be judged yet"
	if listed == 1 {
		sentence = "1 repository cannot be judged yet"
	}
	if coverage.IncompleteTruncated {
		sentence = "At least " + sentence + " (the list was cut)"
	}
	sentence += ": " + strconv.Itoa(clears) + " of them should clear on their own, " +
		strconv.Itoa(listed-clears) + " will not. "
	switch {
	case unindexed == listed:
		// The caller named these ids, so naming them again is no advice.
		return sentence + "Check the ids, or index the repositories."
	case clears == listed && !coverage.IncompleteTruncated:
		return sentence + "Wait and ask again."
	}
	if named {
		return sentence + coverageModeledAdvice
	}
	return sentence + coverageNarrowAdvice
}

// truncatedNextStep is the advice for a snapshot that waiting will not fix. A
// request that named its consumers is not told to name them again.
func truncatedNextStep(named bool) string {
	if named {
		return "Waiting will not clear this. Check whether this repository's framework entry points are modeled."
	}
	return "Waiting will not clear this. Name the repositories you care about with `consumer_repo_ids`, or check whether this repository's framework entry points are modeled."
}
