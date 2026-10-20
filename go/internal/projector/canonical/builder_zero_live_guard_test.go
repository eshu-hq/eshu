// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

// Regression tests for eshu-hq/eshu#8657 (deferred F1 of the #7671 guard
// review): a non-first, non-delta build whose input carries ZERO live file
// facts (no file facts at all, not even tombstoned) still succeeds silently
// after the unbounded retract wipes prior File nodes. The sibling #8660
// guard fails loud when live file facts are dropped; its live == 0 early
// return is exactly this hole. This lane owns only the zero-file-intent
// shape: fail loud when the input carries no file evidence of any kind.
//
// An all-tombstone build keeps succeeding: tombstones are explicit per-file
// delete evidence, so the empty graph matches the input intent (a mass
// delete the retract path carries out), and the sibling lane pins that
// contract. First-generation, delta, and empty-input builds keep succeeding:
// no retract runs for them, so nothing can be wiped.

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func zeroLiveGuardScope() scope.IngestionScope {
	sc := testScope()
	sc.ActiveGenerationID = "gen-previous"
	sc.PreviousGenerationExists = true
	return sc
}

func zeroLiveGuardRepositoryFact() facts.Envelope {
	return facts.Envelope{
		FactID:       "repository-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		FactKind:     "repository",
		Payload: map[string]any{
			"repo_id":    "repo-abc",
			"name":       "my-project",
			"local_path": "/repos/my-project",
		},
	}
}

func zeroLiveGuardEntityFact() facts.Envelope {
	return facts.Envelope{
		FactID:       "entity-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		FactKind:     "content_entity",
		Payload: map[string]any{
			"entity_id":     "content-entity:main",
			"repo_id":       "repo-abc",
			"relative_path": "src/main.go",
			"entity_type":   "Function",
			"entity_name":   "main",
			"language":      "go",
		},
	}
}

// A non-first, non-delta build with no file facts at all must fail loudly:
// success would hand the writer a Repository-only materialization whose
// unbounded retract wipes every prior File node (issue #8657).
func TestBuildMaterializationFailsLoudOnZeroLiveFileFacts(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{zeroLiveGuardRepositoryFact()}

	_, _, err := BuildMaterialization(zeroLiveGuardScope(), testGeneration(), input)
	if err == nil {
		t.Fatal("BuildMaterialization() error = nil, want loud failure; a file-less success on a fileless non-first generation wipes prior File nodes (issue #8657)")
	}
	if !strings.Contains(err.Error(), "gen-1") || !strings.Contains(err.Error(), "scope-1") {
		t.Fatalf("BuildMaterialization() error = %v, want it to name the generation and scope", err)
	}
	if !strings.Contains(err.Error(), "zero file fact") {
		t.Fatalf("BuildMaterialization() error = %v, want it to name the zero-file-facts shape", err)
	}
}

// Non-file rows do not justify the file wipe: entities without any file
// evidence must still fail loud instead of succeeding while the unbounded
// retract deletes every prior File node.
func TestBuildMaterializationFailsLoudOnZeroFileFactsWithEntities(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{zeroLiveGuardRepositoryFact(), zeroLiveGuardEntityFact()}

	_, _, err := BuildMaterialization(zeroLiveGuardScope(), testGeneration(), input)
	if err == nil {
		t.Fatal("BuildMaterialization() error = nil, want loud failure; entity rows do not explain a file wipe (issue #8657)")
	}
}

// An all-tombstone full generation is a legitimate mass delete; that case is
// pinned by TestBuildMaterializationAllTombstoneFilesStillSucceeds in
// builder_fileless_guard_test.go (kept there after the #8660 merge to avoid a
// duplicate), so this file does not repeat it.

// A first generation with no file facts runs no retract, so nothing can be
// wiped and the build keeps succeeding.
func TestBuildMaterializationFirstGenerationRepoOnlyStillSucceeds(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{zeroLiveGuardRepositoryFact()}

	result, _, err := BuildMaterialization(testScope(), testGeneration(), input)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil on a first generation; no retract runs, nothing to wipe", err)
	}
	if result.Repository == nil {
		t.Fatal("Repository = nil, want the repository row materialized")
	}
}

// A delta generation retracts only its scoped path lists, so a fileless
// delta keeps succeeding.
func TestBuildMaterializationDeltaWithoutFileFactsStillSucceeds(t *testing.T) {
	t.Parallel()

	repo := zeroLiveGuardRepositoryFact()
	repo.Payload["delta_generation"] = true
	repo.Payload["delta_relative_paths"] = []string{"src/main.go"}

	result, _, err := BuildMaterialization(zeroLiveGuardScope(), testGeneration(), []facts.Envelope{repo})
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil on a delta generation; retracts stay file-scoped", err)
	}
	if !result.DeltaProjection {
		t.Fatal("DeltaProjection = false, want true")
	}
}

// Empty input returns before any extraction: no repository, no retract, no
// wipe. The guard must not disturb that path.
func TestBuildMaterializationEmptyInputStillSucceeds(t *testing.T) {
	t.Parallel()

	_, _, err := BuildMaterialization(zeroLiveGuardScope(), testGeneration(), nil)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil for empty input; nothing to retract", err)
	}
}
