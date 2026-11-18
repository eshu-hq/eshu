// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"
)

func TestNVDWindowFeedSelectorFoldsOntoClosedSet(t *testing.T) {
	t.Parallel()
	for _, class := range []string{NVDFeedSelectorBeforeSeed, NVDFeedSelectorCoveredSeed, NVDFeedSelectorBeyondSeed, NVDFeedSelectorUnavailable} {
		if got := NVDWindowFeedSelector(class); got != class {
			t.Fatalf("NVDWindowFeedSelector(%q) = %q, want itself", class, got)
		}
	}
	for _, value := range []string{"", "NVD_FEED_BEFORE_SEED", "covered", "nvd_feed_probe_failed"} {
		if got := NVDWindowFeedSelector(value); got != NVDFeedSelectorOther {
			t.Fatalf("NVDWindowFeedSelector(%q) = %q, want %q", value, got, NVDFeedSelectorOther)
		}
	}
	want := []string{"nvd_feed_before_seed", "nvd_feed_covered_seed", "nvd_feed_beyond_seed", "nvd_feed_seed_unavailable", "other"}
	if got := NVDWindowFeedSelectors(); !slices.Equal(got, want) {
		t.Fatalf("NVDWindowFeedSelectors() = %v, want %v", got, want)
	}
}
