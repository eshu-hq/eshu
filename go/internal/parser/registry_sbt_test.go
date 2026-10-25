// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultRegistryAdmitsBuildSbtAsStructuredManifest pins #8938: build.sbt
// is the Scala manifest named by the language ledger and must bind to the
// sbt manifest parser by exact name, in any directory, at the structured
// level. Other .sbt files are not admitted.
func TestDefaultRegistryAdmitsBuildSbtAsStructuredManifest(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, path := range []string{"build.sbt", filepath.Join("modules", "api", "build.sbt")} {
		definition, ok := registry.LookupByPath(path)
		if !ok {
			t.Fatalf("LookupByPath(%q) not admitted", path)
		}
		if definition.ParserKey != "sbt" || definition.Language != "sbt" {
			t.Fatalf("LookupByPath(%q) = %q/%q, want sbt/sbt", path, definition.ParserKey, definition.Language)
		}
		if definition.Admission != AdmissionStructuredFact {
			t.Fatalf("LookupByPath(%q) admission = %v, want structured fact", path, definition.Admission)
		}
		if definition.Ecosystem != "scala" {
			t.Fatalf("LookupByPath(%q) ecosystem = %q, want scala", path, definition.Ecosystem)
		}
	}
	for _, path := range []string{"plugins.sbt", filepath.Join("project", "plugins.sbt"), "build.sbt.bak", "build.scala"} {
		if definition, ok := registry.LookupByPath(path); ok && definition.ParserKey == "sbt" {
			t.Fatalf("LookupByPath(%q) bound to the sbt parser; only build.sbt is admitted", path)
		}
	}
}

// TestEngineParsesBuildSbtDependencies proves the engine dispatches build.sbt
// to the sbt parser and emits the shared dependency row contract.
func TestEngineParsesBuildSbtDependencies(t *testing.T) {
	t.Parallel()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "build.sbt")
	body := "scalaVersion := \"2.13.12\"\nlibraryDependencies += \"org.typelevel\" %% \"cats-core\" % \"2.10.0\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture error = %v", err)
	}
	payload, err := engine.ParsePath(dir, path, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	row := dependencyConfigKindRowsByName(rows)["org.typelevel:cats-core"]
	if row == nil {
		t.Fatalf("variables = %#v, want org.typelevel:cats-core", payload["variables"])
	}
	if row["package_manager"] != "sbt" || row["value"] != "2.10.0" {
		t.Fatalf("row = %#v, want package_manager sbt and value 2.10.0", row)
	}
	state, _ := payload["sbt_state"].(map[string]any)
	if state["scala_version"] != "2.13.12" {
		t.Fatalf("sbt_state = %#v, want scala_version 2.13.12", payload["sbt_state"])
	}
}
