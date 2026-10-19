// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWaitForContentSearchReadySucceedsAfterRetries(t *testing.T) {
	t.Parallel()

	calls := 0
	probe := func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("content substring indexes are not ready")
		}
		return nil
	}
	if err := waitForContentSearchReady(context.Background(), probe, time.Minute, time.Millisecond); err != nil {
		t.Fatalf("waitForContentSearchReady() = %v, want nil after the probe turns ready", err)
	}
	if calls != 3 {
		t.Fatalf("probe calls = %d, want 3 (fail fast on first success, no extra polls)", calls)
	}
}

func TestWaitForContentSearchReadyTimesOut(t *testing.T) {
	t.Parallel()

	probe := func(context.Context) error { return errors.New("content substring indexes are not ready") }
	err := waitForContentSearchReady(context.Background(), probe, 15*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("waitForContentSearchReady() = nil, want timeout naming the last probe error")
	}
	if !strings.Contains(err.Error(), "content substring indexes are not ready") {
		t.Fatalf("timeout error = %q, want it to name the last probe error", err.Error())
	}
}

// TestStoryContentRowsAreDeterministicAndShaped pins the story corpus the
// #7009 pilots read: byte-identical plans across builds, the anchor and
// related counts, and one file of every extractor-relevant kind the stages
// must meet (module .tf, conventional compose name, kustomization base,
// workflow path).
func TestStoryContentRowsAreDeterministicAndShaped(t *testing.T) {
	t.Parallel()

	first, second := BuildStoryContentRows(), BuildStoryContentRows()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("story content plan is not deterministic")
	}
	byRepo := map[string]int{}
	kinds := map[string]bool{}
	for _, row := range first {
		byRepo[row.RepoID]++
		base := row.RelativePath[strings.LastIndex(row.RelativePath, "/")+1:]
		switch {
		case strings.HasSuffix(base, ".tf"):
			kinds["tf"] = true
		case base == "docker-compose.yml":
			kinds["compose"] = true
		case base == "kustomization.yaml":
			kinds["kustomization"] = true
		case strings.HasPrefix(row.RelativePath, ".github/workflows/"):
			kinds["workflow"] = true
		}
		if row.Content == "" {
			t.Fatalf("row %s/%s has no content", row.RepoID, row.RelativePath)
		}
	}
	if got, want := byRepo[SeedStoryAnchorID], 620; got != want {
		t.Errorf("anchor files = %d, want %d", got, want)
	}
	for i := 0; i < SeedStoryRelatedCount; i++ {
		if got, want := byRepo[SeedStoryRelatedID(i)], 120; got != want {
			t.Errorf("related %d files = %d, want %d", i, got, want)
		}
	}
	if got, want := len(first), 620+SeedStoryRelatedCount*120; got != want {
		t.Errorf("story rows = %d, want %d", got, want)
	}
	for _, kind := range []string{"tf", "compose", "kustomization", "workflow"} {
		if !kinds[kind] {
			t.Errorf("story corpus has no %s file", kind)
		}
	}
	seen := map[string]bool{}
	for _, row := range first {
		key := row.RepoID + "\x00" + row.RelativePath
		if seen[key] {
			t.Fatalf("duplicate content_files key %q", key)
		}
		seen[key] = true
	}
}

// TestSearchContentRowsCarryBothTokens pins the file-search corpus: 6,000
// documents, the common token in about one file in three of every
// repository, and the rare token in exactly three files of repository zero.
func TestSearchContentRowsCarryBothTokens(t *testing.T) {
	t.Parallel()

	rows := BuildSearchContentRows()
	if got, want := len(rows), SeedSearchRepoCount*SeedSearchFilesPerRepo; got != want {
		t.Fatalf("search rows = %d, want %d", got, want)
	}
	commonByRepo := map[string]int{}
	rare := 0
	for _, row := range rows {
		if strings.Contains(row.Content, SeedSearchFileToken) {
			commonByRepo[row.RepoID]++
		}
		if strings.Contains(row.Content, SeedSearchRareToken) {
			rare++
			if row.RepoID != SeedSearchRepoID(0) {
				t.Errorf("rare token outside repository zero: %s", row.RepoID)
			}
		}
	}
	if rare != 3 {
		t.Errorf("rare token files = %d, want 3", rare)
	}
	for r := 0; r < SeedSearchRepoCount; r++ {
		got := commonByRepo[SeedSearchRepoID(r)]
		if got < 100 || got > 200 {
			t.Errorf("repo %d common files = %d, want 100..200 of %d", r, got, SeedSearchFilesPerRepo)
		}
	}
}

// TestGateEntityRowsAllCarryTheToken pins the entity-search corpus: 300 rows
// whose source_cache all carry the entity token.
func TestGateEntityRowsAllCarryTheToken(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	rows := gateEntityCopyRows(now)
	if len(rows) != SeedEntityRowCount {
		t.Fatalf("entity rows = %d, want %d", len(rows), SeedEntityRowCount)
	}
	for i, row := range rows {
		source, _ := row[7].(string)
		if !strings.Contains(source, SeedEntityToken) {
			t.Errorf("entity row %d source_cache carries no token", i)
		}
		if row[3] != SeedEntityType {
			t.Errorf("entity row %d type = %v, want %s", i, row[3], SeedEntityType)
		}
	}
}

// TestContentFileCopyRowsShapeOneRow pins the CopyFrom shaping: hash,
// line count and timestamp of a known row.
func TestContentFileCopyRowsShapeOneRow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	rows := contentFileCopyRows([]ContentFileSeedRow{{
		RepoID: "r", RelativePath: "infra/m.tf",
		Content: "module \"m\" {\n}\n", Language: "hcl", ArtifactType: "source",
	}}, now)
	if len(rows) != 1 || len(rows[0]) != len(contentFileColumns) {
		t.Fatalf("shaped row = %v, want %d columns", rows, len(contentFileColumns))
	}
	if rows[0][5] != 3 {
		t.Errorf("line_count = %v, want 3", rows[0][5])
	}
	if rows[0][8] != now {
		t.Errorf("indexed_at = %v, want %v", rows[0][8], now)
	}
}
