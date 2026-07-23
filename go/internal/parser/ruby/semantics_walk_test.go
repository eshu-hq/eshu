// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// These tests characterize framework-route detection and dead-code-root
// tagging (issue #4842) so consolidating the three dead-code walks and the
// framework route walk into one merged pass cannot silently change which
// routes or root kinds are captured, or their order. None of the fixtures
// under tests/fixtures exercise Rails/Sinatra route resolution or every
// dead-code root kind, so these are new characterization coverage, not a
// port of existing assertions.

func TestParseCapturesRailsRoutesInDrawOrder(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "routes.rb", `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "/orders", to: "orders#index"
      post "/orders", to: "orders#create"
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
	if !ok || len(entries) != 2 {
		t.Fatalf("rails route_entries = %#v, want 2 entries", rails["route_entries"])
	}
	if entries[0]["method"] != "GET" || entries[0]["path"] != "/orders" || entries[0]["handler"] != "OrdersController.index" {
		t.Fatalf("route_entries[0] = %#v, want GET /orders OrdersController.index", entries[0])
	}
	if entries[1]["method"] != "POST" || entries[1]["path"] != "/orders" || entries[1]["handler"] != "OrdersController.create" {
		t.Fatalf("route_entries[1] = %#v, want POST /orders OrdersController.create", entries[1])
	}
}

func TestParseCapturesSinatraRouteWithClassContext(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "app.rb", `class App < Sinatra::Base
  get "/health", &method(:health_check)

  def health_check
    'ok'
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
	sinatra, ok := semantics["sinatra"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics[sinatra] = %T, want map[string]any", semantics["sinatra"])
	}
	entries, ok := sinatra["route_entries"].([]map[string]string)
	if !ok || len(entries) != 1 {
		t.Fatalf("sinatra route_entries = %#v, want 1 entry", sinatra["route_entries"])
	}
	if entries[0]["method"] != "GET" || entries[0]["path"] != "/health" || entries[0]["handler"] != "App.health_check" {
		t.Fatalf("route_entries[0] = %#v, want GET /health App.health_check", entries[0])
	}
}

func TestParseFlagsRailsResourcesMacroAsUnmodeledRoute(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "routes.rb", `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      resources :posts
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
	if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); !hasUnmodeled {
		t.Fatalf("rails[has_unmodeled_routes] = %#v, want true", rails["has_unmodeled_routes"])
	}
	frameworks, _ := semantics["frameworks"].([]string)
	if !containsString(frameworks, "rails") {
		t.Fatalf("frameworks = %#v, want to contain rails", frameworks)
	}
}

func TestParseFlagsNamespacedRailsRouteTargetAsUnmodeled(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "routes.rb", `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "/admin/posts", to: "admin/posts#show"
      get "/orders", to: "orders#index"
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
	if hasUnmodeled, _ := rails["has_unmodeled_routes"].(bool); !hasUnmodeled {
		t.Fatalf("rails[has_unmodeled_routes] = %#v, want true", rails["has_unmodeled_routes"])
	}
	// The exact, resolvable "orders#index" route is still captured alongside
	// the ambiguity flag -- the namespaced target does not suppress the exact
	// route this same file also registers.
	entries, ok := rails["route_entries"].([]map[string]string)
	if !ok || len(entries) != 1 || entries[0]["handler"] != "OrdersController.index" {
		t.Fatalf("rails[route_entries] = %#v, want one entry for OrdersController.index", rails["route_entries"])
	}
}

func TestParseExactOnlyRailsRoutesLeaveHasUnmodeledRoutesUnset(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "routes.rb", `class ApplicationController
  def call(env)
    Rails.application.routes.draw do
      get "/orders", to: "orders#index"
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
	if hasUnmodeled, present := rails["has_unmodeled_routes"]; present && hasUnmodeled == true {
		t.Fatalf("rails[has_unmodeled_routes] = %#v, want unset/false", hasUnmodeled)
	}
}

func TestParseTagsRailsCallbackDeadCodeRoot(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "controller.rb", `class WidgetsController
  before_action :authenticate!

  def index
  end

  private

  def authenticate!
    true
  end
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	fn := assertBucketName(t, payload, "functions", "authenticate!")
	rootKinds, _ := fn["dead_code_root_kinds"].([]string)
	if !containsString(rootKinds, "ruby.rails_callback_method") {
		t.Fatalf("functions[authenticate!][dead_code_root_kinds] = %#v, want ruby.rails_callback_method", rootKinds)
	}
}

func TestParseTagsMethodReferenceDeadCodeRoot(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "worker.rb", `def helper
  'value'
end

def dispatch
  method(:helper).call
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	fn := assertBucketName(t, payload, "functions", "helper")
	rootKinds, _ := fn["dead_code_root_kinds"].([]string)
	if !containsString(rootKinds, "ruby.method_reference_target") {
		t.Fatalf("functions[helper][dead_code_root_kinds] = %#v, want ruby.method_reference_target", rootKinds)
	}
}

func TestParseTagsScriptEntrypointDeadCodeRoot(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "script.rb", `def main
  'run'
end

if __FILE__ == $PROGRAM_NAME
  main
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	fn := assertBucketName(t, payload, "functions", "main")
	rootKinds, _ := fn["dead_code_root_kinds"].([]string)
	if !containsString(rootKinds, "ruby.script_entrypoint") {
		t.Fatalf("functions[main][dead_code_root_kinds] = %#v, want ruby.script_entrypoint", rootKinds)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
