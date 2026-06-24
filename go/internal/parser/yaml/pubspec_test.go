// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyCoveragePubspecLockEmitsHostedExactPubRows(t *testing.T) {
	t.Parallel()

	payload := parsePubspecFixture(t, "pubspec.lock", `packages:
  http:
    dependency: "direct main"
    description:
      name: http
      sha256: "sha256-http"
      url: "https://pub.dev"
    source: hosted
    version: "1.2.2"
  collection:
    dependency: transitive
    description:
      name: collection
      url: "https://pub.dartlang.org"
    source: hosted
    version: "1.18.0"
  git_dep:
    dependency: "direct main"
    description:
      url: "https://github.com/example/git_dep.git"
    source: git
    version: "1.0.0"
  private_hosted:
    dependency: transitive
    description:
      name: private_hosted
      url: "https://pub.internal.example"
    source: hosted
    version: "2.0.0"
  mismatched:
    dependency: transitive
    description:
      name: wrong_name
      url: "https://pub.dev"
    source: hosted
    version: "3.0.0"
`)

	rows := pubDependencyRowsByName(t, payload)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("dependency rows = %d, want %d: %#v", got, want, rows)
	}
	http := rows["http"]
	assertPubRowValue(t, http, "package_manager", "pub")
	assertPubRowValue(t, http, "value", "1.2.2")
	assertPubRowValue(t, http, "source_location", "https://pub.dev")
	assertPubRowValue(t, http, "sha256", "sha256-http")
	assertPubRowValue(t, http, "dependency_scope", "runtime")
	assertPubRowValue(t, http, "pub_dependency_kind", "direct main")
	if got := http["lockfile"]; got != true {
		t.Fatalf("http lockfile = %#v, want true", got)
	}
	if got := http["direct_dependency"]; got != true {
		t.Fatalf("http direct_dependency = %#v, want true", got)
	}

	collection := rows["collection"]
	assertPubRowValue(t, collection, "value", "1.18.0")
	assertPubRowValue(t, collection, "source_location", "https://pub.dev")
	assertPubRowValue(t, collection, "dependency_scope", "transitive")
	if got := collection["direct_dependency"]; got != false {
		t.Fatalf("collection direct_dependency = %#v, want false", got)
	}
}

func TestDependencyCoveragePubspecYamlEmitsRangeOnlyAndFailsClosedForOverrides(t *testing.T) {
	t.Parallel()

	payload := parsePubspecFixture(t, "pubspec.yaml", `name: demo
dependencies:
  http: ^1.2.0
  collection:
    hosted:
      name: collection
      url: https://pub.dev
    version: ^1.18.0
  private_hosted:
    hosted: https://pub.internal.example
    version: ^2.0.0
  git_dep:
    git: https://github.com/example/git_dep.git
dev_dependencies:
  test: any
`)

	rows := pubDependencyRowsByName(t, payload)
	if got, want := len(rows), 3; got != want {
		t.Fatalf("dependency rows = %d, want %d: %#v", got, want, rows)
	}
	http := rows["http"]
	assertPubRowValue(t, http, "package_manager", "pub")
	assertPubRowValue(t, http, "value", "^1.2.0")
	assertPubRowValue(t, http, "dependency_scope", "runtime")
	if got := http["lockfile"]; got == true {
		t.Fatalf("http lockfile = %#v, want false or absent for manifest-only range", got)
	}
	collection := rows["collection"]
	assertPubRowValue(t, collection, "value", "^1.18.0")
	assertPubRowValue(t, collection, "source_location", "https://pub.dev")
	testDep := rows["test"]
	assertPubRowValue(t, testDep, "value", "any")
	assertPubRowValue(t, testDep, "dependency_scope", "dev")
	if got := testDep["development_dependency"]; got != true {
		t.Fatalf("test development_dependency = %#v, want true", got)
	}

	overridePayload := parsePubspecFixture(t, "pubspec.yaml", `name: demo
dependencies:
  http: ^1.2.0
dependency_overrides:
  http: 1.2.2
`)
	if rows := pubDependencyRowsByName(t, overridePayload); len(rows) != 0 {
		t.Fatalf("dependency_overrides emitted dependency rows %#v; want fail-closed", rows)
	}

	overridesFile := parsePubspecFixture(t, "pubspec_overrides.yaml", `dependency_overrides:
  http: 1.2.2
`)
	if rows := pubDependencyRowsByName(t, overridesFile); len(rows) != 0 {
		t.Fatalf("pubspec_overrides.yaml emitted dependency rows %#v; want fail-closed", rows)
	}
}

func parsePubspecFixture(t *testing.T, filename string, body string) map[string]any {
	t.Helper()

	path := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("%s: write fixture: %v", filename, err)
	}
	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("%s: Parse() error = %v", filename, err)
	}
	return payload
}

func pubDependencyRowsByName(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables payload missing (got %T)", payload["variables"])
	}
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		if row["config_kind"] != "dependency" {
			continue
		}
		name, _ := row["name"].(string)
		if name != "" {
			out[name] = row
		}
	}
	return out
}

func assertPubRowValue(t *testing.T, row map[string]any, key string, want string) {
	t.Helper()

	if got := row[key]; got != want {
		t.Fatalf("%s = %#v, want %#v in row %#v", key, got, want, row)
	}
}
