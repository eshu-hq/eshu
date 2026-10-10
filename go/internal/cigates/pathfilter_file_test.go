// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestDornyFiltersFile(t *testing.T) {
	root := t.TempDir()
	workflows := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflows, 0o755); err != nil {
		t.Fatal(err)
	}
	filterPath := filepath.Join(root, ".github", "filters.yml")
	if err := os.WriteFile(filterPath, []byte("evidence:\n  - 'specs/**'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(workflows, "static.yml")
	writeWorkflow := func(ref string) {
		t.Helper()
		raw := "jobs:\n  changes:\n    steps:\n      - uses: dorny/paths-filter@v3\n        with:\n          filters: " + ref + "\n"
		if err := os.WriteFile(workflow, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeWorkflow(".github/filters.yml")
	filters, every, err := cigates.DornyFiltersFile(workflow)
	if err != nil || every || len(filters["evidence"]) != 1 || filters["evidence"][0] != "specs/**" {
		t.Fatalf("filters=%v every=%v err=%v", filters, every, err)
	}
	if err := os.WriteFile(filterPath, []byte("evidence: [specs/**]\nevidence: [docs/**]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cigates.DornyFiltersFile(workflow); err == nil {
		t.Fatal("duplicate filter key accepted")
	}
	if err := os.Remove(filterPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cigates.DornyFiltersFile(workflow); err == nil {
		t.Fatal("missing filter file accepted")
	}
	writeWorkflow("../../outside.yml")
	if _, _, err := cigates.DornyFiltersFile(workflow); err == nil {
		t.Fatal("escaping filter file accepted")
	}
}
