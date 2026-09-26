// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

import "testing"

// TestReplayGenerationSupersededClassWireValue pins the operator-visible
// spelling of the replay-fence class (#7130). It is the eshu_dp_superseded_
// generation_fence_total failure_class label, the failure_class of the admin
// replay 422 body, and the value dashboards and docs cite, so a rename is a
// contract change that must move those together.
func TestReplayGenerationSupersededClassWireValue(t *testing.T) {
	t.Parallel()

	const want = "projector_replay_generation_superseded"
	if ReplayGenerationSupersededClass != want {
		t.Fatalf("ReplayGenerationSupersededClass = %q, want %q", ReplayGenerationSupersededClass, want)
	}
}
