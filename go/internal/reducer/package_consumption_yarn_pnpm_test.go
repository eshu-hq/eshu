package reducer

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildPackageConsumptionDecisionsAdmitsYarnLockEvidence pins the
// acceptance criterion from issue #644: a yarn.lock parser fact carrying
// the canonical npm package_manager and the explicit yarn flavor must
// match the npm package registry identity so vulnerability impact
// correlates yarn-only repos.
func TestBuildPackageConsumptionDecisionsAdmitsYarnLockEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 14, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:npm/lodash", "npm", "lodash", "", observedAt),
		packageSourceRepositoryFact("repo-yarn", "yarn-web", "https://github.com/acme/yarn-web", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-yarn",
			"yarn-web",
			"yarn.lock",
			"lodash",
			"npm",
			"4.17.21",
			observedAt,
			map[string]any{
				"section":                "yarn.lock",
				"lockfile":               true,
				"lockfile_format":        "yarn-classic",
				"package_manager_flavor": "yarn",
				"dependency_path":        []any{"lodash"},
				"dependency_depth":       1,
				"direct_dependency":      true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Ecosystem, "npm"; got != want {
		t.Fatalf("Ecosystem = %q, want %q - yarn flavor must keep canonical npm ecosystem identity", got, want)
	}
	if got, want := decision.PackageID, "pkg:npm/lodash"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.RelativePath, "yarn.lock"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if got, want := decision.DependencyRange, "4.17.21"; got != want {
		t.Fatalf("DependencyRange = %q, want %q (lockfile exact version)", got, want)
	}
	if !reflect.DeepEqual(decision.DependencyPath, []string{"lodash"}) {
		t.Fatalf("DependencyPath = %#v, want [lodash] for direct yarn dep", decision.DependencyPath)
	}
	if decision.DirectDependency == nil || !*decision.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true for direct yarn lockfile dependency", decision.DirectDependency)
	}
}

// TestBuildPackageConsumptionDecisionsAdmitsPnpmLockEvidence proves the same
// for pnpm-lock.yaml evidence and pins transitive depth preservation.
func TestBuildPackageConsumptionDecisionsAdmitsPnpmLockEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 14, 30, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:npm/rollup", "npm", "rollup", "", observedAt),
		packageSourceRepositoryFact("repo-pnpm", "pnpm-web", "https://github.com/acme/pnpm-web", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-pnpm",
			"pnpm-web",
			"pnpm-lock.yaml",
			"rollup",
			"npm",
			"4.0.0",
			observedAt,
			map[string]any{
				"section":                "pnpm-package",
				"lockfile":               true,
				"lockfile_format":        "pnpm",
				"package_manager_flavor": "pnpm",
				"dependency_path":        []any{"vite", "rollup"},
				"dependency_depth":       2,
				"direct_dependency":      false,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Ecosystem, "npm"; got != want {
		t.Fatalf("Ecosystem = %q, want %q - pnpm flavor must keep canonical npm ecosystem identity", got, want)
	}
	if !reflect.DeepEqual(decision.DependencyPath, []string{"vite", "rollup"}) {
		t.Fatalf("DependencyPath = %#v, want [vite rollup] for transitive pnpm dep", decision.DependencyPath)
	}
	if got, want := decision.DependencyDepth, 2; got != want {
		t.Fatalf("DependencyDepth = %d, want %d", got, want)
	}
	if decision.DirectDependency == nil || *decision.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want false for transitive pnpm dependency", decision.DirectDependency)
	}
}

// TestBuildPackageConsumptionDecisionsAcceptsYarnBerryUnsupportedFlagButPreservesEvidence
// documents the intentional accept-and-flag behavior for unsupported Yarn
// Berry protocols (patch:, exec:, etc). If the lockfile flagged the row as
// unsupported, we still admit the underlying npm-identity row as a
// consumption decision; the unsupported flag rides along on the fact for
// downstream readiness, it is not used to suppress the decision. A future
// reviewer should not silently change this contract; if suppression becomes
// the desired behavior, the test name and assertions must flip too.
func TestBuildPackageConsumptionDecisionsAcceptsYarnBerryUnsupportedFlagButPreservesEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 15, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:npm/patched-lib", "npm", "patched-lib", "", observedAt),
		packageSourceRepositoryFact("repo-yarn-berry", "yarn-berry-web", "https://github.com/acme/yarn-berry-web", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-yarn-berry",
			"yarn-berry-web",
			"yarn.lock",
			"patched-lib",
			"npm",
			"1.0.0",
			observedAt,
			map[string]any{
				"section":                      "yarn.lock",
				"lockfile":                     true,
				"lockfile_format":              "yarn-berry",
				"package_manager_flavor":       "yarn",
				"lockfile_resolution_protocol": "patch",
				"lockfile_unsupported_feature": "patch",
				"dependency_path":              []any{"patched-lib"},
				"dependency_depth":             1,
				"direct_dependency":            true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d - patch: lockfile entries still produce one decision because the package_id and version are real", got, want)
	}
	if got, want := decisions[0].DependencyRange, "1.0.0"; got != want {
		t.Fatalf("DependencyRange = %q, want %q (patched packages still pin a base version)", got, want)
	}
}
