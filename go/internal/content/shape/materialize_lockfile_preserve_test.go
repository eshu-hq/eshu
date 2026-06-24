// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"testing"
)

// TestMaterializeTransitiveLockfileEntriesPreserved verifies that transitive
// lockfile dependency entries (dependency_depth >= 2, direct_dependency=false)
// are preserved in full after the lockfile variable cap is removed.
//
// Regression for the inverse of #3676's original cap: silently dropping
// transitive entries destroys supply-chain truth fed to the reducer's
// PackageConsumptionDecision. The real cause of lockfile entity explosion is
// 4x corpus nesting, not a shape-layer concern.
func TestMaterializeTransitiveLockfileEntriesPreserved(t *testing.T) {
	t.Parallel()

	const (
		directCount     = 10
		transitiveCount = 590
		totalCount      = directCount + transitiveCount
	)

	entities := make([]Entity, 0, totalCount)

	// Direct deps (dependency_depth == 1, direct_dependency == true).
	for i := 0; i < directCount; i++ {
		entities = append(entities, Entity{
			Name:       "direct-dep-" + itoa(i),
			LineNumber: i + 1,
			Metadata: map[string]any{
				"lockfile":          true,
				"dependency_depth":  1,
				"direct_dependency": true,
			},
		})
	}

	// Transitive deps (dependency_depth == 2, direct_dependency == false).
	// Previously the lockfile variable cap would drop all of these.
	for i := 0; i < transitiveCount; i++ {
		entities = append(entities, Entity{
			Name:       "transitive-dep-" + itoa(i),
			LineNumber: directCount + i + 1,
			Metadata: map[string]any{
				"lockfile":          true,
				"dependency_depth":  2,
				"direct_dependency": false,
			},
		})
	}

	got, err := Materialize(Input{
		RepoID:       "repository:r_lockpreserve",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "package-lock.json",
				Language: "json",
				EntityBuckets: map[string][]Entity{
					"variables": entities,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	// All 600 entries must be present — no cap applied.
	if got, want := len(got.Entities), totalCount; got != want {
		t.Fatalf("entity count = %d, want %d (all entries must be preserved, no lockfile cap)",
			got, want)
	}

	// At least one transitive entry must be present in the output.
	transitiveFound := 0
	for _, e := range got.Entities {
		depth, _ := e.Metadata["dependency_depth"].(int)
		isDirect, _ := e.Metadata["direct_dependency"].(bool)
		if depth == 2 && !isDirect {
			transitiveFound++
		}
	}
	if transitiveFound == 0 {
		t.Fatalf("no transitive entries (depth==2) found in output; want %d", transitiveCount)
	}
}
