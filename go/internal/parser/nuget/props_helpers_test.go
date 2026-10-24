// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// parseProps writes body under a temporary repository as fileName and parses
// it through the real engine, so registry dispatch is part of every case.
func parseProps(t *testing.T, fileName string, body string) map[string]any {
	t.Helper()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "src", fileName)
	writeTestFile(t, path, body)
	return parsertest.MustParsePath(t, repoRoot, path)
}

// parsePropsError parses body through the default engine and returns the error.
func parsePropsError(t *testing.T, fileName string, body string) error {
	t.Helper()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, fileName)
	writeTestFile(t, path, body)
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	_, err = engine.ParsePath(repoRoot, path, false, parser.Options{})
	return err
}

// variableRows returns the variables bucket as rows.
func variableRows(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	return rows
}

// rowsInSection returns the rows whose section equals section, in emission order.
func rowsInSection(t *testing.T, payload map[string]any, section string) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, row := range variableRows(t, payload) {
		if row["section"] == section {
			out = append(out, row)
		}
	}
	return out
}

// onlyRow requires exactly one row named name in section.
func onlyRow(t *testing.T, payload map[string]any, section string, name string) map[string]any {
	t.Helper()

	var found []map[string]any
	for _, row := range rowsInSection(t, payload, section) {
		if row["name"] == name {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("rows named %q in %s = %d, want 1 (rows: %#v)", name, section, len(found), variableRows(t, payload))
	}
	return found[0]
}

// limitCount reads one counter from the msbuild_props_limits bucket.
func limitCount(t *testing.T, payload map[string]any, key string) int {
	t.Helper()

	limits, ok := payload["msbuild_props_limits"].(map[string]any)
	if !ok {
		t.Fatalf("msbuild_props_limits = %T, want map[string]any", payload["msbuild_props_limits"])
	}
	count, ok := limits[key].(int)
	if !ok {
		t.Fatalf("msbuild_props_limits[%q] = %T(%v), want int", key, limits[key], limits[key])
	}
	return count
}

// limitFlag reads one boolean from the msbuild_props_limits bucket.
func limitFlag(t *testing.T, payload map[string]any, key string) bool {
	t.Helper()

	limits, ok := payload["msbuild_props_limits"].(map[string]any)
	if !ok {
		t.Fatalf("msbuild_props_limits = %T, want map[string]any", payload["msbuild_props_limits"])
	}
	flag, ok := limits[key].(bool)
	if !ok {
		t.Fatalf("msbuild_props_limits[%q] = %T(%v), want bool", key, limits[key], limits[key])
	}
	return flag
}

// hasKey reports whether row carries key at all.
func hasKey(row map[string]any, key string) bool {
	_, ok := row[key]
	return ok
}
