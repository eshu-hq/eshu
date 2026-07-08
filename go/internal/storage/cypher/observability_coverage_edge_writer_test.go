// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// observabilityCoverageEdgeRows mirrors the rows
// ExtractObservabilityCoverageEdgeRows produces: the observability resource uid,
// the monitored target uid, the coverage signal class, and the resolution mode.
// It omits scope_id/generation_id/evidence_source — the writer injects those
// reducer-scoped annotations from its call arguments.
func observabilityCoverageEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"observability_uid": "obs-" + string(rune('a'+i)),
			"target_uid":        "tgt-" + string(rune('a'+i)),
			"coverage_signal":   "alarm",
			"resolution_mode":   "arn",
		})
	}
	return rows
}

func TestObservabilityCoverageEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)

	if err := writer.WriteObservabilityCoverageEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
		t.Fatalf("WriteObservabilityCoverageEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestObservabilityCoverageEdgeWriterUsesStaticSignalTypeMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)

	if err := writer.WriteObservabilityCoverageEdges(context.Background(), observabilityCoverageEdgeRows(1), "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
		t.Fatalf("WriteObservabilityCoverageEdges returned error: %v", err)
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
	if !strings.Contains(cypher, "MATCH (obs:CloudResource {uid: row.observability_uid})") {
		t.Fatalf("cypher must MATCH the observability CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:CloudResource {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH the target CloudResource by uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (obs:CloudResource") || strings.Contains(cypher, "MERGE (target:CloudResource") {
		t.Fatalf("cypher must not MERGE (fabricate) endpoint nodes:\n%s", cypher)
	}
	// The coverage signal lives in the static relationship-type token, not in a
	// MERGE property map, so NornicDB keeps its relationship hot path (#805 §5.3,
	// memo §6 Q3): property-keyed relationship MERGE timed out at 20s vs 0–1ms.
	if strings.Contains(cypher, "{coverage_signal: row.coverage_signal}") {
		t.Fatalf("coverage_signal must not live inside MERGE identity because NornicDB misses the relationship hot path:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (obs)-[rel:AWS_COVERS_alarm]->(target)") {
		t.Fatalf("edge MERGE must use the sanitized AWS_COVERS_<signal> relationship type:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.coverage_signal = row.coverage_signal") {
		t.Fatalf("edge must keep the coverage_signal as a property for API/readback truth:\n%s", cypher)
	}
}

func TestObservabilityCoverageEdgeWriterSplitsSamePairBySignal(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 500)
	rows := []map[string]any{
		{
			"observability_uid": "shared-obs",
			"target_uid":        "shared-target",
			"coverage_signal":   "alarm",
			"resolution_mode":   "bare_id",
		},
		{
			"observability_uid": "shared-obs",
			"target_uid":        "shared-target",
			"coverage_signal":   "log_group",
			"resolution_mode":   "arn",
		},
	}

	if err := writer.WriteObservabilityCoverageEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
		t.Fatalf("WriteObservabilityCoverageEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	stmts := executor.groupCalls[0]
	if len(stmts) != 2 {
		t.Fatalf("group statement count = %d, want one statement per coverage signal", len(stmts))
	}
	gotCypher := stmts[0].Cypher + "\n" + stmts[1].Cypher
	for _, want := range []string{
		"MERGE (obs)-[rel:AWS_COVERS_alarm]->(target)",
		"MERGE (obs)-[rel:AWS_COVERS_log_group]->(target)",
	} {
		if !strings.Contains(gotCypher, want) {
			t.Fatalf("missing signal-specific MERGE %q in:\n%s", want, gotCypher)
		}
	}
}

func TestObservabilityCoverageEdgeWriterRejectsUnsafeSignal(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)
	rows := observabilityCoverageEdgeRows(1)
	rows[0]["coverage_signal"] = "bad signal`) DELETE n //"

	err := writer.WriteObservabilityCoverageEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/observability-coverage")
	if err == nil {
		t.Fatal("WriteObservabilityCoverageEdges returned nil, want unsafe coverage_signal error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when coverage_signal is unsafe", len(executor.calls))
	}
}

func TestObservabilityCoverageEdgeWriterRejectsOutOfVocabularySignal(t *testing.T) {
	t.Parallel()

	// A token can be character-safe yet still not belong to the closed coverage
	// vocabulary (alarm/composite_alarm/dashboard/log_group/trace_sampling). The
	// writer must reject it so a deviating upstream signal cannot fabricate a new
	// AWS_COVERS_<token> relationship type and silently grow the schema surface.
	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)
	rows := observabilityCoverageEdgeRows(1)
	rows[0]["coverage_signal"] = "metric_filter"

	err := writer.WriteObservabilityCoverageEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/observability-coverage")
	if err == nil {
		t.Fatal("WriteObservabilityCoverageEdges returned nil, want out-of-vocabulary coverage_signal error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when coverage_signal is out of vocabulary", len(executor.calls))
	}
}

func TestObservabilityCoverageEdgeWriterAcceptsEveryClosedVocabularySignal(t *testing.T) {
	t.Parallel()

	// Every member of the closed vocabulary must still produce its own
	// AWS_COVERS_<signal> relationship type; the allowlist tightens the gate
	// without dropping any contract signal.
	for _, signal := range []string{"alarm", "composite_alarm", "dashboard", "log_group", "trace_sampling"} {
		signal := signal
		t.Run(signal, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			writer := NewObservabilityCoverageEdgeWriter(executor, 0)
			rows := observabilityCoverageEdgeRows(1)
			rows[0]["coverage_signal"] = signal

			if err := writer.WriteObservabilityCoverageEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
				t.Fatalf("WriteObservabilityCoverageEdges(%q) returned error: %v", signal, err)
			}
			if len(executor.calls) != 1 {
				t.Fatalf("len(calls) = %d, want 1 for closed-vocabulary signal %q", len(executor.calls), signal)
			}
			want := "MERGE (obs)-[rel:AWS_COVERS_" + signal + "]->(target)"
			if !strings.Contains(executor.calls[0].Cypher, want) {
				t.Fatalf("missing %q in:\n%s", want, executor.calls[0].Cypher)
			}
		})
	}
}

func TestObservabilityCoverageEdgeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 2)

	if err := writer.WriteObservabilityCoverageEdges(context.Background(), observabilityCoverageEdgeRows(5), "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
		t.Fatalf("WriteObservabilityCoverageEdges returned error: %v", err)
	}
	// 5 rows of the same signal at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestObservabilityCoverageEdgeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 2)

	if err := writer.WriteObservabilityCoverageEdges(context.Background(), observabilityCoverageEdgeRows(5), "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
		t.Fatalf("WriteObservabilityCoverageEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestObservabilityCoverageEdgeWriterAnnotatesScopeGenerationEvidence(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)

	if err := writer.WriteObservabilityCoverageEdges(context.Background(), observabilityCoverageEdgeRows(1), "scope-1", "gen-1", "reducer/observability-coverage"); err != nil {
		t.Fatalf("WriteObservabilityCoverageEdges returned error: %v", err)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	// The resolution layer does not carry these reducer-scoped fields; the writer
	// injects them so the persisted edge carries scope_id/generation_id (else
	// scope-scoped retract is a silent no-op) and evidence_source (else
	// cross-writer retract isolation breaks).
	if got := rows[0]["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id = %v, want scope-1 (injected by writer for scope-scoped retract)", got)
	}
	if got := rows[0]["generation_id"]; got != "gen-1" {
		t.Fatalf("generation_id = %v, want gen-1 (injected by writer)", got)
	}
	if got := rows[0]["evidence_source"]; got != "reducer/observability-coverage" {
		t.Fatalf("evidence_source = %v, want reducer/observability-coverage", got)
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

func TestObservabilityCoverageEdgeWriterRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)

	if err := writer.RetractObservabilityCoverageEdges(
		context.Background(),
		[]string{"scope-1"},
		"gen-1",
		"reducer/observability-coverage",
	); err != nil {
		t.Fatalf("RetractObservabilityCoverageEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 retract statement", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (obs:CloudResource)-[rel]->(:CloudResource)") {
		t.Fatalf("retract must target all reducer-owned CloudResource relationships for the scope:\n%s", cypher)
	}
	// The retract MUST filter on the edge's own scope_id, not the endpoint
	// node's. CloudResource nodes are cross-scope canonical and carry no
	// scope_id property, so a node.scope_id predicate matches nothing and the
	// retract becomes a silent no-op that leaks stale edges across generations.
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must filter by the edge scope_id (rel.scope_id IN $scope_ids):\n%s", cypher)
	}
	if strings.Contains(cypher, "obs.scope_id") || strings.Contains(cypher, "target.scope_id") {
		t.Fatalf("retract must not filter by node scope_id — CloudResource nodes carry none, making the delete a no-op:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must be scoped to this reducer's evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("retract must DELETE only the edge, never a node:\n%s", cypher)
	}
	if strings.Contains(cypher, "DETACH DELETE") || strings.Contains(cypher, "DELETE obs") || strings.Contains(cypher, "DELETE target") {
		t.Fatalf("retract must not delete endpoint nodes:\n%s", cypher)
	}
}

func TestObservabilityCoverageEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)

	if err := writer.RetractObservabilityCoverageEdges(context.Background(), nil, "gen-1", "reducer/observability-coverage"); err != nil {
		t.Fatalf("RetractObservabilityCoverageEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

func TestObservabilityCoverageEdgeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface shape used by the coverage materialization handler.
	var _ interface {
		WriteObservabilityCoverageEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		RetractObservabilityCoverageEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
		RetractObservabilityCoverageEdgesByUIDs(ctx context.Context, sourceUIDs []string, scopeIDs []string, evidenceSource string) error
	} = NewObservabilityCoverageEdgeWriter(&recordingExecutor{}, 0)
}

// TestObservabilityCoverageEdgeWriterRetractByUIDsAnchoredDelete proves the
// anchored retract enumerates $source_uids and seeds the observability
// CloudResource.uid index (MATCH (obs:CloudResource {uid: suid})) instead of
// scanning the whole :CloudResource label.
func TestObservabilityCoverageEdgeWriterRetractByUIDsAnchoredDelete(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)
	if err := writer.RetractObservabilityCoverageEdgesByUIDs(
		context.Background(),
		[]string{"obs-a"},
		[]string{"scope-1"},
		"reducer/observability-coverage",
	); err != nil {
		t.Fatalf("RetractObservabilityCoverageEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $source_uids AS suid",
		"MATCH (obs:CloudResource {uid: suid})",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract by uids cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "MATCH (obs:CloudResource)-[rel]->") {
		t.Fatalf("retract by uids cypher must not fall back to the whole-label scan:\n%s", cypher)
	}
}

// TestObservabilityCoverageEdgeWriterRetractByUIDsEmptyIsNoOp proves empty
// source uids is a clean no-op.
func TestObservabilityCoverageEdgeWriterRetractByUIDsEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)
	if err := writer.RetractObservabilityCoverageEdgesByUIDs(
		context.Background(), nil, []string{"scope-1"}, "reducer/observability-coverage",
	); err != nil {
		t.Fatalf("RetractObservabilityCoverageEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty uids", len(executor.calls))
	}
}

// TestObservabilityCoverageEdgeWriterRetractByUIDsBatchesUids proves uids
// beyond the batch size split into multiple UNWIND statements.
func TestObservabilityCoverageEdgeWriterRetractByUIDsBatchesUids(t *testing.T) {
	t.Parallel()

	uids := make([]string, 1200)
	for i := range uids {
		uids[i] = "uid-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}

	executor := &recordingExecutor{}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)
	if err := writer.RetractObservabilityCoverageEdgesByUIDs(
		context.Background(), uids, []string{"scope-1"}, "reducer/observability-coverage",
	); err != nil {
		t.Fatalf("RetractObservabilityCoverageEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batches", len(executor.calls))
	}
}
