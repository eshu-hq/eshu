// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compparity

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/competitiveparity"
)

// repoRoot is the repository root relative to this package directory
// (go/internal/cli/compparity). The doc paths and the dogfood fixture path
// inside Inventory and ExerciseResults are joined onto it.
const repoRoot = "../../../.."

func passingFirstRun() error { return nil }

func TestExerciseResultsAllPassAgainstRealRepoRoot(t *testing.T) {
	results := ExerciseResults(repoRoot, passingFirstRun)
	wantIDs := []string{
		"first_run_report_artifact",
		"operator_digest_artifact",
		"investigation_evidence_packet_artifact",
		"evidence_packet_dogfood_fixture",
		"capability_catalog_artifacts",
	}
	if len(results) != len(wantIDs) {
		t.Fatalf("len(results) = %d, want %d: %#v", len(results), len(wantIDs), results)
	}
	for i, result := range results {
		if result.ID != wantIDs[i] {
			t.Errorf("results[%d].ID = %q, want %q", i, result.ID, wantIDs[i])
		}
		if !result.OK {
			t.Errorf("results[%d] (%s) OK = false, detail %q", i, result.ID, result.Detail)
		}
		if result.Detail != "exercised" {
			t.Errorf("results[%d] (%s) Detail = %q, want %q", i, result.ID, result.Detail, "exercised")
		}
	}
}

func TestExerciseResultsRedactFailureDetails(t *testing.T) {
	dir := t.TempDir()
	leak := "boom from " + dir
	results := ExerciseResults(dir, func() error { return errors.New(leak) })
	byID := map[string]competitiveparity.ExerciseResult{}
	for _, result := range results {
		byID[result.ID] = result
	}
	firstRun, ok := byID["first_run_report_artifact"]
	if !ok || firstRun.OK {
		t.Fatalf("first_run_report_artifact = %#v, want a failed result", firstRun)
	}
	if firstRun.Detail != "first-run evidence exercise failed" {
		t.Fatalf("first-run Detail = %q", firstRun.Detail)
	}
	dogfood, ok := byID["evidence_packet_dogfood_fixture"]
	if !ok || dogfood.OK {
		t.Fatalf("evidence_packet_dogfood_fixture = %#v, want a failed result", dogfood)
	}
	if dogfood.Detail != "dogfood fixture unavailable" {
		t.Fatalf("dogfood Detail = %q", dogfood.Detail)
	}
	for _, result := range results {
		if strings.Contains(result.Detail, dir) {
			t.Fatalf("result %s leaked temp dir in detail %q", result.ID, result.Detail)
		}
	}
}

func TestExerciseResultsNilFirstRunExerciseFails(t *testing.T) {
	results := ExerciseResults(repoRoot, nil)
	for _, result := range results {
		if result.ID != "first_run_report_artifact" {
			continue
		}
		if result.OK {
			t.Fatal("first_run_report_artifact OK = true with a nil exercise, want failure")
		}
		if result.Detail != "first-run evidence exercise failed" {
			t.Fatalf("Detail = %q", result.Detail)
		}
		return
	}
	t.Fatal("results missing first_run_report_artifact")
}

func TestInventoryCollectsCommandsSurfacesAndDocs(t *testing.T) {
	commands := []string{"competitive-parity", "competitive-parity validate"}
	inv, err := Inventory(repoRoot, commands, passingFirstRun)
	if err != nil {
		t.Fatalf("Inventory error = %v", err)
	}
	if len(inv.Commands) != len(commands) || inv.Commands[0] != commands[0] || inv.Commands[1] != commands[1] {
		t.Fatalf("inv.Commands = %#v, want %#v", inv.Commands, commands)
	}
	if len(inv.APIRoutes) == 0 || len(inv.MCPTools) == 0 || len(inv.ConsolePages) == 0 {
		t.Fatalf("inventory missing surfaces: api=%d mcp=%d console=%d",
			len(inv.APIRoutes), len(inv.MCPTools), len(inv.ConsolePages))
	}
	for name, list := range map[string][]string{
		"APIRoutes": inv.APIRoutes, "MCPTools": inv.MCPTools, "ConsolePages": inv.ConsolePages,
	} {
		if !sort.StringsAreSorted(list) {
			t.Errorf("inv.%s is not sorted: %#v", name, list)
		}
	}
	if len(inv.Docs) == 0 {
		t.Fatal("inv.Docs is empty, want the committed parity docs")
	}
	for path, content := range inv.Docs {
		if strings.TrimSpace(content) == "" {
			t.Errorf("inv.Docs[%q] is empty", path)
		}
	}
	if len(inv.Exercises) == 0 {
		t.Fatal("inv.Exercises is empty")
	}
}

func TestInventorySkipsMissingDocsWithoutError(t *testing.T) {
	inv, err := Inventory(t.TempDir(), nil, passingFirstRun)
	if err != nil {
		t.Fatalf("Inventory error = %v", err)
	}
	if len(inv.Docs) != 0 {
		t.Fatalf("inv.Docs = %#v, want empty for a bare temp repo root", inv.Docs)
	}
}

func TestInventoryReportsUnreadableDoc(t *testing.T) {
	paths := DocPaths()
	if len(paths) == 0 {
		t.Fatal("DocPaths() is empty")
	}
	root := t.TempDir()
	// A directory at a doc path makes os.ReadFile fail with a non-NotExist
	// error, which must surface instead of being skipped.
	if err := os.MkdirAll(filepath.Join(root, paths[0]), 0o750); err != nil {
		t.Fatalf("mkdir doc path: %v", err)
	}
	_, err := Inventory(root, nil, passingFirstRun)
	if err == nil {
		t.Fatal("Inventory error = nil, want read failure for a directory doc path")
	}
	if !strings.Contains(err.Error(), "read parity doc "+paths[0]) {
		t.Fatalf("Inventory error = %v, want it to name %s", err, paths[0])
	}
}

func TestDocPathsSortedAndUnique(t *testing.T) {
	paths := DocPaths()
	if len(paths) == 0 {
		t.Fatal("DocPaths() is empty")
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("DocPaths() not sorted: %#v", paths)
	}
	seen := map[string]struct{}{}
	for _, path := range paths {
		if _, dup := seen[path]; dup {
			t.Fatalf("DocPaths() has duplicate %q", path)
		}
		seen[path] = struct{}{}
	}
}

func TestArtifactRendersJSONAndMarkdown(t *testing.T) {
	inv, err := Inventory(repoRoot, []string{"competitive-parity validate"}, passingFirstRun)
	if err != nil {
		t.Fatalf("Inventory error = %v", err)
	}
	report := competitiveparity.Validate(inv, competitiveparity.DefaultExpectations())

	jsonArtifact, err := Artifact(report, true)
	if err != nil {
		t.Fatalf("Artifact(json) error = %v", err)
	}
	var decoded competitiveparity.Report
	if err := json.Unmarshal(jsonArtifact, &decoded); err != nil {
		t.Fatalf("decode JSON artifact: %v\n%s", err, jsonArtifact)
	}
	if decoded.SchemaVersion != competitiveparity.SchemaVersion {
		t.Fatalf("decoded.SchemaVersion = %q", decoded.SchemaVersion)
	}

	markdown, err := Artifact(report, false)
	if err != nil {
		t.Fatalf("Artifact(markdown) error = %v", err)
	}
	if !bytes.Contains(markdown, []byte("# Competitive Parity Gate")) {
		t.Fatalf("markdown artifact missing heading:\n%s", markdown)
	}
}
