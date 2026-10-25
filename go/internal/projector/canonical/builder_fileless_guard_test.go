// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

// Regression tests for eshu-hq/eshu#7671: six repository scopes projected
// succeeded with file facts but zero File nodes after a graph rebuild.
//
// Ranked hypotheses (prod forensics unavailable, ingester log expired):
//  1. A Repository-only (or file-less) materialization on a non-first,
//     non-delta generation emits the UNBOUNDED RetractFilesCypher plus the
//     per-label entity retracts, then writes only the Repository node: every
//     prior File node is wiped, zero new ones are written, and the projection
//     reports success. This exactly matches the observed shape (Repository
//     node, no edges, no File nodes, succeeded item).
//  2. A delta generation whose delta path lists are empty degrades to a full
//     refresh in extractDeltaProjectionScope, landing in hypothesis 1.
//  3. Every live file fact quarantined: visible dead-letters, but the
//     generation still succeeds and still wipes (hypothesis 1).
//
// This file pins the lane-owned half: after #8551 (fatal decode fails loud,
// empty paths quarantine), the remaining silent wipe shape is a non-first,
// non-delta build that drops EVERY live file fact yet returns success. That
// build must fail loudly and name the dropped facts instead of handing the
// writer a wipe-capable Repository-only materialization. A recurrence then
// names its fact in the projection error instead of succeeding silently.
//
// Legitimate zero-File builds stay green: first generations (no retract
// runs), delta projections (scoped retract), and all-tombstone full
// generations (a mass delete the retract path is meant to carry out).

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func filelessGuardScope() scope.IngestionScope {
	sc := testScope()
	sc.ActiveGenerationID = "gen-previous"
	sc.PreviousGenerationExists = true
	return sc
}

func filelessGuardRepositoryFact() facts.Envelope {
	return facts.Envelope{
		FactID:        "repository-1",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphRepository,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"repo_id":    "repo-abc",
			"name":       "my-project",
			"local_path": "/repos/my-project",
		},
	}
}

func filelessGuardFileFact(factID string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphFile,
		SchemaVersion: "1.0.0",
		IsTombstone:   tombstone,
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"relative_path":    "",
			"parsed_file_data": map[string]any{},
			"language":         "go",
		},
	}
}

// A non-first, non-delta generation whose every live file fact is dropped
// (here: empty relative_path, quarantined) must fail loudly naming the
// facts. Success would hand the writer a Repository-only materialization
// whose unbounded retract wipes every prior File node (issue #7671).
func TestBuildMaterializationFailsLoudWhenAllLiveFilesDropped(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		filelessGuardRepositoryFact(),
		filelessGuardFileFact("file-dropped-1", false),
		filelessGuardFileFact("file-dropped-2", false),
	}

	_, _, err := BuildMaterialization(filelessGuardScope(), testGeneration(), input)
	if err == nil {
		t.Fatal("BuildMaterialization() error = nil, want loud failure; a file-less success on a file-bearing generation wipes prior File nodes (issue #7671)")
	}
	if !strings.Contains(err.Error(), "file-dropped-1") || !strings.Contains(err.Error(), "file-dropped-2") {
		t.Fatalf("BuildMaterialization() error = %v, want it to name the dropped file facts", err)
	}
}

// An all-tombstone full generation is a legitimate mass delete: the retract
// path carries it out, so the build must still succeed.
func TestBuildMaterializationAllTombstoneFilesStillSucceeds(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		filelessGuardRepositoryFact(),
		filelessGuardFileFact("file-deleted-1", true),
		filelessGuardFileFact("file-deleted-2", true),
	}

	result, _, err := BuildMaterialization(filelessGuardScope(), testGeneration(), input)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil; all-tombstone is a legitimate delete, not a silent drop", err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("len(Files) = %d, want 0", len(result.Files))
	}
}

// A first generation with all-dropped files runs no retract, so it keeps the
// #8551 quarantine-and-succeed contract.
func TestBuildMaterializationFirstGenerationAllDroppedStillSucceeds(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		filelessGuardRepositoryFact(),
		filelessGuardFileFact("file-dropped-1", false),
	}

	result, quarantined, err := BuildMaterialization(testScope(), testGeneration(), input)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil on a first generation; no retract runs, nothing to wipe", err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("len(Files) = %d, want 0", len(result.Files))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1", len(quarantined))
	}
}
