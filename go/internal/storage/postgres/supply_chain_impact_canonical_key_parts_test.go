// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestCanonicalKeyCarriesStatusAndObservedVersion pins the premise of the status
// prune of the unscoped precise read (#10880 PR-B2a): impact_status and
// observed_version are parts of the SQL canonical key, so every support of one key
// shares them with the key's winner. The prune skips a winner whose status or
// observed version rules precise out because no other support of that key can be
// precise (TestPreciseImpliesStatusAndObservedVersion pins the classifier side).
// The key text is the one every candidate CTE and the staged scan batch share.
func TestCanonicalKeyCarriesStatusAndObservedVersion(t *testing.T) {
	t.Parallel()
	args := supplyChainImpactCanonicalKeyArgs("fact.payload", "")
	for _, part := range []string{"'observed_version'", "'impact_status'"} {
		if !strings.Contains(args, part) {
			t.Errorf("the canonical key arguments lost %s: the precise status prune would skip a key that has a precise support", part)
		}
	}
	// Seeded violation: the same check rejects a key text without the status.
	broken := strings.Replace(args, "'impact_status'", "'impact_state'", 1)
	if strings.Contains(broken, "'impact_status'") {
		t.Fatal("the seeded violation did not apply")
	}
}
