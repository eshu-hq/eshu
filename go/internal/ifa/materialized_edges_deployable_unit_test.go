// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
