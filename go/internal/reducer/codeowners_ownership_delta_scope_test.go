// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/codeowners"
)

// TestCodeownersOwnershipCandidatePathsMatchCollector locks the reducer's
// hand-duplicated codeownersOwnershipCandidatePaths to the collector's
// canonical codeowners.CandidatePaths(). The two lists are intentionally NOT
// wired by a production import — the collector/reducer package ownership
// boundary forbids the reducer importing internal/collector/codeowners — so
// this test is the CI gate that keeps them in lockstep. Adding, removing, or
// reordering a CODEOWNERS location in one copy without the other would silently
// break the reducer's whole-repo re-projection trigger: the reducer would fail
// to force a whole-repository retract for a location the collector now emits
// facts for, or vice versa (issue #5419). Order is significant because it is
// GitHub's location precedence, so the comparison is exact including order.
func TestCodeownersOwnershipCandidatePathsMatchCollector(t *testing.T) {
	t.Parallel()

	got := codeownersOwnershipCandidatePaths
	want := codeowners.CandidatePaths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"reducer codeownersOwnershipCandidatePaths = %v, want collector codeowners.CandidatePaths() = %v — "+
				"the two hand-maintained CODEOWNERS location lists have drifted; update both in lockstep",
			got, want,
		)
	}
}
