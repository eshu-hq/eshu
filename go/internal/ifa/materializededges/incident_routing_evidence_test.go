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

// TestIncidentRoutingEvidenceEnvelopeMirrorsParserOutput pins the Odù's
// hand-built declaration envelope to the production parser+decode seam the
// guard uses: the envelope's entity identity, start line, and metadata must
// decode (through DecodeIncidentRoutingDeclaredEvidence) to exactly the
// Declared row the guard derives from pagerduty.tf. A fixture or parser
// change that moves entity_name, start line, or any decoded metadata key
// fails here in `go test` instead of only in the live matrix.
func TestIncidentRoutingEvidenceEnvelopeMirrorsParserOutput(t *testing.T) {
	t.Parallel()

	expectedEdgesPath := incidentRoutingEvidenceExpectedEdgesPath(repoRootDir(t))
	declared, problem := incidentRoutingEvidenceDeclaredFromTerraformFixture(expectedEdgesPath)
	if problem != "" {
		t.Fatalf("fixture decode failed: %s", problem)
	}
	if len(declared) != 1 {
		t.Fatalf("declared rows = %d, want 1", len(declared))
	}

	odu := ifa.IncidentRoutingEvidenceFamilyOdu().Odu
	var envelope map[string]any
	for _, fact := range odu.Facts {
		if kind, _ := fact.Payload["graph_kind"].(string); kind == "content_entity" {
			envelope = fact.Payload
		}
	}
	if envelope == nil {
		t.Fatal("odù carries no content_entity envelope")
	}
	entityName, _ := envelope["entity_name"].(string)
	startLine, _ := envelope["start_line"].(int)
	metadata, _ := envelope["entity_metadata"].(map[string]any)
	// start_line may decode as int64/float64 through a JSON round trip.
	if startLine == 0 {
		switch line := envelope["start_line"].(type) {
		case int64:
			startLine = int(line)
		case float64:
			startLine = int(line)
		}
	}
	entityID, _ := envelope["entity_id"].(string)
	repoID, _ := envelope["repo_id"].(string)
	relativePath, _ := envelope["relative_path"].(string)
	reddecoded := incident.DecodeIncidentRoutingDeclaredEvidence(
		entityID, repoID, relativePath, entityName, startLine, metadata,
	)
	if reddecoded != declared[0] {
		t.Fatalf("envelope decodes to %+v, want parser output %+v", reddecoded, declared[0])
	}
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
