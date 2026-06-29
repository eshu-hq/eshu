// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathRubyEmitsExactRailsRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "routes.rb")
	writeTestFile(
		t,
		sourcePath,
		`Rails.application.routes.draw do
  get "/reports/:id", to: "reports#show"
  post "/reports", to: "reports#create"
  patch dynamic_path, to: "reports#update"
  delete "/reports/:id", to: dynamic_handler
  get "/admin/reports/:id", to: "admin/reports#show"
end

class ReportsController
  def show
  end

  def create
  end
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "rails")
	assertNestedRouteEntriesEqual(t, got, "rails", []map[string]string{
		{"method": "GET", "path": "/reports/:id", "handler": "ReportsController.show"},
		{"method": "POST", "path": "/reports", "handler": "ReportsController.create"},
	})
}

func TestDefaultEngineParsePathRubyEmitsExactSinatraMethodRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "app.rb")
	writeTestFile(
		t,
		sourcePath,
		`require "sinatra/base"

class ReportsApp < Sinatra::Base
  get "/health", &method(:health)
  post("/reports", &method(:create_report))
  get "/anonymous" do
    "anonymous"
  end
  get dynamic_path, &method(:dynamic_path)

  def health
  end

  def create_report
  end
end

class RouteHelpers
  get "/helper", &method(:helper)

  def helper
  end
end
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	assertFrameworksEqual(t, got, "sinatra")
	assertNestedRouteEntriesEqual(t, got, "sinatra", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "ReportsApp.health"},
		{"method": "POST", "path": "/reports", "handler": "ReportsApp.create_report"},
	})
}
