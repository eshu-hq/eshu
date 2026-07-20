// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import "testing"

// TestSameFileRegistrySuffixMatchesAlwaysEmpty is the #5376 P0 rev-2 property
// test: the parser's same-file adapter must return NO proper-suffix matches for
// ANY ref. This is what makes the parser's same-file behavior provably unchanged
// by the identity-carrying walk — with SuffixMatches ≡ ∅, the suffix-only probe
// and suffix-ambiguity steps can never fire, so a qualified base the file cannot
// resolve exactly still lands on the F1 keep-biased floor exactly as before.
func TestSameFileRegistrySuffixMatchesAlwaysEmpty(t *testing.T) {
	t.Parallel()

	reg := rubySameFileControllerRegistry{registry: rubyClassRegistry{
		known: map[string]struct{}{
			"Base": {}, "OrdersController": {}, "BaseController": {}, "AdminController": {},
		},
		superclass: map[string]string{
			"OrdersController": "Admin::Base",
			"BaseController":   "ActionController::Base",
			"AdminController":  "BaseController",
		},
	}}

	refs := []string{
		"Base", "Admin::Base", "Api::Base", "Internal::Api::Base", "OrdersController",
		"BaseController", "Admin::BaseController", "ActionController::Base", "Foo::Bar::Base",
		"::Base", "Thor", "ApplicationRecord",
	}
	for _, ref := range refs {
		if got := reg.SuffixMatches(ref); len(got) != 0 {
			t.Fatalf("SuffixMatches(%q) = %#v, want empty (parser same-file adapter must never proper-suffix match)", ref, got)
		}
	}

	// ExactMatches must still resolve exact same-file class names, and only those.
	if got := reg.ExactMatches("BaseController"); len(got) != 1 || got[0] != "BaseController" {
		t.Fatalf("ExactMatches(BaseController) = %#v, want [BaseController]", got)
	}
	if got := reg.ExactMatches("Admin::Base"); len(got) != 0 {
		t.Fatalf("ExactMatches(Admin::Base) = %#v, want empty (qualified ref has no exact simple-name match)", got)
	}
}
