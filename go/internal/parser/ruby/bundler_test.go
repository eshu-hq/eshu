// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseGemfileEmitsRubyGemsDependencies(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Gemfile", `source "https://rubygems.org"

gem "rails", "~> 7.1"

group :development, :test do
  gem "rspec-rails", "~> 6.1"
end

gem "rubocop", group: [:development, :test]
gem "alternate_gem", source: "https://gems.example.com"
gem "internal_admin", git: "https://example.com/internal/admin.git", branch: "main"
gem "local_component", path: "../components/local"
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := rubyDependencyRowsByName(t, payload)
	assertRubyDependencyString(t, rows["rails"], "package_manager", "rubygems")
	assertRubyDependencyString(t, rows["rails"], "value", "~> 7.1")
	assertRubyDependencyString(t, rows["rails"], "section", "default")
	assertRubyDependencyString(t, rows["rails"], "dependency_scope", "runtime")
	assertRubyDependencyBool(t, rows["rails"], "development_dependency", false)

	assertRubyDependencyString(t, rows["rspec-rails"], "section", "development,test")
	assertRubyDependencyString(t, rows["rspec-rails"], "dependency_scope", "development,test")
	assertRubyDependencyBool(t, rows["rspec-rails"], "development_dependency", true)

	assertRubyDependencyString(t, rows["rubocop"], "section", "development,test")
	assertRubyDependencyString(t, rows["rubocop"], "dependency_scope", "development,test")
	assertRubyDependencyBool(t, rows["rubocop"], "development_dependency", true)

	assertRubyDependencyString(t, rows["alternate_gem"], "source_type", "rubygems")
	assertRubyDependencyString(t, rows["alternate_gem"], "source_path", "https://gems.example.com")

	assertRubyDependencyString(t, rows["internal_admin"], "source_type", "git")
	assertRubyDependencyString(t, rows["internal_admin"], "source_path", "https://example.com/internal/admin.git")
	assertRubyDependencyBool(t, rows["internal_admin"], "source_ambiguous", true)

	assertRubyDependencyString(t, rows["local_component"], "source_type", "path")
	assertRubyDependencyString(t, rows["local_component"], "source_path", "../components/local")
	assertRubyDependencyBool(t, rows["local_component"], "source_ambiguous", true)
}

func TestParseGemfileLockEmitsExactVersionsAndDependencyChains(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Gemfile.lock", `GEM
  remote: https://rubygems.org/
  specs:
    rails (7.1.3)
      rack (>= 2.2.4)
    rack (2.2.8)
    puma (6.4.2)

PLATFORMS
  ruby

DEPENDENCIES
  rails (~> 7.1)
  puma

BUNDLED WITH
   2.5.6
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := rubyDependencyRowsByName(t, payload)
	assertRubyDependencyString(t, rows["rails"], "package_manager", "rubygems")
	assertRubyDependencyString(t, rows["rails"], "value", "7.1.3")
	assertRubyDependencyString(t, rows["rails"], "section", "gemfile.lock")
	assertRubyDependencyBool(t, rows["rails"], "lockfile", true)
	assertRubyDependencyPath(t, rows["rails"], []string{"rails"})
	assertRubyDependencyInt(t, rows["rails"], "dependency_depth", 1)
	assertRubyDependencyBool(t, rows["rails"], "direct_dependency", true)

	assertRubyDependencyString(t, rows["rack"], "value", "2.2.8")
	assertRubyDependencyPath(t, rows["rack"], []string{"rails", "rack"})
	assertRubyDependencyInt(t, rows["rack"], "dependency_depth", 2)
	assertRubyDependencyBool(t, rows["rack"], "direct_dependency", false)

	assertRubyDependencyString(t, rows["puma"], "value", "6.4.2")
	assertRubyDependencyPath(t, rows["puma"], []string{"puma"})
	assertRubyDependencyBool(t, rows["puma"], "direct_dependency", true)
}

