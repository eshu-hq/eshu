// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	jsonparser "github.com/eshu-hq/eshu/go/internal/parser/json"
)

func TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	rootManifest := filepath.Join(repoRoot, "Cargo.toml")
	memberManifest := filepath.Join(repoRoot, "crates", "api", "Cargo.toml")
	writeTestFile(t, rootManifest, `
[workspace]
members = ["crates/api"]

[workspace.dependencies]
serde = { version = "1.0.203", features = ["derive"] }
`)
	writeTestFile(t, memberManifest, `
[package]
name = "api"
version = "0.1.0"

[dependencies]
tokio = "1.37"
serde.workspace = true
json = { package = "serde_json", version = "1.0" }

[dev-dependencies]
proptest = "1"

[build-dependencies]
cc = "1"

[target.'cfg(unix)'.dependencies]
libc = "0.2"
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, memberManifest, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(Cargo.toml) error = %v, want nil", err)
	}

	rows := cargoDependencyRowsByName(t, payload)
	assertCargoDependencyRow(t, rows["tokio"], "tokio", "1.37", "dependencies", "runtime")
	assertCargoDependencyRow(t, rows["serde"], "serde", "1.0.203", "dependencies", "runtime")
	assertCargoDependencyRow(t, rows["serde_json"], "serde_json", "1.0", "dependencies", "runtime")
	assertCargoDependencyRow(t, rows["proptest"], "proptest", "1", "dev-dependencies", "dev")
	assertCargoDependencyRow(t, rows["cc"], "cc", "1", "build-dependencies", "build")
	assertCargoDependencyRow(t, rows["libc"], "libc", "0.2", "target.cfg(unix).dependencies", "runtime")

	if got, want := rows["serde"]["workspace_dependency"], true; got != want {
		t.Fatalf("serde workspace_dependency = %#v, want %#v", got, want)
	}
	if got, want := rows["serde_json"]["dependency_alias"], "json"; got != want {
		t.Fatalf("serde_json dependency_alias = %#v, want %#v", got, want)
	}
	if got, want := rows["serde_json"]["manifest_name"], "json"; got != want {
		t.Fatalf("serde_json manifest_name = %#v, want %#v", got, want)
	}
	if got, want := rows["libc"]["target_cfg"], "cfg(unix)"; got != want {
		t.Fatalf("libc target_cfg = %#v, want %#v", got, want)
	}
}

func TestDefaultEngineParsePathCargoLockEmitsExactVersionsAndProvenChains(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	lockfile := filepath.Join(repoRoot, "Cargo.lock")
	writeTestFile(t, lockfile, `
version = 3

[[package]]
name = "api"
version = "0.1.0"
dependencies = [
 "serde",
]

[[package]]
name = "serde"
version = "1.0.203"
source = "registry+https://github.com/rust-lang/crates.io-index"
dependencies = [
 "serde_derive",
]

[[package]]
name = "serde_derive"
version = "1.0.203"
source = "registry+https://github.com/rust-lang/crates.io-index"
dependencies = [
 "proc-macro2",
]

[[package]]
name = "proc-macro2"
version = "1.0.82"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "unreachable"
version = "0.1.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, lockfile, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(Cargo.lock) error = %v, want nil", err)
	}

	rows := cargoDependencyRowsByName(t, payload)
	assertCargoDependencyRow(t, rows["serde"], "serde", "1.0.203", "cargo-lock", "runtime")
	assertCargoDependencyChain(t, rows["serde"], []string{"serde"}, 1, true)
	assertCargoDependencyRow(t, rows["serde_derive"], "serde_derive", "1.0.203", "cargo-lock", "runtime")
	assertCargoDependencyChain(t, rows["serde_derive"], []string{"serde", "serde_derive"}, 2, false)
	assertCargoDependencyRow(t, rows["proc-macro2"], "proc-macro2", "1.0.82", "cargo-lock", "runtime")
	assertCargoDependencyChain(t, rows["proc-macro2"], []string{"serde", "serde_derive", "proc-macro2"}, 3, false)
	assertCargoDependencyRow(t, rows["unreachable"], "unreachable", "0.1.0", "cargo-lock", "runtime")
	if _, ok := rows["unreachable"]["dependency_path"]; ok {
		t.Fatalf("unreachable dependency_path = %#v, want no transitive path without root proof", rows["unreachable"]["dependency_path"])
	}
}

