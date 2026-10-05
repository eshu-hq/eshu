// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

// Every state the coverage statements can return has a reason and a next step,
// so a new state cannot ship without text an admin can act on (#7594).
func TestCoverageGapTextCoversEveryKnownState(t *testing.T) {
	t.Parallel()

	for _, named := range []bool{false, true} {
		for _, state := range code.CrossRepoDeadCodeCoverageStates {
			reason, nextStep := coverageGapText(state, named)
			if reason == "" || nextStep == "" {
				t.Errorf("state %q named=%v: reason = %q, next_step = %q, want both set", state, named, reason, nextStep)
			}
		}
	}
}

// The wording per state is the owner's decision on #7594. Truncated must say
// what Eshu does and does not know: the watermark stores only a boolean, so it
// cannot name which cause applies. A request that named its consumers is not
// told to name them again.
func TestCoverageGapTextWording(t *testing.T) {
	t.Parallel()

	const clears = "Wait and ask again. This should clear by itself."
	const truncatedReason = "The snapshot is current but Eshu cannot prove it is complete: no entry points (roots) were found for this repository, or the walk hit its depth or size limit."
	for name, tc := range map[string]struct {
		state, wantReason, wantNextStep string
		named                           bool
	}{
		"no snapshot yet": {
			state:        code.CrossRepoDeadCodeCoverageStateNoSnapshotYet,
			wantReason:   "Eshu has not finished building the call-graph snapshot for this repository (queued or running).",
			wantNextStep: clears,
		},
		"no snapshot yet, named": {
			state:        code.CrossRepoDeadCodeCoverageStateNoSnapshotYet,
			named:        true,
			wantReason:   "Eshu has not finished building the call-graph snapshot for this repository (queued or running).",
			wantNextStep: clears,
		},
		"older epoch": {
			state:        code.CrossRepoDeadCodeCoverageStateOlderEpoch,
			wantReason:   "The snapshot was built by an older version of the analysis and is being rebuilt.",
			wantNextStep: clears,
		},
		"older epoch, named": {
			state:        code.CrossRepoDeadCodeCoverageStateOlderEpoch,
			named:        true,
			wantReason:   "The snapshot was built by an older version of the analysis and is being rebuilt.",
			wantNextStep: clears,
		},
		"truncated": {
			state:        code.CrossRepoDeadCodeCoverageStateTruncated,
			wantReason:   truncatedReason,
			wantNextStep: "Waiting will not clear this. Name the repositories you care about with `consumer_repo_ids`, or check whether this repository's framework entry points are modeled.",
		},
		"truncated, named": {
			state:        code.CrossRepoDeadCodeCoverageStateTruncated,
			named:        true,
			wantReason:   truncatedReason,
			wantNextStep: "Waiting will not clear this. Check whether this repository's framework entry points are modeled.",
		},
		"no active scope": {
			state:        code.CrossRepoDeadCodeCoverageStateNoActiveScope,
			wantReason:   "This repository id is not an indexed repository.",
			wantNextStep: "Check the id, or index the repository.",
		},
		"no active scope, named": {
			state:        code.CrossRepoDeadCodeCoverageStateNoActiveScope,
			named:        true,
			wantReason:   "This repository id is not an indexed repository.",
			wantNextStep: "Check the id, or index the repository.",
		},
	} {
		reason, nextStep := coverageGapText(tc.state, tc.named)
		if reason != tc.wantReason || nextStep != tc.wantNextStep {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", name, reason, nextStep, tc.wantReason, tc.wantNextStep)
		}
	}
	if reason, nextStep := coverageGapText("made_up", false); reason != "" || nextStep != "" {
		t.Errorf("unknown state: got (%q, %q), want empty", reason, nextStep)
	}
}

