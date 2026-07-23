// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func TestProvenanceEdgeWriterWritePublishesEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)

	if err := writer.WritePublishesEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/package-ownership"); err != nil {
		t.Fatalf("WritePublishesEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestProvenanceEdgeWriterWritePublishesPackageMatchMatchMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)
	rows := []map[string]any{
		{"repository_id": "repo-1", "package_id": "pkg-1"},
	}

	if err := writer.WritePublishesEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/package-ownership"); err != nil {
		t.Fatalf("WritePublishesEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (repo:Repository {id: row.repository_id})") {
		t.Fatalf("cypher must MATCH the repository by id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:Package {uid: row.package_id})") {
		t.Fatalf("cypher must MATCH the package by uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (repo:Repository") || strings.Contains(cypher, "MERGE (target:Package") {
		t.Fatalf("cypher must not MERGE (fabricate) endpoint nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (repo)-[rel:PUBLISHES]->(target)") {
		t.Fatalf("edge MERGE must use the static PUBLISHES relationship type:\n%s", cypher)
	}

	rowsParam, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rowsParam) != 1 {
		t.Fatalf("rows parameter = %#v, want one row", executor.calls[0].Parameters["rows"])
	}
	row := rowsParam[0]
	if row["scope_id"] != "scope-1" || row["generation_id"] != "gen-1" || row["evidence_source"] != "reducer/package-ownership" {
		t.Fatalf("row provenance stamps missing: %#v", row)
	}
}

func TestProvenanceEdgeWriterWritePublishesBucketsPackageVersionSeparately(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)
	rows := []map[string]any{
		{"repository_id": "repo-1", "package_id": "pkg-1"},
		{"repository_id": "repo-2", "version_id": "ver-1"},
	}

	if err := writer.WritePublishesEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/package-publication"); err != nil {
		t.Fatalf("WritePublishesEdges returned error: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 (one per target label)", len(executor.calls))
	}

	var sawPackage, sawVersion bool
	for _, call := range executor.calls {
		if strings.Contains(call.Cypher, "MATCH (target:Package {uid: row.package_id})") {
			sawPackage = true
		}
		if strings.Contains(call.Cypher, "MATCH (target:PackageVersion {uid: row.version_id})") {
			sawVersion = true
		}
	}
	if !sawPackage {
		t.Fatal("expected one statement targeting Package")
	}
	if !sawVersion {
		t.Fatal("expected one statement targeting PackageVersion")
	}
}

func TestProvenanceEdgeWriterWriteBuiltFromMatchesByDigest(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)
	rows := []map[string]any{
		{"digest": "sha256:deadbeef", "repository_id": "repo-1"},
	}

	if err := writer.WriteBuiltFromEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/container-image-identity"); err != nil {
		t.Fatalf("WriteBuiltFromEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (img:ContainerImage {digest: row.digest})") {
		t.Fatalf("cypher must MATCH the container image by digest (no manifest uid on the decision):\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (repo:Repository {id: row.repository_id})") {
		t.Fatalf("cypher must MATCH the repository by id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (img)-[rel:BUILT_FROM]->(repo)") {
		t.Fatalf("edge MERGE must use the static BUILT_FROM relationship type:\n%s", cypher)
	}
}

func TestProvenanceEdgeWriterRetractPublishesUsesSequentialExecuteNeverGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)

	if err := writer.RetractPublishesEdges(context.Background(), "scope-1", "gen-1", "reducer/package-ownership"); err != nil {
		t.Fatalf("RetractPublishesEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("groupCalls = %d, want 0 -- retract must never use ExecuteGroup (NornicDB grouped-DELETE bug)", len(executor.groupCalls))
	}
	if len(executor.executeCalls) != 1 {
		t.Fatalf("executeCalls = %d, want 1", len(executor.executeCalls))
	}
	cypher := executor.executeCalls[0].Cypher
	if !strings.Contains(cypher, "rel.scope_id = $scope_id") || !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract cypher must scope by scope_id+evidence_source:\n%s", cypher)
	}
	if executor.executeCalls[0].Parameters["scope_id"] != "scope-1" {
		t.Fatalf("scope_id param = %v, want scope-1", executor.executeCalls[0].Parameters["scope_id"])
	}
	if executor.executeCalls[0].Parameters["evidence_source"] != "reducer/package-ownership" {
		t.Fatalf("evidence_source param = %v, want reducer/package-ownership", executor.executeCalls[0].Parameters["evidence_source"])
	}
}

func TestProvenanceEdgeWriterRetractBuiltFromUsesSequentialExecuteNeverGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)

	if err := writer.RetractBuiltFromEdges(context.Background(), "scope-1", "gen-1", "reducer/container-image-identity"); err != nil {
		t.Fatalf("RetractBuiltFromEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("groupCalls = %d, want 0 -- retract must never use ExecuteGroup (NornicDB grouped-DELETE bug)", len(executor.groupCalls))
	}
	if len(executor.executeCalls) != 1 {
		t.Fatalf("executeCalls = %d, want 1", len(executor.executeCalls))
	}
	cypher := executor.executeCalls[0].Cypher
	if !strings.Contains(cypher, ":ContainerImage") || !strings.Contains(cypher, ":BUILT_FROM") || !strings.Contains(cypher, ":Repository") {
		t.Fatalf("retract cypher must anchor ContainerImage-BUILT_FROM->Repository:\n%s", cypher)
	}
}

func TestProvenanceEdgeWriterRetractEmptyScopeIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)

	if err := writer.RetractPublishesEdges(context.Background(), "", "gen-1", "reducer/package-ownership"); err != nil {
		t.Fatalf("RetractPublishesEdges returned error: %v", err)
	}
	if err := writer.RetractBuiltFromEdges(context.Background(), "", "gen-1", "reducer/container-image-identity"); err != nil {
		t.Fatalf("RetractBuiltFromEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope", len(executor.calls))
	}
}

func TestProvenanceEdgeWriterWriteUsesAtomicGroupWhenAvailable(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)
	rows := []map[string]any{
		{"repository_id": "repo-1", "package_id": "pkg-1"},
	}

	if err := writer.WritePublishesEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/package-ownership"); err != nil {
		t.Fatalf("WritePublishesEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("groupCalls = %d, want 1", len(executor.groupCalls))
	}
	if len(executor.executeCalls) != 0 {
		t.Fatalf("executeCalls = %d, want 0 when GroupExecutor is available for upserts", len(executor.executeCalls))
	}
}
