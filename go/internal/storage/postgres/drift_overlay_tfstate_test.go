// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/tfstate"
)

// TestTFStateOverlayTextsDerive is the hermetic shape guard for the
// #10006 overlay texts: every active text the Terraform drift overlay
// runs must derive a base text through the shared helper, binding the
// delta-active scope ids at a placeholder the active text leaves unused.
// A future edit that breaks the shape (renamed alias, dropped active
// predicate, reused placeholder) fails here instead of failing closed
// at runtime.
func TestTFStateOverlayTextsDerive(t *testing.T) {
	t.Parallel()

	active := []struct {
		name       string
		text       string
		scopeParam int
	}{
		{"canonical", tfstate.ActiveTerraformBackendCanonicalQuery, 1},
		{"backend_facts", tfstate.ActiveTerraformBackendFactsQuery, 2},
		{"config", tfstate.ActiveConfigResourcesForOverlayQuery, 2},
		{"modules", tfstate.ActiveModuleCallsForOverlayQuery, 2},
	}
	for _, tc := range active {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, err := deriveDeltaOverlayBaseQuery(tc.text, tc.scopeParam)
			if err != nil {
				t.Fatalf("deriveDeltaOverlayBaseQuery(%s) error = %v", tc.name, err)
			}
			for _, fragment := range []string{
				"active_delta",
				"delta_union_base_commit_sha",
				"fact.scope_id = ANY($",
			} {
				if !strings.Contains(base, fragment) {
					t.Errorf("derived %s base query missing %q", tc.name, fragment)
				}
			}
			if strings.Contains(base, "scope.active_generation_id = fact.generation_id") {
				t.Errorf("derived %s base query still carries the active scope join", tc.name)
			}
		})
	}
}
