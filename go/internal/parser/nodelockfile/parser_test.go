// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nodelockfile

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseEmitsDeterministicRowOrder asserts that two parser runs over the
// same fixture return identical row slices, so downstream fact dedupe and
// reducer ordering are stable.
func TestParseEmitsDeterministicRowOrder(t *testing.T) {
	t.Parallel()

	body := `lockfileVersion: '6.0'

importers:
  .:
    dependencies:
      lodash:
        specifier: ^4.17.21
        version: 4.17.21
      vite:
        specifier: ^5.0.0
        version: 5.0.0

packages:

  /lodash@4.17.21:
    resolution: {integrity: sha512-AbCdEf==}
  /vite@5.0.0:
    resolution: {integrity: sha512-V==}
`
	path := writeTestFile(t, "pnpm-lock.yaml", body)

	first := parseAndExtractRows(t, path)
	second := parseAndExtractRows(t, path)
	firstNames := dependencyRowNames(first)
	secondNames := dependencyRowNames(second)
	if !reflect.DeepEqual(firstNames, secondNames) {
		t.Fatalf("row order is unstable: first=%v second=%v", firstNames, secondNames)
	}
	if !sort.StringsAreSorted(firstNames) {
		t.Fatalf("dependency rows must be sorted by name for stable joins; got %v", firstNames)
	}
}

func writeTestFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func parseAndExtractRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", path, err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	return rows
}

func rowsByName(rows []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		if name != "" {
			out[name] = row
		}
	}
	return out
}

func dependencyRowNames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if row["config_kind"] != "dependency" {
			continue
		}
		name, _ := row["name"].(string)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func assertChain(t *testing.T, row map[string]any, wantPath []string, wantDepth int, wantDirect bool) {
	t.Helper()
	if row == nil {
		t.Fatalf("dependency row missing")
	}
	got, ok := row["dependency_path"].([]string)
	if !ok {
		t.Fatalf("dependency_path = %T %#v, want []string", row["dependency_path"], row["dependency_path"])
	}
	if !reflect.DeepEqual(got, wantPath) {
		t.Fatalf("dependency_path = %#v, want %#v", got, wantPath)
	}
	if depth := row["dependency_depth"]; depth != wantDepth {
		t.Fatalf("dependency_depth = %#v, want %d", depth, wantDepth)
	}
	if direct := row["direct_dependency"]; direct != wantDirect {
		t.Fatalf("direct_dependency = %#v, want %v", direct, wantDirect)
	}
}
