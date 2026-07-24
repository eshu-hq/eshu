// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestIsRailsRouteSetReceiver directly exercises the #5772 route-set receiver
// predicate's branches (linuxdynasty PR #5774 P2): the application form, engine
// route-sets under every Rails naming (conventional `::Engine`, a custom
// `*Engine` class, and a fully custom-named engine class), and the negatives a
// route-set receiver must never match.
func TestIsRailsRouteSetReceiver(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		receiver string
		want     bool
	}{
		{"Rails.application.routes", true},       // main application
		{"MyEngine::Engine.routes", true},        // conventional namespaced engine
		{"Foo::Bar::Engine.routes", true},        // deeply namespaced engine
		{"PaymentsEngine.routes", true},          // custom *Engine class, top level
		{"Api.routes", true},                     // fully custom-named engine class
		{"Engine.routes", true},                  // top-level `class Engine < Rails::Engine`
		{"foo.routes", false},                    // lowercase builder -- not a constant
		{"foo.bar.routes", false},                // lowercase method chain
		{"Foo.bar.routes", false},                // constant + method call, not a pure constant path
		{"routes", false},                        // bare receiverless `.routes`
		{"Rails.application.route_paths", false}, // not the `.routes` accessor
		{"", false},                              // empty
		{"::Api.routes", false},                  // leading `::` yields an empty first segment
	} {
		tc := tc
		t.Run(tc.receiver, func(t *testing.T) {
			t.Parallel()
			if got := isRailsRouteSetReceiver(tc.receiver); got != tc.want {
				t.Fatalf("isRailsRouteSetReceiver(%q) = %v, want %v", tc.receiver, got, tc.want)
			}
		})
	}
}

// TestParseRecognizesCustomNamedEngineRouteSet is the #5772 positive parse guard
// (codex PR #5774 P1: a Rails engine need not be named `Engine`): a route-set
// registered on a custom-named engine class must still be captured for exact
// routing, not missed and left to a false `route_unreachable` downgrade.
func TestParseRecognizesCustomNamedEngineRouteSet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		source string
	}{
		{
			name: "canonical namespaced engine (MyEngine::Engine.routes.draw)",
			source: `class WidgetsController; def show; end; end
MyEngine::Engine.routes.draw do
  get "/x", to: "widgets#show"
end
`,
		},
		{
			name: "custom top-level engine class (PaymentsEngine.routes.draw)",
			source: `class WidgetsController; def show; end; end
PaymentsEngine.routes.draw do
  get "/x", to: "widgets#show"
end
`,
		},
		{
			name: "fully custom-named engine class (Api.routes.draw)",
			source: `class WidgetsController; def show; end; end
Api.routes.draw do
  get "/x", to: "widgets#show"
end
`,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeSource(t, "routes.rb", tc.source)
			payload, err := Parse(path, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			semantics, ok := payload["framework_semantics"].(map[string]any)
			if !ok {
				t.Fatalf("framework_semantics missing, want a rails route-set for %q", tc.name)
			}
			rails, ok := semantics["rails"].(map[string]any)
			if !ok {
				t.Fatalf("framework_semantics[rails] missing, want route capture for %q", tc.name)
			}
			entries, ok := rails["route_entries"].([]map[string]string)
			if !ok || len(entries) == 0 {
				t.Fatalf("rails[route_entries] = %#v, want the exact WidgetsController.show route captured", rails["route_entries"])
			}
		})
	}
}

// TestParseDoesNotTreatArbitraryDotRoutesReceiverAsRails is the #5729 follow-up
// regression guard (codex PR #5768 P1): recognizing mountable-engine route-sets
// must NOT promote an arbitrary `.routes` accessor on a non-Rails receiver to
// Rails truth. `foo.routes.draw { ... }` is a plain Ruby DSL on some lowercase
// builder `foo`, not the application (`Rails.application.routes`) or a constant
// engine route-set. Treating it as Rails would emit a false
// framework_semantics.rails.route_entries (and, once WidgetsController.show
// resolves uniquely, a false HANDLES_ROUTE edge), or a spurious repo-wide
// has_unmodeled_routes keep floor when the block contains an unmodeled call.
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
