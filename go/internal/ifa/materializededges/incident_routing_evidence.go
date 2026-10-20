// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/parser/hcl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/incident"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// incidentRoutingEvidenceFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in the multi-type
// direct table (cypher/edge/materialized) and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const incidentRoutingEvidenceFamily = "incident_routing_evidence"

// incidentRoutingEvidenceExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const incidentRoutingEvidenceExpectedEdgesRelPath = "go/internal/ifa/testdata/incidentroutingevidence/ifa-incident-routing-evidence-family-expected-edges.json"

// incidentRoutingEvidenceTerraformFixtureName is the PagerDuty module fixture
// the guard runs the production HCL parser over. It lives beside the
// expected-edge fixture so the guard derives it from the expected-edge path
// without a second repoRoot-anchored constant to drift.
const incidentRoutingEvidenceTerraformFixtureName = "pagerduty.tf"

// incidentRoutingEvidenceExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func incidentRoutingEvidenceExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, incidentRoutingEvidenceExpectedEdgesRelPath)
}

// resolveIncidentRoutingEvidenceMaterializedEdges is
// incident_routing_evidence's named vacuity guard (#6228).
//
// Unlike every sibling guard, the Odù carries facts only: the declared
// PagerDuty half is content-entity metadata, never a fact envelope, so no
// Odù could carry it. The guard derives Declared by running the PRODUCTION
// HCL parser over the committed pagerduty.tf fixture into
// pagerduty_declarations rows and decoding each through the production
// reducer/incident seam — never a hand-built IncidentRoutingEvidenceInput.
// The builder, the extractor, and the slot-to-relationship-type map are all
// production code, so the fixture proves the seam the handler runs.
//
// A quarantined fact is fatal rather than skipped, for the same reason as
// every sibling guard: a fixture that stopped decoding against the incident
// payload contracts cannot prove the set it names, and the surviving facts
// would understate its own claim.
func resolveIncidentRoutingEvidenceMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, incidentRoutingEvidenceFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	declared, problem := incidentRoutingEvidenceDeclaredFromTerraformFixture(expectedEdgesPath)
	if problem != "" {
		return false, fmt.Sprintf("odù %q: %s", odu.Name, problem)
	}
	inputs, quarantined, err := incident.BuildIncidentRoutingEvidenceInputs(
		incident.IncidentRoutingRawEvidence{Facts: odu.Facts, Declared: declared},
	)
	if err != nil {
		return false, fmt.Sprintf("odù %q: BuildIncidentRoutingEvidenceInputs failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the incident payload contracts, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(inputs) == 0 {
		return false, fmt.Sprintf("odù %q: BuildIncidentRoutingEvidenceInputs produced zero incident packets; this fixture cannot prove anything", odu.Name)
	}

	var rows []map[string]any
	for _, input := range inputs {
		inputRows, _ := incident.ExtractIncidentRoutingEvidenceRows(input)
		rows = append(rows, inputRows...)
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractIncidentRoutingEvidenceRows produced zero routing rows; without Declared convergence (or with a tombstone swallowing the packet) this fixture proves nothing", odu.Name)
	}

	actual, problem := incidentRoutingEvidenceRowsToExpectedEdges(rows, registry)
	if problem != "" {
		return false, fmt.Sprintf("odù %q: %s", odu.Name, problem)
	}
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, incidentRoutingEvidenceFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: production builder plus extractor reproduces the expected %d-edge incident-routing set exactly across all %d registry type(s) and %d fact envelope(s); the tombstoned applied fact produced no spurious row",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// incidentRoutingEvidenceDeclaredFromTerraformFixture runs the production
// HCL parser over the pagerduty.tf fixture committed beside the expected-edge
// set and decodes every pagerduty_declarations row through the production
// reducer/incident seam.
//
// The parser row feeds the decode as its metadata map directly. Production
// strips reserved keys (name, line_number, lang, ...) into entity columns
// before persisting metadata, but the decode reads only non-reserved keys,
// so the stripped and unstripped maps decode identically — passing the row
// through avoids duplicating the collector's reserved-key set as a second
// copy that could drift. The entity identity (row name) and start line come
// from the same row, exactly as the content-entity projection reads them;
// only the database-assigned entity id and the repository scope
// (repo/relative path) are fixture literals, hand-set at the RawEvidence
// loader-output seam per the accepted practice in
// routing_materialization_test.go.
func incidentRoutingEvidenceDeclaredFromTerraformFixture(
	expectedEdgesPath string,
) ([]incident.IncidentRoutingDeclaredEvidence, string) {
	fixturePath := filepath.Join(filepath.Dir(expectedEdgesPath), incidentRoutingEvidenceTerraformFixtureName)
	payload, err := hcl.Parse(fixturePath, false, shared.Options{})
	if err != nil {
		return nil, fmt.Sprintf("parse terraform fixture %s: %v", fixturePath, err)
	}
	rows, _ := payload["pagerduty_declarations"].([]map[string]any)
	if len(rows) == 0 {
		return nil, fmt.Sprintf("terraform fixture %s produced zero pagerduty_declarations rows; without a declaration the guard can only prove the empty set", fixturePath)
	}
	declared := make([]incident.IncidentRoutingDeclaredEvidence, 0, len(rows))
	for index, row := range rows {
		entityName, _ := row["name"].(string)
		if strings.TrimSpace(entityName) == "" {
			return nil, fmt.Sprintf("terraform fixture %s row %d carries no entity name", fixturePath, index)
		}
		declared = append(declared, incident.DecodeIncidentRoutingDeclaredEvidence(
			"declared-entity-6228",
			"repo-observability",
			"pagerduty/main.tf",
			entityName,
			pagerdutyDeclarationStartLine(row),
			row,
		))
	}
	return declared, ""
}

// pagerdutyDeclarationStartLine reads the parser row's line_number in
// whatever numeric form the row carries it.
func pagerdutyDeclarationStartLine(row map[string]any) int {
	switch line := row["line_number"].(type) {
	case int:
		return line
	case int64:
		return int(line)
	case float64:
		return int(line)
	default:
		return 0
	}
}

// incidentRoutingEvidenceRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs: the incident
// node uid as source, the routing row uid as target, and the relationship
// type through the writer's own slot-to-type registry — the same map the
// write path screens each row against, so a slot the writer would reject
// fails here instead of materializing.
//
// slot, source_class, and truth_label are SET on the relationship after the
// MERGE, deliberately outside the relationship key, and are asserted here as
// mutable properties so a row that stopped carrying its slot cannot pass on
// endpoint identity alone. scope_id, generation_id, and evidence_source are
// stamped by the WRITER from per-run intent arguments, not carried on the
// extractor's rows, so no static set can pin them; their stamping is covered
// by the cypher writer unit test asserting the annotated row contents.
func incidentRoutingEvidenceRowsToExpectedEdges(
	rows []map[string]any,
	registry map[string]struct{},
) ([]ExpectedEdge, string) {
	slotTypes := cypher.IncidentRoutingEvidenceRelationshipTypes()
	edges := make([]ExpectedEdge, 0, len(rows))
	for index, row := range rows {
		slot, _ := row["slot"].(string)
		relType, ok := slotTypes[slot]
		if !ok {
			return nil, fmt.Sprintf(
				"extractor row %d carries slot %q, which the writer's closed slot vocabulary does not contain; the write path would reject it rather than materialize it",
				index, slot,
			)
		}
		if _, ok := registry[relType]; !ok {
			return nil, fmt.Sprintf(
				"slot %q maps to relationship type %q, which the family's registered edge-type set does not contain; the fixture cannot sanction an edge the registry disowns",
				slot, relType,
			)
		}
		source, _ := row["incident_uid"].(string)
		target, _ := row["uid"].(string)
		slotProp, _ := row["slot"].(string)
		sourceClass, _ := row["source_class"].(string)
		truthLabel, _ := row["truth_label"].(string)
		if strings.TrimSpace(source) == "" || strings.TrimSpace(target) == "" ||
			strings.TrimSpace(slotProp) == "" || strings.TrimSpace(sourceClass) == "" ||
			strings.TrimSpace(truthLabel) == "" {
			return nil, fmt.Sprintf(
				"extractor row %d is missing an edge identity or asserted property: %#v",
				index, row,
			)
		}
		edges = append(edges, ExpectedEdge{
			RelationshipType: relType,
			SourceEntityID:   source,
			TargetEntityID:   target,
			Properties: map[string]string{
				"slot":         slotProp,
				"source_class": sourceClass,
				"truth_label":  truthLabel,
			},
		})
	}
	return edges, ""
}
