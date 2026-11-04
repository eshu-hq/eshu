// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// gitDependencyEntity is a manifest or lockfile dependency row the git
// collector emits: a content_entity Variable whose entity_metadata says
// config_kind dependency.
func gitDependencyEntity(factID string, metadata map[string]any) facts.Envelope {
	entityMetadata := map[string]any{
		"config_kind":     "dependency",
		"package_manager": "npm",
		"value":           "4.17.21",
	}
	for key, value := range metadata {
		entityMetadata[key] = value
	}
	return facts.Envelope{
		FactID:        factID,
		FactKind:      "content_entity",
		CollectorKind: "git",
		SourceRef:     facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"entity_type":     "Variable",
			"entity_name":     "lodash",
			"repo_id":         "repository:r_checkout",
			"relative_path":   "package-lock.json",
			"entity_metadata": entityMetadata,
		},
	}
}

// TestBuildSupplyChainImpactReducerIntentGitDependencyEnqueuesMarker is the
// #9874 regression: a git generation whose only candidate fact is a
// dependency row enqueues exactly one supply_chain_impact intent, keyed by
// scope and carrying the manifest_dependency marker the reducer branches on.
func TestBuildSupplyChainImpactReducerIntentGitDependencyEnqueuesMarker(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file", FactID: "file-1", SourceRef: facts.Ref{SourceSystem: "git"}},
		gitDependencyEntity("dep-1", nil),
		gitDependencyEntity("dep-2", map[string]any{"value": "1.0.0"}),
	})
	intent, ok := BuildSupplyChainImpactReducerIntent("git-repo-1", "gen-2", lookup)
	if !ok {
		t.Fatal("no supply_chain_impact intent for a git generation with a dependency row")
	}
	if got, want := intent.EntityKey, "supply_chain_impact:git-repo-1"; got != want {
		t.Fatalf("EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "dep-1"; got != want {
		t.Fatalf("FactID = %q, want the earliest dependency row %q", got, want)
	}
	if got, want := intent.Reason, "manifest dependency evidence observed"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "git"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
	// The reducer branches on the core constants; asserting against them
	// here keeps the two packages' spellings from drifting apart.
	if got, want := intent.Payload[supplychaincore.PayloadKeyTrigger], supplychaincore.TriggerManifestDependency; got != want {
		t.Fatalf("Payload[%q] = %v, want %q", supplychaincore.PayloadKeyTrigger, got, want)
	}
}

// TestBuildSupplyChainImpactReducerIntentGitNonDependencyEnqueuesNothing
// proves the predicate stays narrow: git content entities that are not
// dependency declarations, central version declarations, and dependency-shaped
// rows from a non-git source do not trigger impact matching.
func TestBuildSupplyChainImpactReducerIntentGitNonDependencyEnqueuesNothing(t *testing.T) {
	t.Parallel()

	function := gitDependencyEntity("fn-1", nil)
	function.Payload["entity_type"] = "Function"
	otherConfig := gitDependencyEntity("cfg-1", map[string]any{"config_kind": "dependency_replace"})
	central := gitDependencyEntity("central-1", map[string]any{"central_version_declaration": "true"})
	nonGit := gitDependencyEntity("other-1", nil)
	nonGit.SourceRef.SourceSystem = "sbom"
	nonGit.CollectorKind = "sbom"
	unnamed := gitDependencyEntity("unnamed-1", nil)
	unnamed.Payload["entity_name"] = " "

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file", FactID: "file-1", SourceRef: facts.Ref{SourceSystem: "git"}},
		function, otherConfig, central, nonGit, unnamed,
	})
	if intent, ok := BuildSupplyChainImpactReducerIntent("git-repo-1", "gen-2", lookup); ok {
		t.Fatalf("queued %#v for a git generation with no dependency row", intent)
	}
}

// TestBuildSupplyChainImpactReducerIntentExistingTriggerWinsOverMarker pins
// that the marker is exclusive: a generation holding an existing trigger kind
// keeps today's full impact pass, even when it also holds dependency rows, so
// the reducer never skips findings for it.
func TestBuildSupplyChainImpactReducerIntentExistingTriggerWinsOverMarker(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		gitDependencyEntity("dep-1", nil),
		{FactKind: chain.SBOMComponentFactKind, FactID: "sbom-1"},
	})
	intent, ok := BuildSupplyChainImpactReducerIntent("git-repo-1", "gen-2", lookup)
	if !ok {
		t.Fatal("no intent queued")
	}
	if intent.FactID != "sbom-1" {
		t.Fatalf("FactID = %q, want the SBOM trigger sbom-1", intent.FactID)
	}
	if _, marked := intent.Payload[supplychaincore.PayloadKeyTrigger]; marked {
		t.Fatalf("Payload = %#v, want no manifest marker when an existing trigger is present", intent.Payload)
	}
}

// TestBuildSupplyChainImpactReducerIntentGitDependencyTombstoneTriggers proves
// a removed dependency row (tombstone) still triggers: a removal changes which
// findings hold for the repository.
func TestBuildSupplyChainImpactReducerIntentGitDependencyTombstoneTriggers(t *testing.T) {
	t.Parallel()

	removed := gitDependencyEntity("dep-removed", nil)
	removed.IsTombstone = true
	lookup := projectorintent.NewFactLookup([]facts.Envelope{removed})
	if _, ok := BuildSupplyChainImpactReducerIntent("git-repo-1", "gen-2", lookup); !ok {
		t.Fatal("no intent queued for a tombstoned dependency row")
	}
}
