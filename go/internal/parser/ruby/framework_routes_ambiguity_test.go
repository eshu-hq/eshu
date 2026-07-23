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
