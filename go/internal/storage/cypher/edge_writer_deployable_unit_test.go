// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesDeployableUnitDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "intent-1",
			RepositoryID: "repo-edge-api",
			GenerationID: "generation-1",
			Payload: map[string]any{
				"repo_id":             "repo-edge-api",
				"deployment_repo_id":  "repo-deployments",
				"deployable_unit_key": "edge-api",
				"correlation_key":     "repo-edge-api:edge-api",
				"confidence":          0.94,
				"evidence_count":      4,
				"evidence_kinds":      []string{"repository_identity", "deployable_unit_key", "deployment_repo", "argocd_application_source"},
				"generation_id":       "generation-1",
				"rule_pack":           "argocd",
				"admission_state":     "admitted",
				"reason":              "admitted deployable unit correlation",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainDeployableUnitEdges, rows, "reducer/deployable-unit-correlation")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	call := executor.calls[0]
	for _, want := range []string{
		"MATCH (source_repo:Repository {id: row.repo_id})",
		"MATCH (deployment_repo:Repository {id: row.deployment_repo_id})",
		"MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)",
		"rel.relationship_type = 'CORRELATES_DEPLOYABLE_UNIT'",
		"rel.deployable_unit_key = row.deployable_unit_key",
	} {
		if !strings.Contains(call.Cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, call.Cypher)
		}
	}
	rowsOut, ok := call.Parameters["rows"].([]map[string]any)
	if !ok || len(rowsOut) != 1 {
		t.Fatalf("rows = %#v, want one batch row", call.Parameters["rows"])
	}
	for key, want := range map[string]any{
		"repo_id":             "repo-edge-api",
		"deployment_repo_id":  "repo-deployments",
		"deployable_unit_key": "edge-api",
		"correlation_key":     "repo-edge-api:edge-api",
		"relationship_type":   "CORRELATES_DEPLOYABLE_UNIT",
		"evidence_source":     "reducer/deployable-unit-correlation",
		"generation_id":       "generation-1",
		"admission_state":     "admitted",
	} {
		if got := rowsOut[0][key]; got != want {
			t.Fatalf("row[%s] = %#v, want %#v", key, got, want)
		}
	}
	wantKinds := []string{"repository_identity", "deployable_unit_key", "deployment_repo", "argocd_application_source"}
	if got := rowsOut[0]["evidence_kinds"]; !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("evidence_kinds = %#v, want %#v", got, wantKinds)
	}
}

func TestEdgeWriterRetractEdgesDeployableUnitDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{
		{RepositoryID: "repo-edge-api", Payload: map[string]any{"repo_id": "repo-edge-api"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainDeployableUnitEdges, rows, "reducer/deployable-unit-correlation")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "CORRELATES_DEPLOYABLE_UNIT") {
		t.Fatalf("retract cypher missing deployable-unit token: %s", executor.calls[0].Cypher)
	}
}