func TestParseGemfileLockPreservesGitAndPathAmbiguity(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Gemfile.lock", `GIT
  remote: https://example.com/acme/internal_admin.git
  revision: 0123456789abcdef
  specs:
    internal_admin (0.1.0)

PATH
  remote: ../components/local
  specs:
    local_component (0.2.0)

DEPENDENCIES
  internal_admin!
  local_component!
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := rubyDependencyRowsByName(t, payload)
	assertRubyDependencyString(t, rows["internal_admin"], "value", "0.1.0")
	assertRubyDependencyString(t, rows["internal_admin"], "source_type", "git")
	assertRubyDependencyString(t, rows["internal_admin"], "source_path", "https://example.com/acme/internal_admin.git")
	assertRubyDependencyBool(t, rows["internal_admin"], "source_ambiguous", true)
	assertRubyDependencyPath(t, rows["internal_admin"], []string{"internal_admin"})
	assertRubyDependencyBool(t, rows["internal_admin"], "direct_dependency", true)

	assertRubyDependencyString(t, rows["local_component"], "value", "0.2.0")
	assertRubyDependencyString(t, rows["local_component"], "source_type", "path")
	assertRubyDependencyString(t, rows["local_component"], "source_path", "../components/local")
	assertRubyDependencyBool(t, rows["local_component"], "source_ambiguous", true)
	assertRubyDependencyPath(t, rows["local_component"], []string{"local_component"})
	assertRubyDependencyBool(t, rows["local_component"], "direct_dependency", true)
}

func TestParseGemfileLockAcceptsCRLFSectionHeaders(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Gemfile.lock", "GEM\r\n  remote: https://rubygems.org/\r\n  specs:\r\n    rails (7.1.3)\r\n\r\nDEPENDENCIES\r\n  rails (~> 7.1)\r\n")

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := rubyDependencyRowsByName(t, payload)
	assertRubyDependencyString(t, rows["rails"], "value", "7.1.3")
	assertRubyDependencyPath(t, rows["rails"], []string{"rails"})
	assertRubyDependencyBool(t, rows["rails"], "direct_dependency", true)
}

func TestParseGemfileKeepsScopeAcrossNonBundlerBlocks(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "Gemfile", `group :development do
  ["rspec-rails"].each do |name|
    puts name
  end
  gem "rubocop", "~> 1.75"
end
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := rubyDependencyRowsByName(t, payload)
	assertRubyDependencyString(t, rows["rubocop"], "section", "development")
	assertRubyDependencyString(t, rows["rubocop"], "dependency_scope", "development")
	assertRubyDependencyBool(t, rows["rubocop"], "development_dependency", true)
}

func TestParseBundlerMalformedFilesDoNotEmitDependencies(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "Gemfile", body: `gem dependency_name, version_range`},
		{name: "Gemfile.lock", body: `GEM
  specs:
    broken (`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload, err := Parse(writeSource(t, tc.name, tc.body), false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			for _, row := range rubyDependencyRows(t, payload) {
				if row["config_kind"] == "dependency" {
					t.Fatalf("%s emitted dependency row from malformed Bundler input: %#v", tc.name, row)
				}
			}
		})
	}
}

func rubyDependencyRowsByName(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()

	rowsByName := make(map[string]map[string]any)
	for _, row := range rubyDependencyRows(t, payload) {
		if row["config_kind"] != "dependency" {
			continue
		}
		name, _ := row["name"].(string)
		if name != "" {
			rowsByName[name] = row
		}
	}
	return rowsByName
}

func rubyDependencyRows(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[variables] = %T, want []map[string]any", payload["variables"])
	}
	return rows
}

func assertRubyDependencyString(t *testing.T, row map[string]any, field string, want string) {
	t.Helper()
	if row == nil {
		t.Fatalf("dependency row missing, want %s=%q", field, want)
	}
	if got := row[field]; got != want {
		t.Fatalf("dependency row[%s] = %#v, want %q in %#v", field, got, want, row)
	}
}

func assertRubyDependencyBool(t *testing.T, row map[string]any, field string, want bool) {
	t.Helper()
	if row == nil {
		t.Fatalf("dependency row missing, want %s=%v", field, want)
	}
	if got := row[field]; got != want {
		t.Fatalf("dependency row[%s] = %#v, want %v in %#v", field, got, want, row)
	}
}

func assertRubyDependencyInt(t *testing.T, row map[string]any, field string, want int) {
	t.Helper()
	if row == nil {
		t.Fatalf("dependency row missing, want %s=%d", field, want)
	}
	if got := row[field]; got != want {
		t.Fatalf("dependency row[%s] = %#v, want %d in %#v", field, got, want, row)
	}
}

func assertRubyDependencyPath(t *testing.T, row map[string]any, want []string) {
	t.Helper()
	if row == nil {
		t.Fatalf("dependency row missing, want dependency_path=%#v", want)
	}
	if got := row["dependency_path"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency_path = %#v, want %#v in %#v", got, want, row)
	}
}
