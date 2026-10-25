// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRegistryAdmitsPipConstraintsFilesOnly proves the constraints names
// resolve to the requirements parser and that the family stops at .txt: a
// constraints-* policy file in YAML, JSON, or Markdown keeps its own parser
// instead of being read as pip input (#8821).
func TestRegistryAdmitsPipConstraintsFilesOnly(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, path := range []string{
		"constraints.txt",
		"deploy/Constraints.TXT",
		"constraints-dev.txt",
		"services/api/constraints-3.11.txt",
		"constraints_prod.txt",
		"a/b/CONSTRAINTS_Test.txt",
	} {
		definition, ok := registry.LookupByPath(path)
		if !ok || definition.ParserKey != "python_requirements" {
			t.Errorf("LookupByPath(%q) = %q, %v, want python_requirements", path, definition.ParserKey, ok)
		}
	}
	for path, notKey := range map[string]string{
		"policy/constraints-gatekeeper.yaml": "python_requirements",
		"policy/constraints-gatekeeper.yml":  "python_requirements",
		"constraints-notes.md":               "python_requirements",
		"constraints-schema.json":            "python_requirements",
		"constraints.yaml":                   "python_requirements",
		"constraints.txt.bak":                "python_requirements",
		"my-constraints.txt":                 "python_requirements",
		"constraints-dev.in":                 "python_requirements",
		"constraintsdev.txt":                 "python_requirements",
	} {
		if definition, ok := registry.LookupByPath(path); ok && definition.ParserKey == notKey {
			t.Errorf("LookupByPath(%q) = %s, must not be admitted as pip constraints", path, notKey)
		}
	}
}

// TestEngineParsesPipConstraintsAsPolicyRows proves the dispatch end to end: a
// constraints file reaches the requirements parser, its pins land as
// config_kind=dependency rows flagged central_version_declaration, and a
// requirements file in the same run stays unflagged.
func TestEngineParsesPipConstraintsAsPolicyRows(t *testing.T) {
	t.Parallel()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	dir := t.TempDir()
	parse := func(name, body string) []map[string]any {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
		payload, err := engine.ParsePath(dir, path, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%s) error = %v", name, err)
		}
		rows, _ := payload["variables"].([]map[string]any)
		return rows
	}

	for _, name := range []string{"constraints.txt", "constraints-dev.txt"} {
		rows := parse(name, "-c other.txt\nrequests==2.31.0\nurllib3<2\n")
		if len(rows) != 2 {
			t.Fatalf("%s: len(rows) = %d, want 2: %#v", name, len(rows), rows)
		}
		for _, row := range rows {
			if row["config_kind"] != "dependency" || row["central_version_declaration"] != true {
				t.Errorf("%s: row %#v, want config_kind=dependency with central_version_declaration=true", name, row)
			}
		}
	}
	for _, row := range parse("requirements.txt", "requests==2.31.0\n") {
		if _, flagged := row["central_version_declaration"]; flagged {
			t.Errorf("requirements.txt row %#v carries the constraints policy flag", row)
		}
	}
}
