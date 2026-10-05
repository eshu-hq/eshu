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

	for _, state := range code.CrossRepoDeadCodeCoverageStates {
		reason, nextStep := coverageGapText(state)
		if reason == "" || nextStep == "" {
			t.Errorf("state %q: reason = %q, next_step = %q, want both set", state, reason, nextStep)
		}
	}
}

// The wording per state is the owner's decision on #7594. Truncated must say
// what Eshu does and does not know: the watermark stores only a boolean, so it
// cannot name which cause applies.
func TestCoverageGapTextWording(t *testing.T) {
	t.Parallel()

	const clears = "Wait and ask again. This clears by itself."
	for state, want := range map[string][2]string{
		code.CrossRepoDeadCodeCoverageStateNoSnapshotYet: {
			"Eshu has not finished building the call-graph snapshot for this repository (queued or running).", clears,
		},
		code.CrossRepoDeadCodeCoverageStateOlderEpoch: {
			"The snapshot was built by an older version of the analysis and is being rebuilt.", clears,
		},
		code.CrossRepoDeadCodeCoverageStateTruncated: {
			"The snapshot is current but Eshu cannot prove it is complete: no entry points (roots) were found for this repository, or the walk hit its depth or size limit.",
			"Waiting will not clear this. Name the repositories you care about with `consumer_repo_ids`, or check whether this repository's framework entry points are modeled.",
		},
		code.CrossRepoDeadCodeCoverageStateNoActiveScope: {
			"This repository id is not an indexed repository.", "Check the id, or index the repository.",
		},
	} {
		reason, nextStep := coverageGapText(state)
		if reason != want[0] || nextStep != want[1] {
			t.Errorf("state %q: got (%q, %q), want (%q, %q)", state, reason, nextStep, want[0], want[1])
		}
	}
	if reason, nextStep := coverageGapText("made_up"); reason != "" || nextStep != "" {
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
		want     string
	}{
		"complete": {
			coverage: code.CrossRepoDeadCodeCoverage{},
			want:     "No repository checked has a coverage gap.",
		},
		"one that clears": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(wait, 1)},
			want:     "1 repository cannot be judged yet: 1 of them will clear on their own, 0 will not. Wait and ask again.",
		},
		"one that will not": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(stuck, 1)},
			want:     "1 repository cannot be judged yet: 0 of them will clear on their own, 1 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		"mixed": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 9)...)},
			want:     "12 repositories cannot be judged yet: 3 of them will clear on their own, 9 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		"capped": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 22)...), IncompleteTruncated: true},
			want:     "At least 25 repositories cannot be judged yet (the list was cut): 3 of them will clear on their own, 22 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		// The statement trims duplicate-hidden repositories after it sets the
		// cut flag, so a cut list can hold fewer than the cap. The text must not
		// quote a cap it cannot know.
		"cut with fewer than the cap": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(wait, 3), repeat(stuck, 21)...), IncompleteTruncated: true},
			want:     "At least 24 repositories cannot be judged yet (the list was cut): 3 of them will clear on their own, 21 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		// The caller already named these ids, so naming them again is no advice.
		"every gap is an id that is not indexed": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(unindexed, 2)},
			want:     "2 repositories cannot be judged yet: 0 of them will clear on their own, 2 will not. Check the ids, or index the repositories.",
		},
		// A list that mixes unindexed ids with real gaps still needs the advice to
		// name the repositories, because the other gaps are repositories.
		"some ids not indexed and some other gaps": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: append(repeat(unindexed, 1), repeat(stuck, 2)...)},
			want:     "3 repositories cannot be judged yet: 0 of them will clear on their own, 3 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
		"capped and all clearing": {
			coverage: code.CrossRepoDeadCodeCoverage{Gaps: repeat(wait, 25), IncompleteTruncated: true},
			want:     "At least 25 repositories cannot be judged yet (the list was cut): 25 of them will clear on their own, 0 will not. Name the repositories you care about with `consumer_repo_ids`.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := coverageSummary(tc.coverage); got != tc.want {
				t.Fatalf("coverageSummary = %q, want %q", got, tc.want)
			}
		})
	}
}
