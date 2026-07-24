// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseFlagsUnmodeledRailsRouteConstructs is the P0 fail-safe-by-default
// regression suite (coordinator review of #5494, head 26ba26d2d): the
// PREVIOUS ambiguity detector was an allow-list of exactly two shapes
// (resources/resource macros, and an unparseable `to:` string), layered on an
// HTTP-verb allow-list. Every case below falls through BOTH the exact-route
// capture and that old allow-list, so a controller routed ONLY through one of
// them would have been silently downgraded to route_unreachable -- a LIVE
// controller called dead, the exact false-positive #5494 exists to prevent.
// Each case sets framework_semantics.rails.has_unmodeled_routes = true
// (mirroring TestParseFlagsRailsResourcesMacroAsUnmodeledRoute), because the
// fail-safe rubyScanRailsDrawBlockForAmbiguity now flags ANY call inside the
// draw block that does not resolve into an exact route entry, rather than
// enumerating known-bad shapes.
func TestParseFlagsUnmodeledRailsRouteConstructs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name: "root shorthand alongside an otherwise-exact route",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "/orders", to: "orders#index"
      root "welcome#index"
    end
  end
end
`,
		},
		{
			name: "root with explicit to: keyword",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      root to: "welcome#index"
    end
  end
end
`,
		},
		{
			name: "match with an explicit via:",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      match "/x", to: "widgets#show", via: [:get, :post]
    end
  end
end
`,
		},
		{
			name: "gem-provided route macro (devise_for)",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      devise_for :users
    end
  end
end
`,
		},
		{
			name: "controller:/action: keyword-pair route",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "/x", controller: "posts", action: "show"
    end
  end
end
`,
		},
		{
			name: "bare (non-\"/\"-prefixed) path",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "about", to: "pages#about"
    end
  end
end
`,
		},
		{
			name: "interpolated path",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "/api/#{version}/x", to: "widgets#show"
    end
  end
end
`,
		},
		{
			name: "non-string to: target",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "/x", to: SomeRackApp
    end
  end
end
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeSource(t, "routes.rb", tt.source)
			payload, err := Parse(path, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			semantics, ok := payload["framework_semantics"].(map[string]any)
			if !ok {
				t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
			}
			rails, ok := semantics["rails"].(map[string]any)
			if !ok {
				t.Fatalf("framework_semantics[rails] = %T, want map[string]any", semantics["rails"])
			}
			if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); !hasUnmodeled {
				t.Fatalf("rails[has_unmodeled_routes] = %#v, want true", rails["has_unmodeled_routes"])
			}
		})
	}
}

// TestParseFlagsUnmodeledRoutesInsideAppendAndPrependBlocks is the P1
// fail-safe regression (same defect class as the P0 fix, narrower trigger):
// isRailsRoutesDraw originally matched ONLY method=="draw", but
// Rails.application.routes.append/.prepend (real, documented Rails APIs
// engines and gems use to insert routes after/before the main set) share the
// identical receiver shape. Before the fix, a call inside an .append/.prepend
// block was invisible to BOTH exact-route capture (context never resolved to
// "rails") AND the ambiguity scan (rubyScanRailsDrawBlockForAmbiguity was
// never even invoked for it) -- so an action routed ONLY inside one of these
// blocks, in a repo whose plain .draw block was otherwise fully exact, would
// have been silently downgraded to route_unreachable. Each case here uses an
// append/prepend block as the ONLY route registration in the file (no
// separate .draw block), mirroring one of the P0 ambiguous-construct cases
// (root shorthand) to prove the block is now actually scanned.
func TestParseFlagsUnmodeledRoutesInsideAppendAndPrependBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name: "append-only block with an unmodeled root route",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.append do
      root "welcome#index"
    end
  end
end
`,
		},
		{
			name: "prepend-only block with an unmodeled root route",
			source: `class ApplicationController
  def call(env)
    Rails.application.routes.prepend do
      root "welcome#index"
    end
  end
end
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeSource(t, "routes.rb", tt.source)
			payload, err := Parse(path, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			semantics, ok := payload["framework_semantics"].(map[string]any)
			if !ok {
				t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
			}
			rails, ok := semantics["rails"].(map[string]any)
			if !ok {
				t.Fatalf("framework_semantics[rails] = %T, want map[string]any", semantics["rails"])
			}
			if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); !hasUnmodeled {
				t.Fatalf("rails[has_unmodeled_routes] = %#v, want true", rails["has_unmodeled_routes"])
			}
		})
	}
}

// TestParseCapturesExactRouteInsideAppendBlock proves append/prepend are
// treated symmetrically with draw for the POSITIVE path too, not merely
// forced into permanent ambiguity: a fully-resolvable route inside an
// append-only block (no separate .draw block) is captured into
// route_entries exactly like one inside .draw, and has_unmodeled_routes is
// NOT set -- an append/prepend-only-routed action must be confirmed via a
// genuine RoutedHandlers match, not merely tolerated via the ambiguity floor.
func TestParseCapturesExactRouteInsideAppendBlock(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "routes.rb", `class ApplicationController
  def call(env)
    Rails.application.routes.append do
      get "/widgets", to: "widgets#index"
    end
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
	}
	rails, ok := semantics["rails"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics[rails] = %T, want map[string]any", semantics["rails"])
	}
	entries, ok := rails["route_entries"].([]map[string]string)
	if !ok || len(entries) != 1 || entries[0]["handler"] != "WidgetsController.index" {
		t.Fatalf("rails[route_entries] = %#v, want one entry for WidgetsController.index", rails["route_entries"])
	}
	if hasUnmodeled, present := rails["has_unmodeled_routes"]; present && hasUnmodeled == true {
		t.Fatalf("rails[has_unmodeled_routes] = %#v, want unset/false for a fully-exact append block", hasUnmodeled)
	}
}

