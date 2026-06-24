// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// kubernetesCorrelationEdgeRows mirrors the rows
// ExtractKubernetesCorrelationEdgeRows produces: the live workload node uid, the
// resolved OCI source node uid, the uid-indexed source label, the static
// relationship type, and the resolution mode. It omits
// scope_id/generation_id/evidence_source — the writer injects those
// reducer-scoped annotations from its call arguments.
func kubernetesCorrelationEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"workload_uid":    "k8s://prod/apps/v1/deployments/ns/w" + string(rune('a'+i)),
			"source_uid":      "oci-descriptor://reg/repo@sha256:" + string(rune('a'+i)),
			"source_label":    "OciImageManifest",
			"rel_type":        "RUNS_IMAGE",
			"resolution_mode": "digest",
		})
	}
	return rows
}

func TestKubernetesCorrelationEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)

	if err := writer.WriteKubernetesCorrelationEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestKubernetesCorrelationEdgeWriterUsesStaticRelTypeMatchMatchMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)

	if err := writer.WriteKubernetesCorrelationEdges(context.Background(), kubernetesCorrelationEdgeRows(1), "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Two MATCHes before the MERGE guarantee a missing endpoint is a no-op, never
	// a fabricated node — the graceful-degradation contract from #805/#391.
	if !strings.Contains(cypher, "MATCH (w:KubernetesWorkload {uid: row.workload_uid})") {
		t.Fatalf("cypher must MATCH the workload by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (img:OciImageManifest {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH the OCI source node by its uid-indexed label:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (w:KubernetesWorkload") || strings.Contains(cypher, "MERGE (img:OciImageManifest") {
		t.Fatalf("cypher must not MERGE (fabricate) endpoint nodes:\n%s", cypher)
	}
	// The relationship type is a static token, never a property-keyed MERGE
	// identity, so NornicDB keeps its relationship hot path (#805 §5.3).
	if !strings.Contains(cypher, "MERGE (w)-[rel:RUNS_IMAGE]->(img)") {
		t.Fatalf("edge MERGE must use the static RUNS_IMAGE relationship type:\n%s", cypher)
	}
	if strings.Contains(cypher, "{rel_type: row.rel_type}") {
		t.Fatalf("rel_type must not live inside MERGE identity:\n%s", cypher)
	}
}

func TestKubernetesCorrelationEdgeWriterSplitsBySourceLabel(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 500)
	rows := []map[string]any{
		{
			"workload_uid":    "w-1",
			"source_uid":      "src-manifest",
			"source_label":    "OciImageManifest",
			"rel_type":        "RUNS_IMAGE",
			"resolution_mode": "digest",
		},
		{
			"workload_uid":    "w-2",
			"source_uid":      "src-index",
			"source_label":    "OciImageIndex",
			"rel_type":        "RUNS_IMAGE",
			"resolution_mode": "digest",
		},
		{
			"workload_uid":    "w-3",
			"source_uid":      "src-descriptor",
			"source_label":    "OciImageDescriptor",
			"rel_type":        "RUNS_IMAGE",
			"resolution_mode": "digest",
		},
	}

	if err := writer.WriteKubernetesCorrelationEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	stmts := executor.groupCalls[0]
	if len(stmts) != 3 {
		t.Fatalf("group statement count = %d, want one statement per source label", len(stmts))
	}
	gotCypher := stmts[0].Cypher + "\n" + stmts[1].Cypher + "\n" + stmts[2].Cypher
	for _, want := range []string{
		"MATCH (img:OciImageManifest {uid: row.source_uid})",
		"MATCH (img:OciImageIndex {uid: row.source_uid})",
		"MATCH (img:OciImageDescriptor {uid: row.source_uid})",
	} {
		if !strings.Contains(gotCypher, want) {
			t.Fatalf("missing label-specific MATCH %q in:\n%s", want, gotCypher)
		}
	}
}

func TestKubernetesCorrelationEdgeWriterRejectsUnsafeSourceLabel(t *testing.T) {
	t.Parallel()

	// The source label is interpolated into the node-label position (which cannot
	// be parameterized), so a label outside the closed OCI source vocabulary must
	// be rejected rather than fabricating a new MATCH label that could scan or
	// inject.
	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)
	rows := kubernetesCorrelationEdgeRows(1)
	rows[0]["source_label"] = "K8sResource) DELETE w //"

	err := writer.WriteKubernetesCorrelationEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/kubernetes-correlation")
	if err == nil {
		t.Fatal("WriteKubernetesCorrelationEdges returned nil, want unsafe source_label error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when source_label is unsafe", len(executor.calls))
	}
}

