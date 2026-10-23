// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// overlayDiffRepoPath is the repository root the differential's delta names.
const overlayDiffRepoPath = "/repos/org"

// overlayDiffBaseFile is one file row of the anchor (the base full
// generation). Its stable key is the collector's file key for the raw path.
func overlayDiffBaseFile(id, relativePath string) facts.Envelope {
	return facts.Envelope{
		FactID: id, ScopeID: "scope-8750", GenerationID: "gen-full", FactKind: "file",
		StableFactKey: "file:org/repo:" + relativePath,
		ObservedAt:    time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		Payload:       map[string]any{"relative_path": relativePath},
	}
}

func overlayDiffDeltaFile(id, relativePath string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID: id, ScopeID: "scope-8750", GenerationID: "gen-next", FactKind: "file",
		StableFactKey: "file:org/repo:" + relativePath, IsTombstone: tombstone,
		ObservedAt: time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC),
		Payload:    map[string]any{"relative_path": relativePath},
	}
}

func overlayDiffRepositoryFact(changed, deleted []string) facts.Envelope {
	return facts.Envelope{
		FactID: "delta-repo", ScopeID: "scope-8750", GenerationID: "gen-next", FactKind: "repository",
		StableFactKey: "repository:org/repo", ObservedAt: time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repo_id": "org/repo", "path": overlayDiffRepoPath, "delta_generation": true,
			"delta_relative_paths": toAnySlice(changed), "delta_deleted_relative_paths": toAnySlice(deleted),
		},
	}
}

// overlayDiffAdversarialBase returns the anchor's file rows. Their raw paths
// are the awkward spellings the overlay's path qualification has to agree on.
func overlayDiffAdversarialBase() []facts.Envelope {
	return []facts.Envelope{
		overlayDiffBaseFile("base-01", "a/b.go"),
		overlayDiffBaseFile("base-02", "./a/b.go"), // same qualified path as base-01
		overlayDiffBaseFile("base-03", "docs/naïve plan.md"),
		overlayDiffBaseFile("base-04", "with space.go"),
		overlayDiffBaseFile("base-05", "a/b"), // deleted as "a/b/" (trailing slash)
		overlayDiffBaseFile("base-06", "../escape.go"),
		overlayDiffBaseFile("base-07", "renamed/old.go"),
		overlayDiffBaseFile("base-08", "keep/one.go"),
		overlayDiffBaseFile("base-09", "keep/two.go"),
		overlayDiffBaseFile("base-10", "ünï/çödé.go"),
		overlayDiffBaseFile("base-11", "  padded.go  "),
	}
}

// overlayDiffAdversarialDelta returns the delta's stream: its repository fact,
// live files for the changed paths, and tombstones for the deleted ones. The
// changed and deleted lists carry the spellings the parser must clean, drop
// (../escape.go, /abs/path.go), or never match (gone/never-indexed.go).
func overlayDiffAdversarialDelta() []facts.Envelope {
	changed := []string{"./a/b.go", "new.go", "../escape.go", "renamed/new.go"}
	deleted := []string{
		"docs/naïve plan.md", "with space.go", "a/b/", "gone/never-indexed.go",
		"renamed/old.go", "padded.go", "ünï/çödé.go", "/abs/path.go",
	}
	delta := []facts.Envelope{overlayDiffRepositoryFact(changed, deleted)}
	for i, rel := range []string{"a/b.go", "new.go", "renamed/new.go"} {
		delta = append(delta, overlayDiffDeltaFile(fmt.Sprintf("delta-live-%d", i), rel, false))
	}
	for i, rel := range deleted {
		delta = append(delta, overlayDiffDeltaFile(fmt.Sprintf("delta-tomb-%d", i), rel, true))
	}
	return delta
}

// overlayDroppedBaseFiles counts the base file rows facts.OverlayDeltaBase
// removes: the rows whose fact id is absent from the merged output.
func overlayDroppedBaseFiles(base, delta []facts.Envelope) int {
	merged := facts.OverlayDeltaBase(base, delta, facts.ParseDeltaScope(delta))
	kept := make(map[string]struct{}, len(merged))
	for _, env := range merged {
		kept[env.FactID] = struct{}{}
	}
	dropped := 0
	for _, env := range base {
		if _, ok := kept[env.FactID]; !ok {
			dropped++
		}
	}
	return dropped
}

// guardAnchorFilesTouched runs the guard's own path: the successorObserver
// folds the delta's stream into the touched-or-live set, then
// countAnchorFilesTouched reads the anchor's file paths (as the probe query
// returns them: payload relative_path, one per file row) and counts the ones
// inside the set.
func guardAnchorFilesTouched(t *testing.T, base, delta []facts.Envelope) int {
	t.Helper()
	observer := newSuccessorObserver(scope.ScopeGeneration{IsDelta: true})
	for _, env := range delta {
		observer.observe(env)
	}
	if !observer.scope.IsDelta {
		t.Fatal("observer did not parse the delta_generation repository fact")
	}
	touchedOrLive, _ := observer.unionSets()
	database := &guardRoutingDB{t: t, tx: &fakeTx{}}
	for _, env := range base {
		database.anchorPaths = append(database.anchorPaths, env.Payload["relative_path"].(string))
	}
	tx, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	touched, err := NewIngestionStore(database).countAnchorFilesTouched(
		context.Background(), tx, "scope-8750", "gen-full", observer.scope, touchedOrLive)
	if err != nil {
		t.Fatalf("countAnchorFilesTouched() error = %v", err)
	}
	return touched
}

