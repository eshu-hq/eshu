// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestResolveDeployableUnitMaterializedEdgesReproducesExpectedSet pins the
// deployable_unit_edges vacuity guard against the family's hand-derived
// expected-edge-set fixture. It proves the guard runs the real
// DiscoverEvidence -> relationships.Resolve -> ExtractDeployableUnitCorrelationRows
// chain over the cataloged Odù's own facts and reproduces exactly the
// CORRELATES_DEPLOYABLE_UNIT edge the ArgoCD Application evidence implies --
// never a hand-authored resolved relationship.
func TestResolveDeployableUnitMaterializedEdgesReproducesExpectedSet(t *testing.T) {
	t.Parallel()

	odu := deployableUnitFamilyOdu().Odu
	ok, detail := resolveDeployableUnitMaterializedEdges(odu, deployableUnitFamilyExpectedEdgesPath(repoRootDir(t)))
	if !ok {
		t.Fatalf("resolveDeployableUnitMaterializedEdges() = (false, %q), want (true, ...)", detail)
	}
	if !strings.Contains(detail, odu.Name) {
		t.Fatalf("detail = %q, want it to name the odù %q", detail, odu.Name)
	}
}

// TestDeployableUnitFamilyResolvesThroughTheManifestResolver proves the
// vacuity guard is reachable by surface name through
// MaterializedEdgeOduResolver, not only by calling
// resolveDeployableUnitMaterializedEdges directly. This is the regression
// test for the #5993 defect: the guard existed and was fully unit-tested, but
// MaterializedEdgeOduResolver.Resolve's dispatch switch had no
// deployable_unit_edges case, so every coverage-manifest row naming this
// family resolved through the switch's default branch ("no vacuity guard
// registered") no matter how correct the guard itself was. Mirrors
// TestDocumentationFamilyResolvesThroughTheManifestResolver
// (materialized_edges_documentation_test.go).
func TestDeployableUnitFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + deployableUnitEdgesFamily,
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          deployableUnitFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for %s: %s", deployableUnitEdgesFamily, detail)
	}
	t.Logf("%s", detail)
}

// TestResolveDeployableUnitMaterializedEdgesRejectsWrongExpectedSet proves the
// guard is not vacuously true: an expected-edge fixture naming the wrong
// target repository must fail closed, not silently pass.
func TestResolveDeployableUnitMaterializedEdgesRejectsWrongExpectedSet(t *testing.T) {
	t.Parallel()

	odu := deployableUnitFamilyOdu().Odu
	wrongPath := filepath.Join(t.TempDir(), "wrong-expected-edges.json")
	wrongFixture := struct {
		Odu   string `json:"odu"`
		Edges []struct {
			RelationshipType string `json:"relationship_type"`
			SourceEntityID   string `json:"source_entity_id"`
			TargetEntityID   string `json:"target_entity_id"`
		} `json:"edges"`
	}{
		Odu: deployableUnitFamilyOduName,
		Edges: []struct {
			RelationshipType string `json:"relationship_type"`
			SourceEntityID   string `json:"source_entity_id"`
			TargetEntityID   string `json:"target_entity_id"`
		}{
			{RelationshipType: "CORRELATES_DEPLOYABLE_UNIT", SourceEntityID: deployableUnitFamilyAppRepoID, TargetEntityID: "not-the-real-deploy-repo"},
		},
	}
	raw, err := json.Marshal(wrongFixture)
	if err != nil {
		t.Fatalf("json.Marshal(wrongFixture) error = %v, want nil", err)
	}
	if err := os.WriteFile(wrongPath, raw, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v, want nil", wrongPath, err)
	}

	ok, detail := resolveDeployableUnitMaterializedEdges(odu, wrongPath)
	if ok {
		t.Fatalf("resolveDeployableUnitMaterializedEdges() = (true, %q), want (false, ...) for a deliberately wrong fixture", detail)
	}
}

