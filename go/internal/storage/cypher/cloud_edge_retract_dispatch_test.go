// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// No-group dispatch guards for the C-14 #4367 cloud-correlation retract fix.
//
// On the pinned NornicDB v1.1.11 a retract DELETE dispatched through
// ExecuteGroup / a managed transaction under-applies even for a single
// statement, while the identical statement run as an auto-commit transaction
// (Execute) deletes correctly (docs/public/reference/nornicdb-pitfalls.md).
// Each writer's Retract* method must therefore route through its
// dispatchRetract helper (sequential Execute, never ExecuteGroup).
//
// These guards use sqlSequentialRecordingExecutor (edge_writer_sql_retract_
// test.go), which implements GroupExecutor and records group calls, so a
// revert to the grouped dispatch() fails here. The writers' other retract
// unit tests use the plain recordingExecutor, which does NOT implement
// GroupExecutor — under it the grouped dispatch takes its sequential fallback
// and produces identical Execute calls, so those tests cannot detect a
// revert. They mirror
// TestEdgeWriterRetractEdgesSQLRelationshipRunsPerLabelStatementsSequentially.

import (
	"context"
	"strings"
	"testing"
)

// assertSingleSequentialRetract asserts the executor saw zero ExecuteGroup
// calls and exactly one sequential Execute call whose Cypher contains every
// expected fragment.
func assertSingleSequentialRetract(t *testing.T, executor *sqlSequentialRecordingExecutor, wantFragments ...string) {
	t.Helper()
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 (grouped DELETEs under-apply on NornicDB v1.1.11)", got)
	}
	if got := len(executor.calls); got != 1 {
		t.Fatalf("Execute calls = %d, want 1 sequential retract statement", got)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range wantFragments {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract cypher missing %q:\n%s", want, cypher)
		}
	}
}

func TestKubernetesCorrelationEdgeWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)
	if err := writer.RetractKubernetesCorrelationEdges(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/kubernetes-correlation",
	); err != nil {
		t.Fatalf("RetractKubernetesCorrelationEdges returned error: %v", err)
	}
	assertSingleSequentialRetract(t, executor,
		"MATCH (w:KubernetesWorkload)-[rel:RUNS_IMAGE]->()",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	)
}

func TestS3LogsToEdgeWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewS3LogsToEdgeWriter(executor, 0)
	if err := writer.RetractS3LogsToEdges(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/s3-logs-to",
	); err != nil {
		t.Fatalf("RetractS3LogsToEdges returned error: %v", err)
	}
	assertSingleSequentialRetract(t, executor,
		"MATCH (:CloudResource)-[rel:LOGS_TO]->(:CloudResource)",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	)
}

func TestS3ExternalPrincipalGrantWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewS3ExternalPrincipalGrantWriter(executor, 0)
	if err := writer.RetractS3ExternalPrincipalGrants(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/s3-external-principal-grant",
	); err != nil {
		t.Fatalf("RetractS3ExternalPrincipalGrants returned error: %v", err)
	}
	assertSingleSequentialRetract(t, executor,
		"MATCH (:CloudResource)-[rel:GRANTS_ACCESS_TO]->(:ExternalPrincipal)",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	)
}

func TestIAMInstanceProfileRoleEdgeWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewIAMInstanceProfileRoleEdgeWriter(executor, 0)
	if err := writer.RetractIAMInstanceProfileRoleEdges(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/iam-instance-profile-role",
	); err != nil {
		t.Fatalf("RetractIAMInstanceProfileRoleEdges returned error: %v", err)
	}
	assertSingleSequentialRetract(t, executor,
		"MATCH (:CloudResource)-[rel:HAS_ROLE]->(:CloudResource)",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	)
}