// TestGuardTouchedAnchorCountEqualsOverlayDrops is the differential behind the
// guard's central claim: the number of anchor file rows the guard counts as
// touched is exactly the number of base file rows the projector's overlay
// drops. Both sides run production code on one adversarial fixture: duplicate
// spellings of one path, a trailing slash, unicode, spaces, an out-of-repo
// path, a deleted path that was never indexed, and a rename.
func TestGuardTouchedAnchorCountEqualsOverlayDrops(t *testing.T) {
	base := overlayDiffAdversarialBase()
	delta := overlayDiffAdversarialDelta()

	dropped := overlayDroppedBaseFiles(base, delta)
	touched := guardAnchorFilesTouched(t, base, delta)

	// base-01..05, 07, 10 and 11 are removed; base-06 (out of repo) and the
	// two keep/ files survive. Pinned as a literal so a degenerate fixture
	// that drops nothing cannot make the equality vacuous.
	if dropped != 8 {
		t.Fatalf("overlay dropped %d base file rows, want 8: the fixture no longer exercises every spelling", dropped)
	}
	if touched != dropped {
		t.Fatalf("guard counted %d anchor files touched, overlay dropped %d: the guard and the projector disagree on paths", touched, dropped)
	}
}

// TestGuardTouchedAnchorCountIsConservativeForLivePathsOutsideTouched pins the
// documented direction of the one place the guard over-counts. A live delta
// file that its repository fact did not list as changed is added to the
// touched-or-live set; if the base holds that path under a different stable
// key the overlay keeps the base row but the guard counts it. The guard never
// counts fewer than the overlay drops, so the union it computes can only be
// smaller, the stricter side for a destructive-successor guard.
func TestGuardTouchedAnchorCountIsConservativeForLivePathsOutsideTouched(t *testing.T) {
	base := append(overlayDiffAdversarialBase(), facts.Envelope{
		FactID: "base-extra", ScopeID: "scope-8750", GenerationID: "gen-full", FactKind: "file",
		StableFactKey: "file:org/repo:extra.go#base-key",
		ObservedAt:    time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		Payload:       map[string]any{"relative_path": "extra.go"},
	})
	delta := append(overlayDiffAdversarialDelta(), overlayDiffDeltaFile("delta-live-extra", "extra.go", false))

	dropped := overlayDroppedBaseFiles(base, delta)
	touched := guardAnchorFilesTouched(t, base, delta)

	if touched < dropped {
		t.Fatalf("guard counted %d anchor files touched, fewer than the %d the overlay drops: the guard would under-count a deletion", touched, dropped)
	}
	if touched != dropped+1 {
		t.Fatalf("guard counted %d, overlay dropped %d: want the guard exactly one higher for the unlisted live path", touched, dropped)
	}
}

// TestGuardTouchedAnchorCountMatchesOverlayWhenLiveFileSharesTheBaseKey is the
// complement: when the unlisted live file carries the same stable key as the
// base row, the overlay replaces it by key and both sides agree again.
func TestGuardTouchedAnchorCountMatchesOverlayWhenLiveFileSharesTheBaseKey(t *testing.T) {
	base := append(overlayDiffAdversarialBase(), overlayDiffBaseFile("base-extra", "extra.go"))
	delta := append(overlayDiffAdversarialDelta(), overlayDiffDeltaFile("delta-live-extra", "extra.go", false))

	dropped := overlayDroppedBaseFiles(base, delta)
	touched := guardAnchorFilesTouched(t, base, delta)

	if dropped != 9 || touched != dropped {
		t.Fatalf("overlay dropped %d, guard counted %d, want both 9", dropped, touched)
	}
}

// TestGuardTouchedAnchorCountMatchesOverlayForUnlistedTombstone is review F3:
// the overlay drops a base row whenever a delta tombstone shares its stable
// key, whether or not the repository fact lists the path. The guard must
// count that file as touched too, or a tombstone-heavy stream whose manifest
// omits paths would wipe files the guard counts as kept.
func TestGuardTouchedAnchorCountMatchesOverlayForUnlistedTombstone(t *testing.T) {
	base := append(overlayDiffAdversarialBase(), overlayDiffBaseFile("base-extra", "unlisted/gone.go"))
	delta := append(overlayDiffAdversarialDelta(), overlayDiffDeltaFile("delta-tomb-extra", "unlisted/gone.go", true))

	dropped := overlayDroppedBaseFiles(base, delta)
	touched := guardAnchorFilesTouched(t, base, delta)

	if dropped != 9 {
		t.Fatalf("overlay dropped %d base file rows, want 9: the unlisted tombstone no longer removes its base row", dropped)
	}
	if touched != dropped {
		t.Fatalf("guard counted %d anchor files touched, overlay dropped %d: an unlisted tombstone escapes the guard", touched, dropped)
	}
}
