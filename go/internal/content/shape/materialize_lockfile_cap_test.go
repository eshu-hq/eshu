package shape

import (
	"testing"
)

// TestMaterializeLockfileVariableCapKeepsDirectDepsOnly verifies that when a
// lockfile file's variables bucket exceeds MaxLockfileVariableEntities, only
// direct-dependency rows (dependency_depth == 1 or direct_dependency == true)
// are kept, and the retained count does not exceed MaxLockfileVariableEntities.
//
// Regression: a single package-lock.json produced 847,675 content_entity facts
// on the full corpus run (issue #3676). The lockfile=true flag in Metadata
// identifies lockfile rows; direct_dependency=true in Metadata identifies the
// direct subset.
func TestMaterializeLockfileVariableCapKeepsDirectDepsOnly(t *testing.T) {
	t.Parallel()

	// Build more entities than the cap, mixing direct (depth 1) and transitive
	// (depth > 1) lockfile dependencies.
	entities := make([]Entity, 0, MaxLockfileVariableEntities*3)

	// Direct deps: these must survive the cap.
	for i := 0; i < 10; i++ {
		entities = append(entities, Entity{
			Name:       "direct-dep-" + itoa(i),
			LineNumber: i + 1,
			Metadata: map[string]any{
				"lockfile":         true,
				"config_kind":      "dependency",
				"dependency_depth": 1,
				"direct_dependency": true,
			},
		})
	}

	// Transitive deps: these must be dropped when over cap.
	transitiveCount := MaxLockfileVariableEntities*2 + 5
	for i := 0; i < transitiveCount; i++ {
		entities = append(entities, Entity{
			Name:       "transitive-dep-" + itoa(i),
			LineNumber: i + 11,
			Metadata: map[string]any{
				"lockfile":         true,
				"config_kind":      "dependency",
				"dependency_depth": 2,
				"direct_dependency": false,
			},
		})
	}

	got, err := Materialize(Input{
		RepoID:       "repository:r_test0001",
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

	if len(got.Entities) > MaxLockfileVariableEntities {
		t.Fatalf("entity count = %d, want <= %d (MaxLockfileVariableEntities)",
			len(got.Entities), MaxLockfileVariableEntities)
	}

	// All surviving entities must be direct deps.
	for _, e := range got.Entities {
		depth, _ := e.Metadata["dependency_depth"].(int)
		isDirect, _ := e.Metadata["direct_dependency"].(bool)
		if depth > 1 || !isDirect {
			t.Errorf("transitive entity %q survived lockfile cap (depth=%d direct=%v)",
				e.EntityName, depth, isDirect)
		}
	}
}

// TestMaterializeLockfileVariableCapPreservesAllDirectWhenUnderCap verifies
// that when the lockfile variable count is at or below MaxLockfileVariableEntities,
// all entities are kept (no loss of dependency truth).
func TestMaterializeLockfileVariableCapPreservesAllDirectWhenUnderCap(t *testing.T) {
	t.Parallel()

	entities := make([]Entity, 0, 5)
	for i := 0; i < 5; i++ {
		entities = append(entities, Entity{
			Name:       "dep-" + itoa(i),
			LineNumber: i + 1,
			Metadata: map[string]any{
				"lockfile":         true,
				"config_kind":      "dependency",
				"dependency_depth": 1,
				"direct_dependency": true,
			},
		})
	}

	got, err := Materialize(Input{
		RepoID:       "repository:r_test0002",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "yarn.lock",
				Language: "node_lockfile",
				EntityBuckets: map[string][]Entity{
					"variables": entities,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if got, want := len(got.Entities), 5; got != want {
		t.Fatalf("entity count = %d, want %d (all under cap must survive)", got, want)
	}
}

// TestMaterializeLockfileVariableCapNonLockfileVariablesUnaffected verifies
// that the lockfile cap does not affect non-lockfile Variable entities (e.g.,
// tsconfig paths, Gradle variables, package.json scripts).
func TestMaterializeLockfileVariableCapNonLockfileVariablesUnaffected(t *testing.T) {
	t.Parallel()

	// Non-lockfile variables: no lockfile=true metadata.
	count := MaxLockfileVariableEntities * 2
	entities := make([]Entity, 0, count)
	for i := 0; i < count; i++ {
		entities = append(entities, Entity{
			Name:       "var-" + itoa(i),
			LineNumber: i + 1,
			// No lockfile key in Metadata — ordinary code variable.
			Metadata: map[string]any{
				"config_kind": "dependency",
			},
		})
	}

	// Non-lockfile file: the per-file entity cap (MaxFileEntityCount) is
	// intentionally much higher than MaxLockfileVariableEntities, so these
	// entities should all survive.
	got, err := Materialize(Input{
		RepoID:       "repository:r_test0003",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "build.gradle",
				Language: "gradle",
				EntityBuckets: map[string][]Entity{
					"variables": entities,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if got, want := len(got.Entities), count; got != want {
		t.Fatalf("non-lockfile entity count = %d, want %d (cap must not apply)",
			got, want)
	}
}

// itoa is a minimal int-to-string helper so this test file has no external
// imports beyond testing.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
