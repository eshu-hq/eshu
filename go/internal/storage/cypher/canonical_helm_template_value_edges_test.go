// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestHelmTemplateValueEdgeStatementsResolvesUsageToDefinition proves a
// HelmTemplateValueUsage entity resolves to the HelmValueDefinition with the
// same dotted path within the same chart, producing one REFERENCES edge row
// carrying the HELM_TEMPLATE_VALUE_REFERENCE evidence kind.
func TestHelmTemplateValueEdgeStatementsResolvesUsageToDefinition(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		// Not a first projection: the two retracts precede the MERGE. The
		// first-generation skip is covered by
		// TestHelmTemplateValueEdgeStatementsSkipsRetractOnFirstGeneration.
		FirstGeneration: false,
		Entities: []projector.EntityRow{
			{
				EntityID:   "def-image-repo",
				Label:      "HelmValueDefinition",
				EntityName: "image.repository",
				FilePath:   "/repo/charts/webapp/values.yaml",
			},
			{
				EntityID:   "def-replicas",
				Label:      "HelmValueDefinition",
				EntityName: "replicaCount",
				FilePath:   "/repo/charts/webapp/values.yaml",
			},
			{
				EntityID:   "use-image-repo",
				Label:      "HelmTemplateValueUsage",
				EntityName: "image.repository",
				FilePath:   "/repo/charts/webapp/templates/deployment.yaml",
			},
			{
				EntityID:   "use-replicas",
				Label:      "HelmTemplateValueUsage",
				EntityName: "replicaCount",
				FilePath:   "/repo/charts/webapp/templates/deployment.yaml",
			},
		},
	}

	stmts := helmTemplateValueEdgeStatements(mat)
	// A legacy-REFERENCES retract and the dedicated-type retract precede the
	// MERGE upsert (legacy migration, then current-type retract, then upsert).
	if len(stmts) != 3 {
		t.Fatalf("statement count = %d, want 3 (legacy retract + HELM_VALUE_REFERENCE retract + upsert)", len(stmts))
	}
	legacyRetract := stmts[0]
	if legacyRetract.Operation != OperationCanonicalRetract {
		t.Fatalf("stmts[0].Operation = %v, want retract", legacyRetract.Operation)
	}
	// The legacy migration retract removes pre-#4476 Helm edges written on the
	// shared REFERENCES type, scoped by call_kind so it never touches code-symbol
	// REFERENCES; it must be Drain-marked (autocommit).
	if !legacyRetract.Drain {
		t.Fatalf("legacy retract must be Drain-marked for autocommit routing")
	}
	if !strings.Contains(legacyRetract.Cypher, "[r:REFERENCES]") ||
		!strings.Contains(legacyRetract.Cypher, "r.call_kind = 'helm_template_value_reference'") {
		t.Fatalf("legacy retract must delete shared-REFERENCES Helm edges scoped by call_kind: %s", legacyRetract.Cypher)
	}

	retract := stmts[1]
	if retract.Operation != OperationCanonicalRetract {
		t.Fatalf("stmts[1].Operation = %v, want retract", retract.Operation)
	}
	// The dedicated-type retract must be Drain-marked so the phase-group executor
	// runs it as a standalone autocommit statement (it silently no-ops inside the
	// grouped ExecuteWrite transaction, #4476).
	if !retract.Drain {
		t.Fatalf("retract statement must be Drain-marked for autocommit routing")
	}
	// It must use the dedicated HELM_VALUE_REFERENCE type (small delete-index,
	// #4476) and stay scoped by call_kind so it never widens to another verb.
	if !strings.Contains(retract.Cypher, "[r:HELM_VALUE_REFERENCE]") {
		t.Fatalf("retract Cypher must match the dedicated HELM_VALUE_REFERENCE type: %s", retract.Cypher)
	}
	if !strings.Contains(retract.Cypher, "r.call_kind = 'helm_template_value_reference'") {
		t.Fatalf("retract Cypher must stay scoped by call_kind: %s", retract.Cypher)
	}
	upsert := stmts[2]
	if !strings.Contains(upsert.Cypher, "MERGE (u)-[r:HELM_VALUE_REFERENCE]->(d)") {
		t.Fatalf("upsert Cypher must MERGE the dedicated HELM_VALUE_REFERENCE type: %s", upsert.Cypher)
	}
	if !strings.Contains(upsert.Cypher, "r.source_tool = row.source_tool") {
		t.Fatalf("upsert Cypher missing source_tool SET: %s", upsert.Cypher)
	}
	rows, ok := upsert.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", upsert.Parameters["rows"])
	}
	if len(rows) != 2 {
		t.Fatalf("edge row count = %d, want 2", len(rows))
	}

	bySource := map[string]map[string]any{}
	for _, row := range rows {
		bySource[row["source_uid"].(string)] = row
	}
	imageRow, ok := bySource["use-image-repo"]
	if !ok {
		t.Fatalf("missing edge from use-image-repo; rows=%v", rows)
	}
	if imageRow["target_uid"] != "def-image-repo" {
		t.Errorf("image.repository target_uid = %v, want def-image-repo", imageRow["target_uid"])
	}
	kinds, ok := imageRow["evidence_kinds"].([]string)
	if !ok || len(kinds) != 1 || kinds[0] != "HELM_TEMPLATE_VALUE_REFERENCE" {
		t.Errorf("evidence_kinds = %v, want [HELM_TEMPLATE_VALUE_REFERENCE]", imageRow["evidence_kinds"])
	}
	if imageRow["source_tool"] != "helm" {
		t.Errorf("source_tool = %v, want helm", imageRow["source_tool"])
	}
}

