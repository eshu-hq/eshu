// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseComposerLockEmitsRuntimeAndDevDependencyRows proves that
// composer.lock entries from the `packages` and `packages-dev` arrays are
// promoted into separate dependency rows that preserve the runtime/dev scope
// alongside the exact installed version. Without the scope split the supply
// chain reducer cannot bound impact to production code paths.
func TestParseComposerLockEmitsRuntimeAndDevDependencyRows(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "composer.lock", `{
  "_readme": ["sample lockfile"],
  "content-hash": "abc123",
  "packages": [
    {
      "name": "monolog/monolog",
      "version": "2.9.1",
      "type": "library",
      "require": {"psr/log": "^2.0"}
    },
    {
      "name": "psr/log",
      "version": "2.0.0",
      "type": "library"
    }
  ],
  "packages-dev": [
    {
      "name": "phpunit/phpunit",
      "version": "9.6.13",
      "type": "library"
    }
  ]
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	rowsByName := dependencyRowsByName(rows)

	runtime, ok := rowsByName["monolog/monolog"]
	if !ok {
		t.Fatalf("monolog/monolog missing from rows: %#v", rows)
	}
	if runtime["value"] != "2.9.1" {
		t.Fatalf("monolog value = %#v, want %q", runtime["value"], "2.9.1")
	}
	if runtime["section"] != "packages" {
		t.Fatalf("monolog section = %#v, want %q", runtime["section"], "packages")
	}
	if runtime["package_manager"] != "composer" {
		t.Fatalf("monolog package_manager = %#v, want composer", runtime["package_manager"])
	}
	if runtime["config_kind"] != "dependency" {
		t.Fatalf("monolog config_kind = %#v, want dependency", runtime["config_kind"])
	}
	if runtime["lockfile"] != true {
		t.Fatalf("monolog lockfile = %#v, want true", runtime["lockfile"])
	}

	dev, ok := rowsByName["phpunit/phpunit"]
	if !ok {
		t.Fatalf("phpunit/phpunit missing from rows: %#v", rows)
	}
	if dev["section"] != "packages-dev" {
		t.Fatalf("phpunit section = %#v, want %q", dev["section"], "packages-dev")
	}
	if dev["value"] != "9.6.13" {
		t.Fatalf("phpunit value = %#v, want %q", dev["value"], "9.6.13")
	}

	psr, ok := rowsByName["psr/log"]
	if !ok {
		t.Fatalf("psr/log missing from rows: %#v", rows)
	}
	if psr["section"] != "packages" {
		t.Fatalf("psr/log section = %#v, want %q", psr["section"], "packages")
	}
}

// TestParseComposerLockMalformedReturnsNoDependencyRows verifies that
// malformed package entries (missing name or version, wrong types) do not
// smuggle dependency facts into the parser payload. Composer lockfiles
// produced by partially failed installs or hand edits must not leak
// half-evidence into the supply chain reducer.
func TestParseComposerLockMalformedReturnsNoDependencyRows(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "composer.lock", `{
  "_readme": ["malformed"],
  "packages": [
    {"name": "", "version": "1.0.0"},
    {"name": "vendor/no-version"},
    {"version": "9.9.9"},
    "not-an-object",
    {"name": "vendor/empty-version", "version": ""},
    {"name": "vendor/keeps", "version": "3.1.4"}
  ],
  "packages-dev": "not-an-array"
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, _ := payload["variables"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want exactly the one well-formed package", rows)
	}
	row := rows[0]
	if row["name"] != "vendor/keeps" {
		t.Fatalf("name = %#v, want vendor/keeps", row["name"])
	}
	if row["value"] != "3.1.4" {
		t.Fatalf("value = %#v, want 3.1.4", row["value"])
	}
}

// TestParseComposerLockEmptyArraysProduceEmptyRows guards the edge case
// where a lockfile is structurally valid but installs no packages — for
// example after `composer install` on an empty manifest. The parser must
// not error or invent rows.
func TestParseComposerLockEmptyArraysProduceEmptyRows(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "composer.lock", `{
  "_readme": ["empty"],
  "packages": [],
  "packages-dev": []
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want zero rows for empty arrays", rows)
	}
}

// TestParseComposerManifestAndLockfilePreserveBothEvidence drives the
// acceptance criterion that the manifest range and lockfile exact version
// are joined without losing either piece of evidence. The Composer parser
// emits range rows from composer.json and exact rows from composer.lock so
// downstream reducer code can present both signals to the operator.
func TestParseComposerManifestAndLockfilePreserveBothEvidence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	manifestPath := filepath.Join(dir, "composer.json")
	mustWriteFile(t, manifestPath, `{
  "name": "demo/app",
  "require": {"monolog/monolog": "^2.0"},
  "require-dev": {"phpunit/phpunit": "^9.0"}
}`)
	lockfilePath := filepath.Join(dir, "composer.lock")
	mustWriteFile(t, lockfilePath, `{
  "packages": [
    {"name": "monolog/monolog", "version": "2.9.1"}
  ],
  "packages-dev": [
    {"name": "phpunit/phpunit", "version": "9.6.13"}
  ]
}`)

	manifestPayload, err := Parse(manifestPath, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse(manifest) error = %v", err)
	}
	lockPayload, err := Parse(lockfilePath, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse(lockfile) error = %v", err)
	}

	manifestRows := dependencyRowsByName(rowsOrNil(manifestPayload))
	lockRows := dependencyRowsByName(rowsOrNil(lockPayload))

	manifestRuntime, ok := manifestRows["monolog/monolog"]
	if !ok {
		t.Fatalf("manifest missing monolog/monolog: %#v", manifestRows)
	}
	if manifestRuntime["value"] != "^2.0" {
		t.Fatalf("manifest range = %#v, want %q", manifestRuntime["value"], "^2.0")
	}
	if manifestRuntime["section"] != "require" {
		t.Fatalf("manifest section = %#v, want require", manifestRuntime["section"])
	}
	if _, present := manifestRuntime["lockfile"]; present {
		t.Fatalf("manifest row should not carry a lockfile flag: %#v", manifestRuntime)
	}

	lockRuntime, ok := lockRows["monolog/monolog"]
	if !ok {
		t.Fatalf("lockfile missing monolog/monolog: %#v", lockRows)
	}
	if lockRuntime["value"] != "2.9.1" {
		t.Fatalf("lockfile value = %#v, want exact %q", lockRuntime["value"], "2.9.1")
	}
	if lockRuntime["lockfile"] != true {
		t.Fatalf("lockfile flag = %#v, want true", lockRuntime["lockfile"])
	}
	if lockRuntime["section"] != "packages" {
		t.Fatalf("lockfile section = %#v, want packages", lockRuntime["section"])
	}

	manifestDev, ok := manifestRows["phpunit/phpunit"]
	if !ok || manifestDev["section"] != "require-dev" {
		t.Fatalf("manifest dev row missing or wrong section: %#v", manifestDev)
	}
	lockDev, ok := lockRows["phpunit/phpunit"]
	if !ok || lockDev["section"] != "packages-dev" {
		t.Fatalf("lockfile dev row missing or wrong section: %#v", lockDev)
	}
	if !reflect.DeepEqual([]string{lockDev["value"].(string), lockRuntime["value"].(string)}, []string{"9.6.13", "2.9.1"}) {
		t.Fatalf("lockfile exact versions = %#v, want phpunit 9.6.13 and monolog 2.9.1", []string{lockDev["value"].(string), lockRuntime["value"].(string)})
	}
}

func TestParseComposerLockEmitsDependencyChainAndDevScope(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "composer.lock", `{
  "packages": [
    {
      "name": "symfony/http-kernel",
      "version": "6.4.1",
      "require": {
        "php": ">=8.1",
        "symfony/event-dispatcher": "^6.4"
      }
    },
    {
      "name": "symfony/event-dispatcher",
      "version": "6.4.0"
    }
  ],
  "packages-dev": [
    {
      "name": "phpunit/phpunit",
      "version": "10.5.0",
      "require": {
        "sebastian/version": "^4.0"
      }
    },
    {
      "name": "sebastian/version",
      "version": "4.0.1"
    }
  ]
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := dependencyRowsByName(rowsOrNil(payload))
	dispatcher := rows["symfony/event-dispatcher"]
	if got, want := dispatcher["dependency_path"], []string{"symfony/http-kernel", "symfony/event-dispatcher"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("symfony/event-dispatcher dependency_path = %#v, want %#v", got, want)
	}
	if got, want := dispatcher["dependency_depth"], 2; got != want {
		t.Fatalf("symfony/event-dispatcher dependency_depth = %#v, want %#v", got, want)
	}
	if got, want := dispatcher["direct_dependency"], false; got != want {
		t.Fatalf("symfony/event-dispatcher direct_dependency = %#v, want %#v", got, want)
	}
	if got, want := rows["symfony/http-kernel"]["direct_dependency"], true; got != want {
		t.Fatalf("symfony/http-kernel direct_dependency = %#v, want %#v", got, want)
	}
	if got, want := rows["symfony/http-kernel"]["dependency_scope"], "runtime"; got != want {
		t.Fatalf("symfony/http-kernel dependency_scope = %#v, want %#v", got, want)
	}

	sebastian := rows["sebastian/version"]
	if got, want := sebastian["dependency_scope"], "dev"; got != want {
		t.Fatalf("sebastian/version dependency_scope = %#v, want %#v", got, want)
	}
	if got, want := sebastian["development_dependency"], true; got != want {
		t.Fatalf("sebastian/version development_dependency = %#v, want %#v", got, want)
	}
	if got, want := sebastian["dependency_path"], []string{"phpunit/phpunit", "sebastian/version"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sebastian/version dependency_path = %#v, want %#v", got, want)
	}
}

func TestParseComposerLockOmitsUnprovenDependencyChain(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "composer.lock", `{
  "packages": [
    {
      "name": "vendor/a",
      "version": "1.0.0",
      "require": {"vendor/b": "^1.0"}
    },
    {
      "name": "vendor/b",
      "version": "1.0.0",
      "require": {"vendor/a": "^1.0"}
    }
  ]
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := dependencyRowsByName(rowsOrNil(payload))
	for _, name := range []string{"vendor/a", "vendor/b"} {
		row := rows[name]
		if _, ok := row["dependency_path"]; ok {
			t.Fatalf("%s dependency_path = %#v, want omitted for unrooted lock graph", name, row["dependency_path"])
		}
		if _, ok := row["dependency_depth"]; ok {
			t.Fatalf("%s dependency_depth = %#v, want omitted for unrooted lock graph", name, row["dependency_depth"])
		}
		if _, ok := row["direct_dependency"]; ok {
			t.Fatalf("%s direct_dependency = %#v, want omitted for unrooted lock graph", name, row["direct_dependency"])
		}
	}
}

func rowsOrNil(payload map[string]any) []map[string]any {
	rows, _ := payload["variables"].([]map[string]any)
	return rows
}

func mustWriteFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