// TestParseRecognizesMountableEngineRoutesReceiver is the #5729 regression:
// isRailsRoutesDraw originally gated BOTH exact-route capture and the
// ambiguity scan on an EXACT-STRING receiver match
// (receiverName(receiver) == "Rails.application.routes"). A mountable Rails
// engine's own config/routes.rb is written by convention as
// `MyEngine::Engine.routes.draw do ... end` (and the .append/.prepend
// variants), whose receiver text is "MyEngine::Engine.routes" -- never equal
// to the literal "Rails.application.routes". So a controller routed ONLY
// through an internal engine's own routes file, in a repo whose main
// Rails.application.routes.draw block is otherwise exact-only, was invisible
// to BOTH exact capture AND the ambiguity scan and got silently downgraded to
// route_unreachable. The generalized structural suffix check (receiver text
// ends in ".routes") recognizes any engine route-set the same way it
// recognizes Rails.application.routes.
//
// Positive-path (exact capture): a fully-resolvable route inside an
// engine-routes block is captured into route_entries exactly like one inside
// Rails.application.routes.draw, and has_unmodeled_routes is NOT set.
func TestParseRecognizesMountableEngineRoutesReceiver(t *testing.T) {
	t.Parallel()

	t.Run("exact route captured inside engine routes draw block", func(t *testing.T) {
		t.Parallel()

		path := writeSource(t, "routes.rb", `class ApplicationController
  def call(env)
    MyEngine::Engine.routes.draw do
      get "/widgets", to: "widgets#index"
    end
  end
end
`)

		payload, err := Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("Parse() error = %v, want nil", err)
		}

		semantics, ok := payload["framework_semantics"].(map[string]any)
		if !ok {
			t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
		}
		rails, ok := semantics["rails"].(map[string]any)
		if !ok {
			t.Fatalf("framework_semantics[rails] = %T, want map[string]any", semantics["rails"])
		}
		entries, ok := rails["route_entries"].([]map[string]string)
		if !ok || len(entries) != 1 || entries[0]["handler"] != "WidgetsController.index" {
			t.Fatalf("rails[route_entries] = %#v, want one entry for WidgetsController.index", rails["route_entries"])
		}
		if hasUnmodeled, present := rails["has_unmodeled_routes"]; present && hasUnmodeled == true {
			t.Fatalf("rails[has_unmodeled_routes] = %#v, want unset/false for a fully-exact engine draw block", hasUnmodeled)
		}
	})

	// Ambiguity path: an unmodeled construct inside each engine receiver
	// variant (.draw/.append/.prepend, plus a deeper namespace) is now
	// scanned and flags has_unmodeled_routes, where before the block was
	// never recognized as route-registration context at all.
	ambiguousTests := []struct {
		name   string
		source string
	}{
		{
			name: "engine draw block with an unmodeled root route",
			source: `class ApplicationController
  def call(env)
    MyEngine::Engine.routes.draw do
      root "welcome#index"
    end
  end
end
`,
		},
		{
			name: "engine append block with an unmodeled root route",
			source: `class ApplicationController
  def call(env)
    MyEngine::Engine.routes.append do
      root "welcome#index"
    end
  end
end
`,
		},
		{
			name: "engine prepend block with an unmodeled root route",
			source: `class ApplicationController
  def call(env)
    MyEngine::Engine.routes.prepend do
      root "welcome#index"
    end
  end
end
`,
		},
		{
			name: "deeply-namespaced engine draw block",
			source: `class ApplicationController
  def call(env)
    Foo::Bar::Engine.routes.draw do
      root "welcome#index"
    end
  end
end
`,
		},
	}

	for _, tt := range ambiguousTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeSource(t, "routes.rb", tt.source)
			payload, err := Parse(path, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			semantics, ok := payload["framework_semantics"].(map[string]any)
			if !ok {
				t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
			}
			rails, ok := semantics["rails"].(map[string]any)
			if !ok {
				t.Fatalf("framework_semantics[rails] = %T, want map[string]any", semantics["rails"])
			}
			if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); !hasUnmodeled {
				t.Fatalf("rails[has_unmodeled_routes] = %#v, want true", rails["has_unmodeled_routes"])
			}
		})
	}
}

