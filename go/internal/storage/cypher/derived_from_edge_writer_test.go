// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// TestProvenanceEdgeWriterWriteDerivedFromMatchesBothEndpointsByDigest pins the
// #5460 join shape. DERIVED_FROM carries ContainerImage on BOTH ends and the
// identity decision never carries an OciImageManifest uid, so both endpoints
// must be matched by digest. Two MATCHes precede the MERGE so a row whose child
// or base node is absent produces no edge and no fabricated node (the
// missing-endpoint no-op contract, #5472).
func TestProvenanceEdgeWriterWriteDerivedFromMatchesBothEndpointsByDigest(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)
	rows := []map[string]any{
		{
			"digest":            "sha256:child",
			"base_digest":       "sha256:base",
			"attribution_basis": "repository_single_base",
		},
	}

	if err := writer.WriteDerivedFromEdges(
		context.Background(), rows, "scope-1", "gen-1", "reducer/container-image-base-image",
	); err != nil {
		t.Fatalf("WriteDerivedFromEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (img:ContainerImage {digest: row.digest})",
		"MATCH (base:ContainerImage {digest: row.base_digest})",
		"MERGE (img)-[rel:DERIVED_FROM]->(base)",
		"rel.attribution_basis = row.attribution_basis",
		"rel.source_tool = row.source_tool",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}

	rowsParam, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rowsParam) != 1 {
		t.Fatalf("rows parameter = %#v, want one row", executor.calls[0].Parameters["rows"])
	}
	if rowsParam[0]["source_tool"] != "oci" {
		t.Fatalf("source_tool = %v, want oci", rowsParam[0]["source_tool"])
	}
	// The evidence_kinds token is what lets a golden-corpus required-correlation
	// assertion isolate THIS domain's edges from any other domain that might
	// later share the DERIVED_FROM verb.
	kinds, ok := rowsParam[0]["evidence_kinds"].([]string)
	if !ok || len(kinds) != 1 || kinds[0] != "CONTAINER_IMAGE_DERIVED_FROM" {
		t.Fatalf("evidence_kinds = %#v, want [CONTAINER_IMAGE_DERIVED_FROM]", rowsParam[0]["evidence_kinds"])
	}
	if rowsParam[0]["scope_id"] != "scope-1" || rowsParam[0]["generation_id"] != "gen-1" {
		t.Fatalf("scope/generation not stamped: %#v", rowsParam[0])
	}
}

// TestProvenanceEdgeWriterRetractDerivedFromUsesSequentialExecuteNeverGroup
// pins the retract dispatch. On the pinned NornicDB a DELETE dispatched through
// a managed transaction under-applies even for a single statement, so the
// retract must run as a sequential auto-commit Execute.
func TestProvenanceEdgeWriterRetractDerivedFromUsesSequentialExecuteNeverGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)

	if err := writer.RetractDerivedFromEdges(
		context.Background(), "scope-1", "gen-1", "reducer/container-image-base-image",
	); err != nil {
		t.Fatalf("RetractDerivedFromEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("groupCalls = %d, want 0 -- retract must never use ExecuteGroup (NornicDB grouped-DELETE bug)", len(executor.groupCalls))
	}
	if len(executor.executeCalls) != 1 {
		t.Fatalf("executeCalls = %d, want 1", len(executor.executeCalls))
	}
	cypher := executor.executeCalls[0].Cypher
	if !strings.Contains(cypher, "(:ContainerImage)-[rel:DERIVED_FROM]->(:ContainerImage)") {
		t.Fatalf("retract cypher must anchor ContainerImage-DERIVED_FROM->ContainerImage:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.scope_id = $scope_id") ||
		!strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract cypher must scope by scope_id+evidence_source:\n%s", cypher)
	}
	if executor.executeCalls[0].Parameters["evidence_source"] != "reducer/container-image-base-image" {
		t.Fatalf("evidence_source param = %v", executor.executeCalls[0].Parameters["evidence_source"])
	}
}

// TestProvenanceEdgeWriterDerivedFromEmptyInputsAreNoOps proves neither an empty
// row set nor a blank scope reaches the executor. A blank-scope retract would
// otherwise DELETE every DERIVED_FROM edge this evidence_source owns across all
// scopes.
func TestProvenanceEdgeWriterDerivedFromEmptyInputsAreNoOps(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewProvenanceEdgeWriter(executor, 0)

	if err := writer.WriteDerivedFromEdges(
		context.Background(), nil, "scope-1", "gen-1", "reducer/container-image-base-image",
	); err != nil {
		t.Fatalf("WriteDerivedFromEdges(nil) returned error: %v", err)
	}
	if err := writer.RetractDerivedFromEdges(
		context.Background(), "", "gen-1", "reducer/container-image-base-image",
	); err != nil {
		t.Fatalf("RetractDerivedFromEdges(blank scope) returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0", len(executor.calls))
	}
}
