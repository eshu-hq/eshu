// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseDoesNotTreatArbitraryDotRoutesReceiverAsRails is the #5729 follow-up
// regression guard (codex PR #5768 P1): recognizing mountable-engine route-sets
// by a `.routes` suffix must NOT promote an arbitrary `.routes` accessor on a
// non-Rails receiver to Rails truth. `foo.routes.draw { get "/x", to:
// "widgets#show" }` is a plain Ruby DSL on some builder named `foo`, not the
// application (`Rails.application.routes`) or an engine (`::Engine.routes`)
// route-set. Treating it as Rails would emit a false
// framework_semantics.rails.route_entries (and, once WidgetsController.show
// resolves uniquely, a false HANDLES_ROUTE edge), or a spurious repo-wide
// has_unmodeled_routes keep floor when the block contains an unmodeled call.
// isRailsRouteSetReceiver claims Rails only for the two source-proven shapes.
func TestParseDoesNotTreatArbitraryDotRoutesReceiverAsRails(t *testing.T) {
	t.Parallel()

	source := `class ApplicationController
  def call(env)
    foo.routes.draw do
      get "/x", to: "widgets#show"
    end
  end
end
`
	path := writeSource(t, "routes.rb", source)
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		// No framework semantics at all is a valid "not a Rails route-set"
		// outcome for this negative control.
		return
	}
	rails, ok := semantics["rails"].(map[string]any)
	if !ok {
		return
	}
	if entries, present := rails["route_entries"]; present {
		t.Fatalf("rails[route_entries] = %#v, want none for a non-Rails foo.routes.draw receiver", entries)
	}
	if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); hasUnmodeled {
		t.Fatalf("rails[has_unmodeled_routes] = true, want unset for a non-Rails foo.routes.draw receiver")
	}
}
