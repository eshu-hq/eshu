// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// cloudResourceEdgeRows mirrors the rows ExtractAWSRelationshipEdgeRows
// produces: endpoint uids, relationship/target type, and resolution mode. It
// deliberately omits scope_id/generation_id/evidence_source — those are
// reducer-scoped annotations the writer injects from its call arguments, not
// fields the resolution layer carries.
func cloudResourceEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        "src-" + string(rune('a'+i)),
			"target_uid":        "tgt-" + string(rune('a'+i)),
			"relationship_type": "USES_KMS_KEY",
			"target_type":       "aws_kms_key",
			"resolution_mode":   "arn",
		})
	}
	return rows
}

func TestCloudResourceEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestCloudResourceEdgeWriterUsesStaticRelationshipTypeMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(1), "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Two MATCHes before the MERGE guarantee a missing endpoint is a no-op,
	// never a fabricated node — the graceful-degradation contract from #805.
	if !strings.Contains(cypher, "MATCH (source:CloudResource {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH the source CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:CloudResource {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH the target CloudResource by uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (target:CloudResource") || strings.Contains(cypher, "MERGE (source:CloudResource") {
		t.Fatalf("cypher must not MERGE (fabricate) endpoint nodes:\n%s", cypher)
	}
	if strings.Contains(cypher, "{relationship_type: row.relationship_type}") {
		t.Fatalf("relationship_type must not live inside MERGE identity because NornicDB misses the relationship hot path:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:AWS_USES_KMS_KEY]->(target)") {
		t.Fatalf("edge MERGE must use the sanitized AWS relationship type as the Cypher relationship type:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.relationship_type = row.relationship_type") {
		t.Fatalf("edge must keep the original relationship_type as a property for API/readback truth:\n%s", cypher)
	}
}

func TestCloudResourceEdgeWriterSplitsSameEndpointByRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 500)
	rows := []map[string]any{
		{
			"source_uid":        "shared-source",
			"target_uid":        "shared-target",
			"relationship_type": "ec2_subnet_in_vpc",
			"target_type":       "aws_vpc",
			"resolution_mode":   "bare_id",
		},
		{
			"source_uid":        "shared-source",
			"target_uid":        "shared-target",
			"relationship_type": "ec2_subnet_routes_to_nat_gateway",
			"target_type":       "aws_nat_gateway",
			"resolution_mode":   "bare_id",
		},
	}

	if err := writer.WriteCloudResourceEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	stmts := executor.groupCalls[0]
	if len(stmts) != 2 {
		t.Fatalf("group statement count = %d, want one statement per AWS relationship type", len(stmts))
	}
	gotCypher := stmts[0].Cypher + "\n" + stmts[1].Cypher
	for _, want := range []string{
		"MERGE (source)-[rel:AWS_ec2_subnet_in_vpc]->(target)",
		"MERGE (source)-[rel:AWS_ec2_subnet_routes_to_nat_gateway]->(target)",
	} {
		if !strings.Contains(gotCypher, want) {
			t.Fatalf("missing relationship-type-specific MERGE %q in:\n%s", want, gotCypher)
		}
	}
}

func TestCloudResourceEdgeWriterPreservesRelationshipTypeCaseInIdentity(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 500)
	rows := []map[string]any{
		{
			"source_uid":        "shared-source",
			"target_uid":        "shared-target",
			"relationship_type": "uses_kms_key",
			"target_type":       "aws_kms_key",
			"resolution_mode":   "arn",
		},
		{
			"source_uid":        "shared-source",
			"target_uid":        "shared-target",
			"relationship_type": "USES_KMS_KEY",
			"target_type":       "aws_kms_key",
			"resolution_mode":   "arn",
		},
	}

	if err := writer.WriteCloudResourceEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	stmts := executor.groupCalls[0]
	if len(stmts) != 2 {
		t.Fatalf("group statement count = %d, want case-distinct relationship types to stay distinct", len(stmts))
	}
	gotCypher := stmts[0].Cypher + "\n" + stmts[1].Cypher
	for _, want := range []string{
		"MERGE (source)-[rel:AWS_USES_KMS_KEY]->(target)",
		"MERGE (source)-[rel:AWS_uses_kms_key]->(target)",
	} {
		if !strings.Contains(gotCypher, want) {
			t.Fatalf("missing case-preserving relationship MERGE %q in:\n%s", want, gotCypher)
		}
	}
}

func TestCloudResourceEdgeWriterRejectsUnsafeRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)
	rows := cloudResourceEdgeRows(1)
	rows[0]["relationship_type"] = "bad type`) DELETE n //"

	err := writer.WriteCloudResourceEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/aws-relationships")
	if err == nil {
		t.Fatal("WriteCloudResourceEdges returned nil, want unsafe relationship_type error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when relationship_type is unsafe", len(executor.calls))
	}
}

func TestCloudResourceEdgeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 2)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(5), "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	// 5 rows at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestCloudResourceEdgeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 2)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(5), "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestCloudResourceEdgeWriterAnnotatesScopeGenerationEvidence(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(1), "scope-1", "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	// The resolution layer does not carry these reducer-scoped fields; the writer
	// must inject them from its call arguments so the persisted edge actually
	// carries scope_id/generation_id (else scope-scoped retract is a silent
	// no-op) and evidence_source (else cross-writer retract isolation breaks).
	if got := rows[0]["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id = %v, want scope-1 (injected by writer for scope-scoped retract)", got)
	}
	if got := rows[0]["generation_id"]; got != "gen-1" {
		t.Fatalf("generation_id = %v, want gen-1 (injected by writer)", got)
	}
	if got := rows[0]["evidence_source"]; got != "reducer/aws-relationships" {
		t.Fatalf("evidence_source = %v, want reducer/aws-relationships", got)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher must persist %q for retract scoping:\n%s", want, cypher)
		}
	}
}

func TestCloudResourceEdgeWriterRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceEdges(
		context.Background(),
		[]string{"scope-1"},
		"gen-1",
		"reducer/aws-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 retract statement", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if strings.Contains(cypher, "[rel:AWS_RELATIONSHIP]") {
		t.Fatalf("retract must not target only the legacy AWS_RELATIONSHIP type after writes use relationship-type-specific edges:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (source:CloudResource)-[rel]->(:CloudResource)") {
		t.Fatalf("retract must target all reducer-owned CloudResource relationships for the scope:\n%s", cypher)
	}
	// The retract MUST filter on the edge's own scope_id, not the endpoint
	// node's. CloudResource nodes are cross-scope canonical and carry no
	// scope_id property, so a source.scope_id predicate matches nothing and the
	// retract becomes a silent no-op that leaks stale edges across generations.
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must filter by the edge scope_id (rel.scope_id IN $scope_ids):\n%s", cypher)
	}
	if strings.Contains(cypher, "source.scope_id") {
		t.Fatalf("retract must not filter by node scope_id — CloudResource nodes carry none, making the delete a no-op:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must be scoped to this reducer's evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("retract must DELETE only the edge, never a node:\n%s", cypher)
	}
	if strings.Contains(cypher, "DETACH DELETE") || strings.Contains(cypher, "DELETE source") || strings.Contains(cypher, "DELETE target") {
		t.Fatalf("retract must not delete endpoint nodes:\n%s", cypher)
	}
}

func TestCloudResourceEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceEdges(context.Background(), nil, "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("RetractCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

func TestCloudResourceEdgeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface shape used by the relationship materialization handler.
	var _ interface {
		WriteCloudResourceEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		RetractCloudResourceEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
		RetractCloudResourceEdgesByUIDs(ctx context.Context, sourceUIDs []string, scopeIDs []string, evidenceSource string) error
	} = NewCloudResourceEdgeWriter(&recordingExecutor{}, 0)
}

// TestCloudResourceEdgeWriterRetractByUIDsAnchoredDelete proves the anchored
// retract enumerates $source_uids via a single-clause
// `WHERE source.uid IN $source_uids` predicate that seeds the
// CloudResource.uid index, instead of scanning the whole :CloudResource label
// or splitting into a two-clause MATCH/MATCH, while still filtering by scope
// and evidence source.
func TestCloudResourceEdgeWriterRetractByUIDsAnchoredDelete(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(),
		[]string{"src-a", "src-b"},
		[]string{"scope-1"},
		"reducer/aws-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (source:CloudResource)-[rel]->()",
		"WHERE source.uid IN $source_uids",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract by uids cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "UNWIND $source_uids AS suid") || strings.Contains(cypher, "{uid: suid}") {
		t.Fatalf("retract by uids cypher must not use the slow UNWIND + property-map MATCH shape:\n%s", cypher)
	}
	if got := executor.calls[0].Parameters["source_uids"]; got == nil {
		t.Fatalf("source_uids param missing")
	}
	if got := executor.calls[0].Parameters["scope_ids"]; got == nil {
		t.Fatalf("scope_ids param missing")
	}
	if got := executor.calls[0].Parameters["evidence_source"]; got != "reducer/aws-relationships" {
		t.Fatalf("evidence_source param = %v, want reducer/aws-relationships", got)
	}
}

// TestCloudResourceEdgeWriterRetractByUIDsEmptyIsNoOp proves empty source uids
// is a clean no-op (no executor call at all).
func TestCloudResourceEdgeWriterRetractByUIDsEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(), nil, []string{"scope-1"}, "reducer/aws-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty uids", len(executor.calls))
	}
}

// TestCloudResourceEdgeWriterRetractByUIDsBatchesUids proves uids beyond the
// batch size split into multiple statements.
func TestCloudResourceEdgeWriterRetractByUIDsBatchesUids(t *testing.T) {
	t.Parallel()

	uids := make([]string, 1200)
	for i := range uids {
		uids[i] = "uid-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(), uids, []string{"scope-1"}, "reducer/aws-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
	}
	// 1200 uids at 500 batch = ceil(1200/500) = 3 batches.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batches", len(executor.calls))
	}
}