func TestKubernetesCorrelationEdgeWriterRejectsOutOfVocabularySourceLabel(t *testing.T) {
	t.Parallel()

	// A label can be character-safe yet not belong to the closed OCI source-node
	// vocabulary. Rejecting it stops a deviating upstream from anchoring the edge
	// on an arbitrary label and silently growing the schema surface.
	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)
	rows := kubernetesCorrelationEdgeRows(1)
	rows[0]["source_label"] = "ContainerImageTagObservation"

	err := writer.WriteKubernetesCorrelationEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/kubernetes-correlation")
	if err == nil {
		t.Fatal("WriteKubernetesCorrelationEdges returned nil, want out-of-vocabulary source_label error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when source_label is out of vocabulary", len(executor.calls))
	}
}

func TestKubernetesCorrelationEdgeWriterAcceptsEveryClosedSourceLabel(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"OciImageManifest", "OciImageIndex", "OciImageDescriptor"} {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			writer := NewKubernetesCorrelationEdgeWriter(executor, 0)
			rows := kubernetesCorrelationEdgeRows(1)
			rows[0]["source_label"] = label

			if err := writer.WriteKubernetesCorrelationEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
				t.Fatalf("WriteKubernetesCorrelationEdges(%q) returned error: %v", label, err)
			}
			if len(executor.calls) != 1 {
				t.Fatalf("len(calls) = %d, want 1 for closed-vocabulary label %q", len(executor.calls), label)
			}
			want := "MATCH (img:" + label + " {uid: row.source_uid})"
			if !strings.Contains(executor.calls[0].Cypher, want) {
				t.Fatalf("missing %q in:\n%s", want, executor.calls[0].Cypher)
			}
		})
	}
}

func TestKubernetesCorrelationEdgeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 2)

	if err := writer.WriteKubernetesCorrelationEdges(context.Background(), kubernetesCorrelationEdgeRows(5), "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges returned error: %v", err)
	}
	// 5 rows of the same source label at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestKubernetesCorrelationEdgeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 2)

	if err := writer.WriteKubernetesCorrelationEdges(context.Background(), kubernetesCorrelationEdgeRows(5), "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestKubernetesCorrelationEdgeWriterAnnotatesScopeGenerationEvidence(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)

	if err := writer.WriteKubernetesCorrelationEdges(context.Background(), kubernetesCorrelationEdgeRows(1), "scope-1", "gen-1", "reducer/kubernetes-correlation"); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges returned error: %v", err)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if got := rows[0]["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id = %v, want scope-1 (injected for scope-scoped retract)", got)
	}
	if got := rows[0]["generation_id"]; got != "gen-1" {
		t.Fatalf("generation_id = %v, want gen-1", got)
	}
	if got := rows[0]["evidence_source"]; got != "reducer/kubernetes-correlation" {
		t.Fatalf("evidence_source = %v, want reducer/kubernetes-correlation", got)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
		"rel.resolution_mode = row.resolution_mode",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher must persist %q:\n%s", want, cypher)
		}
	}
}

func TestKubernetesCorrelationEdgeWriterRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)

	if err := writer.RetractKubernetesCorrelationEdges(
		context.Background(),
		[]string{"scope-1"},
		"gen-1",
		"reducer/kubernetes-correlation",
	); err != nil {
		t.Fatalf("RetractKubernetesCorrelationEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 retract statement", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (w:KubernetesWorkload)-[rel:RUNS_IMAGE]->()") {
		t.Fatalf("retract must target reducer-owned RUNS_IMAGE edges from workloads:\n%s", cypher)
	}
	// The retract MUST filter on the edge's own scope_id. KubernetesWorkload and
	// OCI nodes are cross-generation canonical and carry no reducer scope_id, so a
	// node-scoped predicate would be a silent no-op that leaks stale edges.
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must filter by the edge scope_id:\n%s", cypher)
	}
	if strings.Contains(cypher, "w.scope_id") || strings.Contains(cypher, "img.scope_id") {
		t.Fatalf("retract must not filter by node scope_id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must be scoped to this reducer's evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("retract must DELETE only the edge:\n%s", cypher)
	}
	if strings.Contains(cypher, "DETACH DELETE") || strings.Contains(cypher, "DELETE w") || strings.Contains(cypher, "DELETE img") {
		t.Fatalf("retract must not delete endpoint nodes:\n%s", cypher)
	}
}

func TestKubernetesCorrelationEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesCorrelationEdgeWriter(executor, 0)

	if err := writer.RetractKubernetesCorrelationEdges(context.Background(), nil, "gen-1", "reducer/kubernetes-correlation"); err != nil {
		t.Fatalf("RetractKubernetesCorrelationEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

func TestKubernetesCorrelationEdgeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	var _ interface {
		WriteKubernetesCorrelationEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		RetractKubernetesCorrelationEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
	} = NewKubernetesCorrelationEdgeWriter(&recordingExecutor{}, 0)
}
