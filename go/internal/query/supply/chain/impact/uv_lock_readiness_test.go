// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
)

// readinessGapConfigKinds are the config_kind values the
// package_dependency_gap_active CTE turns into dependency_source unsupported
// targets. TestUVLockReadinessGapKindsMatchTheSQL pins them to the SQL text.
var readinessGapConfigKinds = []string{
	"vcs_dependency", "path_dependency", "url_dependency", "editable_dependency", "unsupported_dependency",
}

// TestUVLockReadinessGapKindsMatchTheSQL keeps readinessGapConfigKinds in step
// with the production CTE, so the chain test below cannot drift from it.
func TestUVLockReadinessGapKindsMatchTheSQL(t *testing.T) {
	t.Parallel()

	sql := listSupplyChainImpactReadinessQueryUnsupportedAndSource
	start := strings.Index(sql, "'entity_metadata'->>'config_kind' IN (")
	if start < 0 {
		t.Fatal("package_dependency_gap_active config_kind IN list not found")
	}
	list := sql[start : start+strings.Index(sql[start:], ")")]
	for _, kind := range readinessGapConfigKinds {
		if !strings.Contains(list, "'"+kind+"'") {
			t.Fatalf("config_kind %q is not in the readiness gap list %s", kind, list)
		}
	}
	if got := strings.Count(list, "_dependency'"); got != len(readinessGapConfigKinds) {
		t.Fatalf("readiness gap list has %d kinds, test models %d: %s", got, len(readinessGapConfigKinds), list)
	}
}

// uvLockUnsupportedTargets maps parsed uv.lock rows the way the readiness CTE
// does: a gap config_kind becomes one dependency_source target.
func uvLockUnsupportedTargets(t *testing.T, body string) []UnsupportedTarget {
	t.Helper()
	path := filepath.Join(t.TempDir(), "uv.lock")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	payload, err := pythondep.ParseUVLock(path)
	if err != nil {
		t.Fatalf("ParseUVLock() error = %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	var targets []UnsupportedTarget
	for _, row := range rows {
		kind, _ := row["config_kind"].(string)
		for _, gap := range readinessGapConfigKinds {
			if kind == gap {
				targets = append(targets, UnsupportedTarget{
					TargetKind:   UnsupportedTargetKindDependencySource,
					Reason:       kind + "_unsupported",
					Count:        1,
					Ecosystem:    "pypi",
					FeatureToken: kind,
				})
			}
		}
	}
	return targets
}

func uvLockReadiness(targets []UnsupportedTarget) ReadinessEnvelope {
	return BuildReadiness(
		TargetScope{RepositoryID: "repo://example/python-service"},
		nil,
		false,
		ReadinessSnapshot{
			EvidenceSources: []EvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 8, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 2, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageRegistry, FactCount: 2, Freshness: FreshnessLabelFresh},
			},
			UnsupportedTargets: targets,
		},
	)
}

// TestUVLockProjectRootDoesNotMakeReadinessUnsupported proves the chain from
// the uv.lock reader to the readiness state: a repository whose lock holds only
// PyPI pins plus the project itself (editable ".") and a workspace member is
// ready_zero_findings, while a genuine local path dependency still reads
// unsupported. Before the reader counted the root instead of emitting it,
// every uv-locked repository read unsupported because of its own project row.
func TestUVLockProjectRootDoesNotMakeReadinessUnsupported(t *testing.T) {
	t.Parallel()

	lock := "version = 1\nrevision = 3\n\n[manifest]\nmembers = [\"app\", \"lib\"]\n\n" +
		"[[package]]\nname = \"app\"\nversion = \"0.1.0\"\nsource = { editable = \".\" }\n\n" +
		"[[package]]\nname = \"lib\"\nversion = \"0.1.0\"\nsource = { editable = \"packages/lib\" }\n\n" +
		"[[package]]\nname = \"requests\"\nversion = \"2.31.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"

	if got := uvLockReadiness(uvLockUnsupportedTargets(t, lock)); got.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q (unsupported_targets %#v), want %q: the project and its members are not dependency gaps",
			got.State, got.UnsupportedTargets, ReadinessStateReadyZeroFindings)
	}

	// The row the reader used to emit for the project itself is a gap.
	rootRow := []UnsupportedTarget{{
		TargetKind: UnsupportedTargetKindDependencySource, Reason: "editable_dependency_unsupported",
		Count: 1, Ecosystem: "pypi", FeatureToken: "editable_dependency",
	}}
	if got := uvLockReadiness(rootRow); got.State != ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q for an emitted editable root row", got.State, ReadinessStateUnsupported)
	}

	vendored := lock + "\n[[package]]\nname = \"vendored\"\nversion = \"1.0.0\"\nsource = { path = \"../vendored\" }\n"
	if got := uvLockReadiness(uvLockUnsupportedTargets(t, vendored)); got.State != ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q: a local path dependency outside the workspace is a real gap", got.State, ReadinessStateUnsupported)
	}
}