// TestParseDoesNotTreatNonRoutesReceiverAsRouteSet is the #5729 negative
// control: the generalized receiver check must key on the structural
// ".routes" suffix, NOT loosen to "any .draw/.append/.prepend call". A block
// whose receiver does not end in ".routes" (an unrelated builder DSL, or a
// bare receiverless `routes.draw`) is NOT a Rails route-set: its route-shaped
// calls must neither be captured as exact routes nor flag
// has_unmodeled_routes. If this test flags rails semantics, the suffix guard
// has regressed into matching a plain `.draw`.
func TestParseDoesNotTreatNonRoutesReceiverAsRouteSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name: "unrelated builder .draw receiver (foo.bar.draw)",
			source: `class ApplicationController
  def call(env)
    foo.bar.draw do
      get "/x", to: "widgets#show"
    end
  end
end
`,
		},
		{
			name: "bare receiverless routes.draw (no dotted receiver)",
			source: `class ApplicationController
  def call(env)
    routes.draw do
      get "/x", to: "widgets#show"
    end
  end
end
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeSource(t, "routes.rb", tt.source)
			payload, err := Parse(path, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			semantics, ok := payload["framework_semantics"].(map[string]any)
			if !ok {
				// No framework semantics at all is a valid "not a route-set"
				// outcome for this negative control.
				return
			}
			rails, ok := semantics["rails"].(map[string]any)
			if !ok {
				return
			}
			if entries, present := rails["route_entries"]; present {
				t.Fatalf("rails[route_entries] = %#v, want none for a non-.routes receiver", entries)
			}
			if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); hasUnmodeled {
				t.Fatalf("rails[has_unmodeled_routes] = true, want unset for a non-.routes receiver")
			}
		})
	}
}

// TestParseFlagsUnmodeledRouteInCurlyBraceDrawBlock proves the fail-safe scan
// also covers the one-line curly-brace block form
// (`Rails.application.routes.draw { ... }`), which tree-sitter-ruby parses as
// a "block" node rather than "do_block". RuboCop's default style enforces
// do...end for multi-line blocks, so this form is rare in practice, but a
// fail-safe scan cannot assume a style guide is followed.
func TestParseFlagsUnmodeledRouteInCurlyBraceDrawBlock(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "routes.rb", `class ApplicationController
  def call(env)
    Rails.application.routes.draw { root "welcome#index" }
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("payload[framework_semantics] = %T, want map[string]any", payload["framework_semantics"])
	}
	rails, ok := semantics["rails"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics[rails] = %T, want map[string]any", semantics["rails"])
	}
	if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); !hasUnmodeled {
		t.Fatalf("rails[has_unmodeled_routes] = %#v, want true", rails["has_unmodeled_routes"])
	}
}
