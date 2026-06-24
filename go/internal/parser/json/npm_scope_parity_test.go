// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParsePackageJSONEmitsRuntimeDevOptionalAndPeerScopes(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package.json", `{
  "dependencies": {"express": "^4.18.2"},
  "devDependencies": {"vitest": "^2.0.0"},
  "optionalDependencies": {"fsevents": "^2.3.3"},
  "peerDependencies": {"react": ">=18"}
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse(package.json) error = %v", err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	got := dependencyRowsByName(rows)

	assertNPMDependencyScope(t, got["express"], "dependencies", "runtime", false)
	assertNPMDependencyScope(t, got["vitest"], "devDependencies", "dev", true)
	assertNPMDependencyScope(t, got["fsevents"], "optionalDependencies", "optional", false)
	assertNPMDependencyScope(t, got["react"], "peerDependencies", "peer", false)
}

func TestParsePackageLockEmitsExactVersionScopeFlags(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package-lock.json", `{
  "name": "demo",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "dependencies": {"express": "^4.18.2"},
      "devDependencies": {"vitest": "^2.0.0"},
      "optionalDependencies": {"fsevents": "^2.3.3"},
      "peerDependencies": {"react": ">=18"}
    },
    "node_modules/express": {"version": "4.18.2"},
    "node_modules/vitest": {"version": "2.0.0", "dev": true},
    "node_modules/fsevents": {"version": "2.3.3", "optional": true},
    "node_modules/react": {"version": "18.3.1"}
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse(package-lock.json) error = %v", err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	got := dependencyRowsByName(rows)

	assertNPMDependencyScope(t, got["express"], "package-lock", "runtime", false)
	assertNPMDependencyScope(t, got["vitest"], "package-lock", "dev", true)
	assertNPMDependencyScope(t, got["fsevents"], "package-lock", "optional", false)
	assertNPMDependencyScope(t, got["react"], "package-lock", "peer", false)
	assertPackageLockChain(t, got["react"], []string{"react"}, 1, true)
}

func assertNPMDependencyScope(
	t *testing.T,
	row map[string]any,
	wantSection string,
	wantScope string,
	wantDev bool,
) {
	t.Helper()
	if row == nil {
		t.Fatalf("dependency row missing")
	}
	if got := row["package_manager"]; got != "npm" {
		t.Fatalf("package_manager = %#v, want npm", got)
	}
	if got := row["section"]; got != wantSection {
		t.Fatalf("section = %#v, want %q", got, wantSection)
	}
	if got := row["dependency_scope"]; got != wantScope {
		t.Fatalf("dependency_scope = %#v, want %q", got, wantScope)
	}
	if got := row["development_dependency"]; got != wantDev {
		t.Fatalf("development_dependency = %#v, want %v", got, wantDev)
	}
}
