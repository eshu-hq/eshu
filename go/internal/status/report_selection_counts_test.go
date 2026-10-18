// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"strings"
	"testing"
)

func selectionCountsFixture() RepositorySelectionCounts {
	return RepositorySelectionCounts{
		States:          map[string]int{"selected": 777, "not_listed": 25},
		Classifications: map[string]int{"transferred": 22, "renamed": 1, "not_found_or_inaccessible": 1, "unclassified": 1},
	}
}

func TestBuildReportCarriesRepositorySelection(t *testing.T) {
	t.Parallel()

	raw := RawSnapshot{RepositorySelection: selectionCountsFixture()}
	got := BuildReport(raw, DefaultOptions()).RepositorySelection
	if len(got.States) != 2 || got.States["not_listed"] != 25 || len(got.Classifications) != 4 {
		t.Fatalf("RepositorySelection = %+v, want the fixture counts", got)
	}
	got.States["not_listed"] = 0
	if raw.RepositorySelection.States["not_listed"] != 25 {
		t.Fatal("BuildReport aliases the reader maps: later reader mutations leak into the report")
	}
}

func TestRenderJSONRepositorySelection(t *testing.T) {
	t.Parallel()

	populated, err := RenderJSON(BuildReport(RawSnapshot{RepositorySelection: selectionCountsFixture()}, DefaultOptions()))
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(populated, &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	section, ok := body["repository_selection"].(map[string]any)
	if !ok {
		t.Fatalf("repository_selection = %#v, want the counts section", body["repository_selection"])
	}
	states, ok := section["states"].(map[string]any)
	if !ok || states["not_listed"] != float64(25) || states["selected"] != float64(777) {
		t.Fatalf("states = %#v, want selected 777 and not_listed 25", section["states"])
	}
	classes, ok := section["classifications"].(map[string]any)
	if !ok || classes["transferred"] != float64(22) || classes["unclassified"] != float64(1) {
		t.Fatalf("classifications = %#v, want the verdict shares", section["classifications"])
	}

	empty, err := RenderJSON(BuildReport(RawSnapshot{}, DefaultOptions()))
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	if strings.Contains(string(empty), "repository_selection") {
		t.Fatal("empty selection evidence must stay out of the JSON surface")
	}
}

func TestRenderTextRepositorySelection(t *testing.T) {
	t.Parallel()

	text := RenderText(BuildReport(RawSnapshot{RepositorySelection: selectionCountsFixture()}, DefaultOptions()))
	if !strings.Contains(text, "Repository selection: ") || !strings.Contains(text, "not_listed") || !strings.Contains(text, "transferred") {
		t.Fatalf("text missing the selection line:\n%s", text)
	}
	if strings.Contains(RenderText(BuildReport(RawSnapshot{}, DefaultOptions())), "Repository selection:") {
		t.Fatal("empty selection evidence must stay out of the text surface")
	}
}