func TestCoverageSummary(t *testing.T) {
	t.Parallel()

	wait := code.CrossRepoDeadCodeCoverageGap{State: code.CrossRepoDeadCodeCoverageStateOlderEpoch, Retryable: true}
	stuck := code.CrossRepoDeadCodeCoverageGap{State: code.CrossRepoDeadCodeCoverageStateTruncated}
	unindexed := code.CrossRepoDeadCodeCoverageGap{State: code.CrossRepoDeadCodeCoverageStateNoActiveScope}
	repeat := func(g code.CrossRepoDeadCodeCoverageGap, n int) []code.CrossRepoDeadCodeCoverageGap {
		out := make([]code.CrossRepoDeadCodeCoverageGap, n)
		for i := range out {
			out[i] = g
		}
		return out
	}

	for name, tc := range map[string]struct {
		coverage code.CrossRepoDeadCodeCoverage
		named    bool
		want     string
	}{
		"complete": {
			coverage: code.CrossRepoDeadCodeCoverage{},
			want:     "No repository checked has a coverage gap.",
		},
		"one that clears": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(wait, 1)},
			want:     "1 repository cannot be judged yet: 1 of them should clear on their own, 0 will not. Wait and ask again.",
		},
		"one that will not": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(stuck, 1)},
			want:     "1 repository cannot be judged yet: 0 of them should clear on their own, 1 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		"mixed": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 9)...)},
			want:     "12 repositories cannot be judged yet: 3 of them should clear on their own, 9 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		"capped": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 22)...), IncompleteTruncated: true},
			want:     "At least 25 repositories cannot be judged yet (the list was cut): 3 of them should clear on their own, 22 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		// The statement trims duplicate-hidden repositories after it sets the
		// cut flag, so a cut list can hold fewer than the cap. The text must not
		// quote a cap it cannot know.
		"cut with fewer than the cap": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 21)...), IncompleteTruncated: true},
			want:     "At least 24 repositories cannot be judged yet (the list was cut): 3 of them should clear on their own, 21 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		// The caller already named these ids, so naming them again is no advice.
		"every gap is an id that is not indexed": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(unindexed, 2)},
			want:     "2 repositories cannot be judged yet: 0 of them should clear on their own, 2 will not. Check the ids, or index the repositories.",
		},
		// A list that mixes unindexed ids with real gaps still needs the advice to
		// name the repositories, because the other gaps are repositories.
		"some ids not indexed and some other gaps": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(unindexed, 1), repeat(stuck, 2)...)},
			want:     "3 repositories cannot be judged yet: 0 of them should clear on their own, 3 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		// The request named its consumers, so the gaps are the ones it named and
		// telling it to name them again is circular.
		"named, one that will not": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(stuck, 1)},
			named:    true,
			want:     "1 repository cannot be judged yet: 0 of them should clear on their own, 1 will not. Check whether their framework entry points are modeled.",
		},
		"named, mixed": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 9)...)},
			named:    true,
			want:     "12 repositories cannot be judged yet: 3 of them should clear on their own, 9 will not. Check whether their framework entry points are modeled.",
		},
		"named, capped": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 22)...), IncompleteTruncated: true},
			named:    true,
			want:     "At least 25 repositories cannot be judged yet (the list was cut): 3 of them should clear on their own, 22 will not. Check whether their framework entry points are modeled.",
		},
		"named, some ids not indexed and some other gaps": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(unindexed, 1), repeat(stuck, 2)...)},
			named:    true,
			want:     "3 repositories cannot be judged yet: 0 of them should clear on their own, 3 will not. Check whether their framework entry points are modeled.",
		},
		"named, every gap is an id that is not indexed": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(unindexed, 2)},
			named:    true,
			want:     "2 repositories cannot be judged yet: 0 of them should clear on their own, 2 will not. Check the ids, or index the repositories.",
		},
		"named, one that clears": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(wait, 1)},
			named:    true,
			want:     "1 repository cannot be judged yet: 1 of them should clear on their own, 0 will not. Wait and ask again.",
		},
		"capped and all clearing": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(wait, 25), IncompleteTruncated: true},
			want:     "At least 25 repositories cannot be judged yet (the list was cut): 25 of them should clear on their own, 0 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := coverageSummary(tc.coverage, tc.named); got != tc.want {
				t.Fatalf("coverageSummary = %q, want %q", got, tc.want)
			}
		})
	}
}
