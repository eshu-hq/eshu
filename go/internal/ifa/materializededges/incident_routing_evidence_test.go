// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/incident"
)

// TestIncidentRoutingEvidenceFamilyReproducesExpectedEdges is the
// incident_routing_evidence vacuity guard's own proof: the production
// builder over the Odù's facts plus production-parser-derived Declared,
// projected through ExtractIncidentRoutingEvidenceRows and mapped slot to
// relationship type through the writer's own registry, reproduces the
// hand-derived 3-edge set exactly — across all three registry types, with
// the tombstoned applied fact contributing no edge.
func TestIncidentRoutingEvidenceFamilyReproducesExpectedEdges(t *testing.T) {
	t.Parallel()

	odu := ifa.IncidentRoutingEvidenceFamilyOdu().Odu
	ok, detail := resolveIncidentRoutingEvidenceMaterializedEdges(
		odu,
		incidentRoutingEvidenceExpectedEdgesPath(repoRootDir(t)),
	)
	if !ok {
		t.Fatalf("incident_routing_evidence guard reported uncovered: %s", detail)
	}
	t.Logf("covered: %s", detail)
}

// TestIncidentRoutingEvidenceGuardRedsWithoutDeclared pins the RED half of
// the guard's contract: the Odù's facts alone, with no Declared evidence,
// project zero rows, so a fixture that lost its Terraform-source declaration
// can never reproduce the 3-edge set. This is the failing state the
// Terraform-fixture derivation in the guard exists to close.
func TestIncidentRoutingEvidenceGuardRedsWithoutDeclared(t *testing.T) {
	t.Parallel()

	odu := ifa.IncidentRoutingEvidenceFamilyOdu().Odu
	inputs, quarantined, err := incident.BuildIncidentRoutingEvidenceInputs(
		incident.IncidentRoutingRawEvidence{Facts: odu.Facts},
	)
	if err != nil {
		t.Fatalf("BuildIncidentRoutingEvidenceInputs() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0 on the all-valid fixture", len(quarantined))
	}
	var rows []map[string]any
	for _, input := range inputs {
		inputRows, _ := incident.ExtractIncidentRoutingEvidenceRows(input)
		rows = append(rows, inputRows...)
	}
	if len(rows) != 0 {
		t.Fatalf("rows without Declared = %d, want 0: %#v", len(rows), rows)
	}
}