// TestHelmTemplateValueEdgeStatementsSkipsRetractOnFirstGeneration proves the
// #4726 fix: on a scope's first generation the two retracts (the legacy
// shared-REFERENCES migration and the dedicated-type retract) are skipped
// because a first-ever projection has no prior Helm edges to retract, leaving
// only the MERGE upsert. This removes the legacy retract's 30s full-scan of the
// large shared REFERENCES type from the cold-ingest hot path while staying
// output-preserving (0 rows deleted whether run or skipped).
func TestHelmTemplateValueEdgeStatementsSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID:    "gen-1",
		FirstGeneration: true,
		Entities: []projector.EntityRow{
			{
				EntityID:   "def-image-repo",
				Label:      "HelmValueDefinition",
				EntityName: "image.repository",
				FilePath:   "/repo/charts/webapp/values.yaml",
			},
			{
				EntityID:   "use-image-repo",
				Label:      "HelmTemplateValueUsage",
				EntityName: "image.repository",
				FilePath:   "/repo/charts/webapp/templates/deployment.yaml",
			},
		},
	}

	stmts := helmTemplateValueEdgeStatements(mat)
	if len(stmts) != 1 {
		t.Fatalf("first-generation statement count = %d, want 1 (MERGE only, both retracts skipped)", len(stmts))
	}
	if stmts[0].Operation != OperationCanonicalUpsert {
		t.Fatalf("stmts[0].Operation = %v, want upsert (the MERGE)", stmts[0].Operation)
	}
	if !strings.Contains(stmts[0].Cypher, "MERGE (u)-[r:HELM_VALUE_REFERENCE]->(d)") {
		t.Fatalf("the only statement must be the MERGE upsert: %s", stmts[0].Cypher)
	}
	// No statement may carry the legacy shared-REFERENCES retract on first gen.
	for i, s := range stmts {
		if strings.Contains(s.Cypher, "[r:REFERENCES]") {
			t.Fatalf("stmts[%d] emits the legacy REFERENCES retract on first generation: %s", i, s.Cypher)
		}
	}
}

// TestHelmTemplateValueEdgeStatementsScopedToChart proves a usage only resolves
// to a definition in the SAME chart: a usage in chart A whose path matches a
// definition only present in chart B produces no edge.
func TestHelmTemplateValueEdgeStatementsScopedToChart(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			{
				EntityID:   "def-b",
				Label:      "HelmValueDefinition",
				EntityName: "image.tag",
				FilePath:   "/repo/charts/other/values.yaml",
			},
			{
				EntityID:   "use-a",
				Label:      "HelmTemplateValueUsage",
				EntityName: "image.tag",
				FilePath:   "/repo/charts/webapp/templates/deployment.yaml",
			},
		},
	}

	stmts := helmTemplateValueEdgeStatements(mat)
	if len(stmts) != 0 {
		t.Fatalf("statement count = %d, want 0 (no cross-chart resolution)", len(stmts))
	}
}

// TestHelmTemplateValueEdgeStatementsNoHelmEntities proves the builder is a no-op
// for repos with no Helm template-value entities.
func TestHelmTemplateValueEdgeStatementsNoHelmEntities(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			{EntityID: "fn-1", Label: "Function", EntityName: "main", FilePath: "/repo/main.go"},
		},
	}
	if stmts := helmTemplateValueEdgeStatements(mat); len(stmts) != 0 {
		t.Fatalf("statement count = %d, want 0", len(stmts))
	}
}
