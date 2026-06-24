// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// TestSharedProjectionDomainEvidenceSource pins that a promoted edge domain keeps
// its dedicated handler's evidence source so an upgrade's retract matches the edges
// the old handler wrote, while un-promoted/new domains keep the runner's global
// source (#2867). A regression here would silently leave stale edges un-retracted.
func TestSharedProjectionDomainEvidenceSource(t *testing.T) {
	t.Parallel()

	const fallback = "finalization/workloads"
	cases := []struct {
		domain string
		want   string
	}{
		{DomainInheritanceEdges, inheritanceEvidenceSource},
		{DomainHandlesRoute, fallback},
		{DomainRunsIn, fallback},
		{DomainPlatformInfra, fallback},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.domain, func(t *testing.T) {
			t.Parallel()
			if got := sharedProjectionDomainEvidenceSource(tc.domain, fallback); got != tc.want {
				t.Fatalf("sharedProjectionDomainEvidenceSource(%q) = %q, want %q", tc.domain, got, tc.want)
			}
		})
	}
	if inheritanceEvidenceSource == fallback {
		t.Fatal("inheritance evidence source must differ from the runner global to exercise the fix")
	}
}
