// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// dependencyEntity builds a package.json-shaped npm manifest dependency
// Entity as dependencyVariablesWithScope (parser/json/language.go) emits it:
// config_kind=="dependency", package_manager=="npm", no lockfile key, and a
// section name. lineNumber of 0 mirrors the "line-less" case documented in
// dependencyVariablesWithScope's doc comment (ordered walk unavailable for
// this file).
func dependencyEntity(name, section string, lineNumber int) Entity {
	return Entity{
		Name:       name,
		LineNumber: lineNumber,
		Metadata: map[string]any{
			"section":         section,
			"config_kind":     "dependency",
			"package_manager": "npm",
			"lang":            "json",
		},
	}
}

func entityIDForName(t *testing.T, entities []content.EntityRecord, name string) content.EntityRecord {
	t.Helper()
	for _, entity := range entities {
		if entity.EntityName == name {
			return entity
		}
	}
	t.Fatalf("no entity named %q in %d entities", name, len(entities))
	return content.EntityRecord{}
}

// TestMaterializeDependencyIdentityReorderDoesNotChurn is test (a) from the
// #5357 locked spec: two package.json bodies differing ONLY in dependency
// order within the same section must mint identical entity_ids per
// dependency name, even though reordering shifts each dependency's source
// line. This also asserts the new id differs from the pre-fix line-keyed
// CanonicalEntityID for the same row, proving the fix actually changed the
// minted id rather than coincidentally matching it.
func TestMaterializeDependencyIdentityReorderDoesNotChurn(t *testing.T) {
	t.Parallel()

	// Manifest A: react before lodash in source order (react on line 2,
	// lodash on line 3).
	gotA, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "package.json",
				Body: "{}",
				EntityBuckets: map[string][]Entity{
					"variables": {
						dependencyEntity("react", "dependencies", 2),
						dependencyEntity("lodash", "dependencies", 3),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize(A) error = %v, want nil", err)
	}

	// Manifest B: same two dependencies, reordered — lodash now precedes
	// react in source, so their line numbers swap.
	gotB, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "package.json",
				Body: "{}",
				EntityBuckets: map[string][]Entity{
					"variables": {
						dependencyEntity("lodash", "dependencies", 2),
						dependencyEntity("react", "dependencies", 3),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize(B) error = %v, want nil", err)
	}

	reactA := entityIDForName(t, gotA.Entities, "react")
	reactB := entityIDForName(t, gotB.Entities, "react")
	lodashA := entityIDForName(t, gotA.Entities, "lodash")
	lodashB := entityIDForName(t, gotB.Entities, "lodash")

	if reactA.EntityID != reactB.EntityID {
		t.Fatalf("react entity_id churned across reorder: A=%q (line %d) vs B=%q (line %d)",
			reactA.EntityID, reactA.StartLine, reactB.EntityID, reactB.StartLine)
	}
	if lodashA.EntityID != lodashB.EntityID {
		t.Fatalf("lodash entity_id churned across reorder: A=%q (line %d) vs B=%q (line %d)",
			lodashA.EntityID, lodashA.StartLine, lodashB.EntityID, lodashB.StartLine)
	}

	// Prove the fix actually changed the minted id: under the pre-fix
	// line-keyed scheme, react's id in manifest A (line 2) and manifest B
	// (line 3) would differ because CanonicalEntityID hashes the line. The
	// new scheme must NOT match that legacy per-manifest line-keyed id.
	legacyReactA := content.CanonicalEntityID("repository:r_12345678", "package.json", "Variable", "react", reactA.StartLine)
	legacyReactB := content.CanonicalEntityID("repository:r_12345678", "package.json", "Variable", "react", reactB.StartLine)
	if legacyReactA == legacyReactB {
		t.Fatalf("test fixture invalid: legacy CanonicalEntityID did not churn across reorder (A=%q, B=%q); the pre-fix scheme must differ here for this to be a meaningful regression test", legacyReactA, legacyReactB)
	}
	if reactA.EntityID == legacyReactA {
		t.Fatalf("react entity_id = %q, want it to differ from the legacy line-keyed CanonicalEntityID %q (section-keyed identity must not equal the pre-fix scheme)", reactA.EntityID, legacyReactA)
	}

	// StartLine/EndLine on the EntityRecord stay unchanged — only the id hash
	// loses the line, per the locked spec.
	if reactA.StartLine != 2 {
		t.Fatalf("reactA.StartLine = %d, want 2 (StartLine must stay unchanged)", reactA.StartLine)
	}
	if reactB.StartLine != 3 {
		t.Fatalf("reactB.StartLine = %d, want 3 (StartLine must stay unchanged)", reactB.StartLine)
	}
}

// TestMaterializeDependencyIdentityCrossSectionDistinctness is test (b) from
// the #5357 locked spec: the same package name in two different sections
// (dependencies vs peerDependencies) must mint two distinct entity_ids.
//
// The line-less variant (both rows omit line_number, as
// dependencyVariablesWithScope documents happens when the ordered walk is
// unavailable) is the failing-then-green regression for a real status-quo
// bug: indexedEntity.lineNumber() defaults line-less entities to 1, so today
// BOTH rows hash to (path, type, name, line=1) under CanonicalEntityID and
// collapse into one entity — a peerDependencies-only "react" would be
// invisible in the content store, and content_entities.metadata (section,
// dependency_scope) would nondeterministically reflect only one of the two
// declarations depending on write order. This test's line-less case
// FAILS under the pre-fix scheme and PASSES section-keyed.
func TestMaterializeDependencyIdentityCrossSectionDistinctness(t *testing.T) {
	t.Parallel()

	t.Run("with line numbers", func(t *testing.T) {
		t.Parallel()

		got, err := Materialize(Input{
			RepoID: "repository:r_12345678",
			Files: []File{
				{
					Path: "package.json",
					Body: "{}",
					EntityBuckets: map[string][]Entity{
						"variables": {
							dependencyEntity("react", "dependencies", 2),
							dependencyEntity("react", "peerDependencies", 8),
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Materialize() error = %v, want nil", err)
		}
		if len(got.Entities) != 2 {
			t.Fatalf("len(Materialize().Entities) = %d, want 2 (both sections' react rows must survive)", len(got.Entities))
		}
		if got.Entities[0].EntityID == got.Entities[1].EntityID {
			t.Fatalf("dependencies react entity_id (%q) collided with peerDependencies react entity_id (%q)",
				got.Entities[0].EntityID, got.Entities[1].EntityID)
		}
	})

	t.Run("line-less regression (status-quo collapse)", func(t *testing.T) {
		t.Parallel()

		got, err := Materialize(Input{
			RepoID: "repository:r_12345678",
			Files: []File{
				{
					Path: "package.json",
					Body: "{}",
					EntityBuckets: map[string][]Entity{
						"variables": {
							// LineNumber omitted (0) on both rows, matching
							// dependencyVariablesWithScope's documented
							// fallback when the ordered walk could not run.
							dependencyEntity("react", "dependencies", 0),
							dependencyEntity("react", "peerDependencies", 0),
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Materialize() error = %v, want nil", err)
		}

		// Both line-less rows default to StartLine 1 via
		// indexedEntity.lineNumber()'s floor, so this is exactly the
		// collision case: two distinct logical dependencies sharing a line.
		// Materialize() itself still emits one EntityRecord per input row
		// (it does not dedup); the collision manifests one layer down, at
		// the Postgres upsert keyed on entity_id — a shared id there means
		// only one of the two rows survives. Asserting entity_id
		// distinctness here is what actually proves the fix.
		for _, entity := range got.Entities {
			if entity.StartLine != 1 {
				t.Fatalf("entity[%s].StartLine = %d, want 1 (line-less floor)", entity.EntityName, entity.StartLine)
			}
		}

		if len(got.Entities) != 2 {
			t.Fatalf("len(Materialize().Entities) = %d, want 2", len(got.Entities))
		}
		if got.Entities[0].EntityID == got.Entities[1].EntityID {
			t.Fatalf("line-less dependencies react entity_id (%q) collided with line-less peerDependencies react entity_id (%q); section-keyed identity must split these even though both default to StartLine=1",
				got.Entities[0].EntityID, got.Entities[1].EntityID)
		}
	})
}
