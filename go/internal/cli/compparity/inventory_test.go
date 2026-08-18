// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compparity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/competitiveparity"
)

// repoRoot is the repository root relative to this package directory
// (go/internal/cli/compparity). The doc paths and the dogfood fixture path
// inside Inventory and exerciseResults are joined onto it.
const repoRoot = "../../../.."

func TestExerciseResultsAllPassAgainstRealRepoRoot(t *testing.T) {
	results := exerciseResults(repoRoot)
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
	results := exerciseResults(dir)
	byID := map[string]competitiveparity.ExerciseResult{}
	for _, result := range results {
		byID[result.ID] = result
	}
	// The dogfood fixture is read from repoRoot, so a bare temp dir makes it
	// fail with an os error carrying the absolute temp path. That is the
	// failure whose detail must come back redacted.
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

// TestExerciseFailureDetailIsStaticPerID pins the share-safe detail strings
// directly. Before the first-run exercise moved in here, the redaction test
// above could force any exercise to fail by injecting a failing func; now
// only the two repoRoot-reading exercises can be failed from a test, so the
// mapping is asserted head-on instead of through whichever exercise happens
// to be breakable.
func TestExerciseFailureDetailIsStaticPerID(t *testing.T) {
	want := map[string]string{
		"first_run_report_artifact":              "first-run evidence exercise failed",
		"operator_digest_artifact":               "operator digest artifact exercise failed",
		"investigation_evidence_packet_artifact": "investigation evidence packet exercise failed",
		"evidence_packet_dogfood_fixture":        "dogfood fixture unavailable",
		"capability_catalog_artifacts":           "capability catalog artifact exercise failed",
	}
	for _, result := range exerciseResults(repoRoot) {
		detail, ok := want[result.ID]
		if !ok {
			t.Errorf("exercise %q has no pinned failure detail; add one to exerciseFailureDetail and to this table", result.ID)
			continue
		}
		if got := exerciseFailureDetail(result.ID); got != detail {
			t.Errorf("exerciseFailureDetail(%q) = %q, want %q", result.ID, got, detail)
		}
		delete(want, result.ID)
	}
	for id := range want {
		t.Errorf("pinned detail for %q has no exercise; exerciseResults no longer runs it", id)
	}
	if got := exerciseFailureDetail("not_an_exercise"); got != "exercise failed" {
		t.Errorf("exerciseFailureDetail(unknown) = %q, want %q", got, "exercise failed")
	}
}

func TestInventoryCollectsCommandsSurfacesAndDocs(t *testing.T) {
	commands := []string{"competitive-parity", "competitive-parity validate"}
	inv, err := Inventory(repoRoot, commands)
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
	inv, err := Inventory(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Inventory error = %v", err)
	}
	if len(inv.Docs) != 0 {
		t.Fatalf("inv.Docs = %#v, want empty for a bare temp repo root", inv.Docs)
	}
}

func TestInventoryReportsUnreadableDoc(t *testing.T) {
	paths := docPaths()
	if len(paths) == 0 {
		t.Fatal("docPaths() is empty")
	}
	root := t.TempDir()
	// A directory at a doc path makes os.ReadFile fail with a non-NotExist
	// error, which must surface instead of being skipped.
	if err := os.MkdirAll(filepath.Join(root, paths[0]), 0o750); err != nil {
		t.Fatalf("mkdir doc path: %v", err)
	}
	_, err := Inventory(root, nil)
	if err == nil {
		t.Fatal("Inventory error = nil, want read failure for a directory doc path")
	}
	if !strings.Contains(err.Error(), "read parity doc "+paths[0]) {
		t.Fatalf("Inventory error = %v, want it to name %s", err, paths[0])
	}
}

func TestDocPathsSortedAndUnique(t *testing.T) {
	paths := docPaths()
	if len(paths) == 0 {
		t.Fatal("docPaths() is empty")
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("docPaths() not sorted: %#v", paths)
	}
	seen := map[string]struct{}{}
	for _, path := range paths {
		if _, dup := seen[path]; dup {
			t.Fatalf("docPaths() has duplicate %q", path)
		}
		seen[path] = struct{}{}
	}
}

func TestArtifactRendersJSONAndMarkdown(t *testing.T) {
	inv, err := Inventory(repoRoot, []string{"competitive-parity validate"})
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