// TestDeployableUnitAssertDropReasonsCatchesRejectedRowLeak proves
// deployableUnitAssertDropReasons is not vacuous: a rejected candidate's row
// leaking into the admitted set must fail, not silently pass.
func TestDeployableUnitAssertDropReasonsCatchesRejectedRowLeak(t *testing.T) {
	t.Parallel()

	rejectedRow := reducer.SharedProjectionIntentRow{
		RepositoryID: deployableUnitFamilyRejectedRepoID,
		Payload:      map[string]any{"admission_state": "rejected"},
	}
	noDeployRow := reducer.SharedProjectionIntentRow{
		RepositoryID: deployableUnitFamilyAdmittedNoDeployRepoID,
		Payload:      map[string]any{"admission_state": "admitted", "deployment_repo_id": ""},
	}
	rows := []reducer.SharedProjectionIntentRow{rejectedRow, noDeployRow}
	// Simulate the bug: the rejected row leaked into "admitted".
	admitted := []reducer.SharedProjectionIntentRow{rejectedRow}

	if detail := deployableUnitAssertDropReasons("odu:test", rows, admitted); detail == "" {
		t.Fatal("deployableUnitAssertDropReasons() = \"\", want a non-empty failure detail for a leaked rejected row")
	}
}

// TestDeployableUnitAssertDropReasonsCatchesMissingNegativeRows proves the
// guard fails closed when the fixture no longer carries the negative-case
// repositories at all, rather than treating their absence as vacuously fine.
func TestDeployableUnitAssertDropReasonsCatchesMissingNegativeRows(t *testing.T) {
	t.Parallel()

	if detail := deployableUnitAssertDropReasons("odu:test", nil, nil); detail == "" {
		t.Fatal("deployableUnitAssertDropReasons() = \"\", want a non-empty failure detail when neither negative-case row is present")
	}
}

// TestDeployableUnitAssertDropReasonsCatchesUnexpectedDeploymentRepoID proves
// the blank-deployment_repo_id drop reason is actually checked: a "no-deploy"
// fixture row that unexpectedly carries a deployment_repo_id no longer
// isolates that drop condition and must fail.
func TestDeployableUnitAssertDropReasonsCatchesUnexpectedDeploymentRepoID(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		{RepositoryID: deployableUnitFamilyRejectedRepoID, Payload: map[string]any{"admission_state": "rejected"}},
		{
			RepositoryID: deployableUnitFamilyAdmittedNoDeployRepoID,
			Payload:      map[string]any{"admission_state": "admitted", "deployment_repo_id": "repo-unexpected"},
		},
	}

	if detail := deployableUnitAssertDropReasons("odu:test", rows, nil); detail == "" {
		t.Fatal("deployableUnitAssertDropReasons() = \"\", want a non-empty failure detail when the no-deploy row unexpectedly carries a deployment_repo_id")
	}
}

// TestDeployableUnitAssertAdmittedEdgePropertiesCatchesWrongRulePack proves
// deployableUnitAssertAdmittedEdgeProperties is not vacuous: the bare MERGE
// key means a repo pair admitted for the WRONG reason (here, a different
// rule pack than argocd) must fail this check even though the edge's
// {type, source, target} triple is unchanged.
func TestDeployableUnitAssertAdmittedEdgePropertiesCatchesWrongRulePack(t *testing.T) {
	t.Parallel()

	admitted := []reducer.SharedProjectionIntentRow{
		{
			Payload: map[string]any{
				"admission_state":     "admitted",
				"evidence_type":       "deployable_unit_correlation",
				"resolution_source":   "reducer/deployable-unit-correlation",
				"generation_id":       deployableUnitFamilyGenerationID,
				"deployable_unit_key": deployableUnitFamilyAppRepoName,
				"correlation_key":     deployableUnitFamilyAppRepoID + ":" + deployableUnitFamilyAppRepoName,
				"rule_pack":           "dockerfile", // wrong: this Odù's positive case is argocd-sourced.
			},
		},
	}

	if detail := deployableUnitAssertAdmittedEdgeProperties("odu:test", admitted); detail == "" {
		t.Fatal("deployableUnitAssertAdmittedEdgeProperties() = \"\", want a non-empty failure detail for a rule_pack mismatch")
	}
}
