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
	// One generation-scoped retract precedes the MERGE upsert.
	if len(stmts) != 2 {
		t.Fatalf("statement count = %d, want 2 (retract + REFERENCES upsert)", len(stmts))
	}
	retract := stmts[0]
	if retract.Operation != OperationCanonicalRetract {
		t.Fatalf("stmts[0].Operation = %v, want retract", retract.Operation)
	}
	upsert := stmts[1]
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