func TestDefaultEngineParsePathCargoLockUsesSourceQualifiedDependencies(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	lockfile := filepath.Join(repoRoot, "Cargo.lock")
	writeTestFile(t, lockfile, `
version = 3

[[package]]
name = "api"
version = "0.1.0"
dependencies = [
 "shared 1.0.0 (git+https://example.test/shared)",
]

[[package]]
name = "shared"
version = "1.0.0"
source = "git+https://example.test/shared"
dependencies = [
 "git_leaf",
]

[[package]]
name = "shared"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
dependencies = [
 "registry_leaf",
]

[[package]]
name = "git_leaf"
version = "0.1.0"
source = "git+https://example.test/shared"

[[package]]
name = "registry_leaf"
version = "0.1.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, lockfile, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(Cargo.lock) error = %v, want nil", err)
	}

	gitShared := cargoDependencyRowByNameAndSource(t, payload, "shared", "git+https://example.test/shared")
	assertCargoDependencyChain(t, gitShared, []string{"shared"}, 1, true)
	registryShared := cargoDependencyRowByNameAndSource(t, payload, "shared", "registry+https://github.com/rust-lang/crates.io-index")
	if _, ok := registryShared["dependency_path"]; ok {
		t.Fatalf("registry shared dependency_path = %#v, want no path when root requested git source", registryShared["dependency_path"])
	}
	gitLeaf := cargoDependencyRowByNameAndSource(t, payload, "git_leaf", "git+https://example.test/shared")
	assertCargoDependencyChain(t, gitLeaf, []string{"shared", "git_leaf"}, 2, false)
	registryLeaf := cargoDependencyRowByNameAndSource(t, payload, "registry_leaf", "registry+https://github.com/rust-lang/crates.io-index")
	if _, ok := registryLeaf["dependency_path"]; ok {
		t.Fatalf("registry_leaf dependency_path = %#v, want no path without source-qualified reachability proof", registryLeaf["dependency_path"])
	}
}

func TestDefaultEngineParsePathCargoRejectsMalformedDependencyFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	tests := []struct {
		name string
		file string
		body string
	}{
		{
			name: "manifest",
			file: "Cargo.toml",
			body: "[dependencies\nserde = \"1\"",
		},
		{
			name: "lockfile",
			file: "Cargo.lock",
			body: "[[package]]\nname = \"serde\"\nversion = ",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(repoRoot, tt.name, tt.file)
			writeTestFile(t, path, tt.body)
			engine, engineErr := DefaultEngine()
			if engineErr != nil {
				t.Fatalf("DefaultEngine() error = %v, want nil", engineErr)
			}
			_, err := engine.ParsePath(repoRoot, path, false, Options{})
			if err == nil {
				t.Fatalf("ParsePath(%s) error = nil, want malformed Cargo dependency file error", tt.file)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "cargo") {
				t.Fatalf("ParsePath(%s) error = %v, want Cargo-specific error", tt.file, err)
			}
		})
	}
}

func TestCargoDependencyCoverageMatrixMarksCargoFilesCovered(t *testing.T) {
	t.Parallel()

	for _, file := range []string{"cargo.toml", "cargo.lock"} {
		entry, ok := jsonparser.DependencyCoverageByFile(file)
		if !ok {
			t.Fatalf("DependencyCoverageByFile(%q) missing", file)
		}
		if got, want := entry.Ecosystem, "cargo"; got != want {
			t.Fatalf("%s ecosystem = %q, want %q", file, got, want)
		}
		if got, want := entry.Status, jsonparser.DependencyCoverageCovered; got != want {
			t.Fatalf("%s status = %q, want %q", file, got, want)
		}
	}
}

func cargoDependencyRowsByName(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		if row["config_kind"] != "dependency" {
			continue
		}
		if row["package_manager"] != "cargo" {
			continue
		}
		name, _ := row["name"].(string)
		if name != "" {
			out[name] = row
		}
	}
	return out
}

func cargoDependencyRowByNameAndSource(t *testing.T, payload map[string]any, name string, source string) map[string]any {
	t.Helper()

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	for _, row := range rows {
		if row["config_kind"] == "dependency" &&
			row["package_manager"] == "cargo" &&
			row["name"] == name &&
			row["package_source"] == source {
			return row
		}
	}
	t.Fatalf("dependency row name=%q source=%q missing in %#v", name, source, rows)
	return nil
}

func assertCargoDependencyRow(
	t *testing.T,
	row map[string]any,
	name string,
	value string,
	section string,
	scope string,
) {
	t.Helper()

	if row == nil {
		t.Fatalf("dependency row %q missing", name)
	}
	if got := row["name"]; got != name {
		t.Fatalf("%s name = %#v, want %#v", name, got, name)
	}
	if got := row["value"]; got != value {
		t.Fatalf("%s value = %#v, want %#v", name, got, value)
	}
	if got := row["section"]; got != section {
		t.Fatalf("%s section = %#v, want %#v", name, got, section)
	}
	if got := row["dependency_scope"]; got != scope {
		t.Fatalf("%s dependency_scope = %#v, want %#v", name, got, scope)
	}
	if got := row["config_kind"]; got != "dependency" {
		t.Fatalf("%s config_kind = %#v, want dependency", name, got)
	}
	if got := row["package_manager"]; got != "cargo" {
		t.Fatalf("%s package_manager = %#v, want cargo", name, got)
	}
	if _, ok := row["source_path"].(string); !ok {
		t.Fatalf("%s source_path = %T, want string", name, row["source_path"])
	}
}

func assertCargoDependencyChain(
	t *testing.T,
	row map[string]any,
	wantPath []string,
	wantDepth int,
	wantDirect bool,
) {
	t.Helper()

	if row == nil {
		t.Fatalf("dependency row missing")
	}
	gotPath, ok := row["dependency_path"].([]string)
	if !ok {
		t.Fatalf("dependency_path = %T %#v, want []string", row["dependency_path"], row["dependency_path"])
	}
	if !reflect.DeepEqual(gotPath, wantPath) {
		t.Fatalf("dependency_path = %#v, want %#v", gotPath, wantPath)
	}
	if got := row["dependency_depth"]; got != wantDepth {
		t.Fatalf("dependency_depth = %#v, want %d", got, wantDepth)
	}
	if got := row["direct_dependency"]; got != wantDirect {
		t.Fatalf("direct_dependency = %#v, want %v", got, wantDirect)
	}
}
