// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
)

// deltaCassetteRelPath is the multi-generation tombstone cassette relative to
// this package directory (go/internal/replay/offlinetier).
var deltaCassetteRelPath = filepath.Join("..", "..", "..", "..", "testdata", "cassettes", "replaydelta", "multi-generation-tombstone.json")

// deltaRepoID is the canonical repository ID for the delta tombstone scenario.
const deltaRepoID = "replay-delta-tombstone"

// deltaRepoPath is the canonical repository path for the delta tombstone scenario.
const deltaRepoPath = "/repos/replay-delta-tombstone"

// --- Offline (no-backend) structural checks — run every PR ---

// TestDeltaMaterializationGen1Baseline verifies the gen1 cassette materializes
// alpha, beta, gamma, and the parent/child directories used for direct
// CONTAINS edge-retraction coverage. This proves the baseline is correct before
// gen2 delta/retraction is applied.
func TestDeltaMaterializationGen1Baseline(t *testing.T) {
	t.Parallel()

	src := loadDeltaCassette(t)

	gen1, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen1: %v", err)
	}
	if !ok {
		t.Fatal("cassette yielded no gen1")
	}
	gen2, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen2: %v", err)
	}
	if !ok {
		t.Fatal("cassette yielded no gen2")
	}

	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations: %v", err)
	}

	// Gen2 must retain alpha, beta, delta, both edge parents, and edge-child.
	if got, want := len(dm.Gen2.Directories), 6; got != want {
		t.Fatalf("gen2 surviving directory rows = %d, want %d", got, want)
	}

	// Gamma must appear in TombstonedDirectoryPaths.
	if got, want := len(dm.TombstonedDirectoryPaths), 1; got != want {
		t.Fatalf("tombstoned directory paths = %d, want %d", got, want)
	}
	if got, want := dm.TombstonedDirectoryPaths[0], deltaRepoPath+"/gamma"; got != want {
		t.Fatalf("tombstoned path = %q, want %q", got, want)
	}

	// Gen2 must have FirstGeneration=false so the production retract phase fires.
	if dm.Gen2.FirstGeneration {
		t.Fatal("gen2 FirstGeneration = true, want false — retraction would be skipped")
	}
	if !dm.Gen2.DeltaProjection {
		t.Fatal("gen2 DeltaProjection = false, want true — replaydelta must exercise bounded delta retraction")
	}
	if got, want := dm.Gen2.DeltaDeletedDirectoryPaths, []string{deltaRepoPath + "/gamma"}; !equalStringSlices(got, want) {
		t.Fatalf("gen2 DeltaDeletedDirectoryPaths = %#v, want %#v", got, want)
	}

	// Gen1 baseline must be materialized from a single drain.
	// This guards the double-drain regression: a CollectedGeneration's fact
	// channel is closed after one range, so the caller must use dm.Gen1 rather
	// than re-materializing gen1 (which would yield an empty generation).
	if dm.Gen1.Repository == nil {
		t.Fatal("dm.Gen1.Repository is nil — gen1 baseline not materialized (double-drain regression?)")
	}
	if got, want := len(dm.Gen1.Directories), 6; got != want {
		t.Fatalf("gen1 baseline directory rows = %d, want %d", got, want)
	}
}

// TestDeltaMaterializationGen2RetainsSupersededRepo verifies that gen2 carries
// the updated repository name (supersession): the repo fact in gen2 changes the
// name from "replay-delta-tombstone" to "replay-delta-tombstone-v2".
func TestDeltaMaterializationGen2RetainsSupersededRepo(t *testing.T) {
	t.Parallel()

	src := loadDeltaCassette(t)
	gen1, _, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen1: %v", err)
	}
	gen2, _, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen2: %v", err)
	}

	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations: %v", err)
	}

	if dm.Gen2.Repository == nil {
		t.Fatal("gen2 Repository is nil")
	}
	if got, want := dm.Gen2.Repository.Name, "replay-delta-tombstone-v2"; got != want {
		t.Fatalf("gen2 repository name = %q, want %q (supersession not reflected)", got, want)
	}
}

// TestDeltaMaterializationSurvivingDirNames verifies that alpha, beta, delta,
// both edge parents, and edge-child are present in the gen2 materialization and
// gamma is absent (tombstoned).
func TestDeltaMaterializationSurvivingDirNames(t *testing.T) {
	t.Parallel()

	src := loadDeltaCassette(t)
	gen1, _, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen1: %v", err)
	}
	gen2, _, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen2: %v", err)
	}

	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations: %v", err)
	}

	survivingPaths := make(map[string]struct{}, len(dm.Gen2.Directories))
	for _, d := range dm.Gen2.Directories {
		survivingPaths[d.Path] = struct{}{}
	}

	for _, want := range []string{
		deltaRepoPath + "/alpha",
		deltaRepoPath + "/beta",
		deltaRepoPath + "/delta",
		deltaRepoPath + "/edge-parent-a",
		deltaRepoPath + "/edge-parent-b",
		deltaRepoPath + "/edge-parent-a/edge-child",
	} {
		if _, ok := survivingPaths[want]; !ok {
			t.Errorf("directory %q missing from gen2 surviving rows", want)
		}
	}
	// Gamma must not appear in surviving rows (tombstoned in gen2).
	if _, ok := survivingPaths[deltaRepoPath+"/gamma"]; ok {
		t.Error("gamma present in gen2 surviving rows — tombstone not filtered")
	}
}

// TestDeltaMaterializationMovedChildKeepsNodeAndChangesParent verifies the
// replaydelta cassette models direct CONTAINS edge retraction, not node removal:
// edge-child survives both generations, but gen2 points it at edge-parent-b
// instead of edge-parent-a.
func TestDeltaMaterializationMovedChildKeepsNodeAndChangesParent(t *testing.T) {
	t.Parallel()

	src := loadDeltaCassette(t)
	gen1, _, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen1: %v", err)
	}
	gen2, _, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read gen2: %v", err)
	}

	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations: %v", err)
	}

	const childPath = deltaRepoPath + "/edge-parent-a/edge-child"
	assertDirectoryParentPath(t, dm.Gen1.Directories, childPath, deltaRepoPath+"/edge-parent-a")
	assertDirectoryParentPath(t, dm.Gen2.Directories, childPath, deltaRepoPath+"/edge-parent-b")
}

// --- helpers ---

// loadDeltaCassette loads the multi-generation tombstone cassette.
func loadDeltaCassette(t *testing.T) *cassette.Source {
	t.Helper()
	src, err := cassette.NewSource(deltaCassetteRelPath)
	if err != nil {
		t.Fatalf("load delta cassette %s: %v", deltaCassetteRelPath, err)
	}
	return src
}

func assertDirectoryParentPath(t *testing.T, dirs []projector.DirectoryRow, childPath, wantParentPath string) {
	t.Helper()
	for _, dir := range dirs {
		if dir.Path != childPath {
			continue
		}
		if dir.ParentPath != wantParentPath {
			t.Fatalf("directory %q parent_path = %q, want %q", childPath, dir.ParentPath, wantParentPath)
		}
		return
	}
	t.Fatalf("missing directory %q", childPath)
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
