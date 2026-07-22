// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func kubernetesNamespaceRow(uid, environment, environmentState, evidenceClass string) map[string]any {
	return map[string]any{
		"uid":                 uid,
		"cluster_id":          "prod-eks",
		"namespace":           uid,
		"labels":              []string{"team=payments"},
		"correlation_anchors": []string{uid},
		"environment":         environment,
		"environment_state":   environmentState,
		"evidence_class":      evidenceClass,
		"source_fact_id":      "fact-1",
		"stable_fact_key":     "key-1",
		"source_system":       "kubernetes_live",
		"source_record_id":    "rec-1",
		"source_confidence":   "reported",
		"collector_kind":      "kubernetes_live",
	}
}

func TestKubernetesNamespaceNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), nil, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

// TestKubernetesNamespaceNodeWriterUnboundRowNeverCreatesEnvironment is the
// negative-regression lock for issue #5434: an unbound namespace row (empty
// "environment") MUST route its node-property upsert through
// canonicalKubernetesNamespaceUpsertCypher only. If the writer's
// environment-presence guard were removed or inverted -- for example if
// WriteKubernetesNamespaceNodes always used
// canonicalKubernetesNamespaceWithEnvironmentUpsertCypher -- this test fails
// because the recorded upsert statement would then contain
// "MERGE (env:Environment". The write also emits a retract statement ahead of
// the upsert (see TestKubernetesNamespaceNodeWriterEmitsRetractBeforeUpsert),
// so this test locates the upsert call by its Operation rather than assuming
// index 0.
func TestKubernetesNamespaceNodeWriterUnboundRowNeverCreatesEnvironment(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	row := kubernetesNamespaceRow("misc", "", "environment-unbound", "")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{row}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 (retract + unbound upsert)", len(executor.calls))
	}
	cypher := findUpsertCall(t, executor.calls).Cypher
	if strings.Contains(cypher, "MERGE (env:Environment") {
		t.Fatalf("unbound-row statement must never MERGE an Environment node:\n%s", cypher)
	}
	if strings.Contains(cypher, "TARGETS_ENVIRONMENT") {
		t.Fatalf("unbound-row statement must never write a TARGETS_ENVIRONMENT edge:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (n:KubernetesNamespace {uid: row.uid})") {
		t.Fatalf("cypher must MERGE on uid identity only:\n%s", cypher)
	}
	if !strings.Contains(cypher, "n.environment = null") || !strings.Contains(cypher, "n.evidence_class = null") {
		t.Fatalf("unbound-row statement must SET environment/evidence_class to null to clear any stale value from a prior bound generation:\n%s", cypher)
	}
	if strings.Contains(cypher, "REMOVE") {
		t.Fatalf("unbound-row statement must clear environment/evidence_class via SET ... = null, not REMOVE (REMOVE fails under the managed ExecuteWrite transaction on NornicDB v1.1.11):\n%s", cypher)
	}
}

// findUpsertCall returns the single OperationCanonicalUpsert statement among
// recorded calls, failing the test if there is not exactly one. Test helper
// for isolating the upsert call from the retract call the writer now emits
// ahead of it (#5434 codex review finding P1).
func findUpsertCall(t *testing.T, calls []Statement) Statement {
	t.Helper()
	var upserts []Statement
	for _, call := range calls {
		if call.Operation == OperationCanonicalUpsert {
			upserts = append(upserts, call)
		}
	}
	if len(upserts) != 1 {
		t.Fatalf("upsert call count = %d, want 1 among %d total calls", len(upserts), len(calls))
	}
	return upserts[0]
}

// TestKubernetesNamespaceNodeWriterBoundRowCreatesEnvironment proves the
// positive counterpart: a row with a non-empty "environment" property routes
// through canonicalKubernetesNamespaceWithEnvironmentUpsertCypher and MERGEs
// an Environment node plus a TARGETS_ENVIRONMENT edge carrying the
// evidence_class the reducer classified.
func TestKubernetesNamespaceNodeWriterBoundRowCreatesEnvironment(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	row := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{row}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 (retract + bound upsert)", len(executor.calls))
	}
	upsertCall := findUpsertCall(t, executor.calls)
	cypher := upsertCall.Cypher
	if !strings.Contains(cypher, "MERGE (env:Environment {name: row.environment})") {
		t.Fatalf("bound-row statement must MERGE the Environment node:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)") {
		t.Fatalf("bound-row statement must MERGE the TARGETS_ENVIRONMENT edge:\n%s", cypher)
	}
	if !strings.Contains(cypher, "env_rel.evidence_class = row.evidence_class") {
		t.Fatalf("bound-row statement must set the edge's evidence_class:\n%s", cypher)
	}
	rows, ok := upsertCall.Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows parameter = %#v, want 1 row", upsertCall.Parameters["rows"])
	}
	if got := rows[0]["environment"]; got != "prod" {
		t.Fatalf("row[environment] = %v, want prod", got)
	}
	if got := rows[0]["evidence_source"]; got != "reducer/kubernetes-namespaces" {
		t.Fatalf("row[evidence_source] = %v, want reducer/kubernetes-namespaces", got)
	}
}

// TestKubernetesNamespaceNodeWriterSplitsMixedBatch proves a mixed batch (one
// bound namespace, one unbound namespace) produces two SEPARATE upsert
// statements -- the unbound row never appears in the with-environment
// statement's rows and vice versa, so one namespace's binding can never leak
// into another's write. The write also emits one retract statement covering
// both rows (see TestKubernetesNamespaceNodeWriterEmitsRetractBeforeUpsert),
// so this test filters to OperationCanonicalUpsert calls before checking the
// one-statement-per-variant split.
func TestKubernetesNamespaceNodeWriterSplitsMixedBatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	bound := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	unbound := kubernetesNamespaceRow("misc", "", "environment-unbound", "")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{bound, unbound}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 (one retract covering both rows, plus one upsert per variant)", len(executor.calls))
	}
	var upsertCalls []Statement
	for _, call := range executor.calls {
		if call.Operation == OperationCanonicalUpsert {
			upsertCalls = append(upsertCalls, call)
		}
	}
	if len(upsertCalls) != 2 {
		t.Fatalf("upsert call count = %d, want 2 (one per variant)", len(upsertCalls))
	}
	var sawBoundStatement, sawUnboundStatement bool
	for _, call := range upsertCalls {
		rows, ok := call.Parameters["rows"].([]map[string]any)
		if !ok || len(rows) != 1 {
			t.Fatalf("rows parameter = %#v, want exactly 1 row per statement", call.Parameters["rows"])
		}
		containsEnvMerge := strings.Contains(call.Cypher, "MERGE (env:Environment")
		switch rows[0]["uid"] {
		case "payments-prod":
			sawBoundStatement = true
			if !containsEnvMerge {
				t.Fatalf("bound row's statement must MERGE Environment:\n%s", call.Cypher)
			}
		case "misc":
			sawUnboundStatement = true
			if containsEnvMerge {
				t.Fatalf("unbound row's statement must NOT MERGE Environment:\n%s", call.Cypher)
			}
		default:
			t.Fatalf("unexpected row uid %v", rows[0]["uid"])
		}
	}
	if !sawBoundStatement || !sawUnboundStatement {
		t.Fatalf("expected one statement per variant: sawBound=%v sawUnbound=%v", sawBoundStatement, sawUnboundStatement)
	}
}

// TestKubernetesNamespaceNodeWriterUsesGroupExecutorAtomically proves the
// UPSERT statements (bound + unbound variants) still dispatch through one
// atomic ExecuteGroup call, exactly as before this fix. The retract
// statement is deliberately NOT part of that group -- see
// TestKubernetesNamespaceNodeWriterRetractNeverGroups -- because
// recordingGroupExecutor's Execute is a no-op that records nothing, this
// test cannot observe the retract call at all, only the upsert group.
func TestKubernetesNamespaceNodeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	bound := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	unbound := kubernetesNamespaceRow("misc", "", "environment-unbound", "")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{bound, unbound}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 2 {
		t.Fatalf("group statement count = %d, want 2 (bound + unbound upsert variants only)", len(executor.groupCalls[0]))
	}
	for _, stmt := range executor.groupCalls[0] {
		if stmt.Operation != OperationCanonicalUpsert {
			t.Fatalf("grouped statement Operation = %q, want %q (retract must never enter the group)", stmt.Operation, OperationCanonicalUpsert)
		}
	}
}

// TestKubernetesNamespaceNodeWriterRetractNeverGroups is the no-group
// dispatch guard for codex review finding P1 (#5434), mirroring
// cloud_edge_retract_dispatch_test.go's TestKubernetesCorrelationEdgeWriterRetractNeverGroups
// et al.: on the pinned NornicDB v1.1.11 a retract DELETE dispatched through
// ExecuteGroup (the real production reducer executor for this writer) can
// under-apply even as the sole statement, while the identical statement run
// auto-commit (Execute) deletes correctly. sqlSequentialRecordingExecutor
// implements GroupExecutor and records both Execute and ExecuteGroup calls,
// so a regression that folds the retract back into the grouped dispatch
// fails this test; the plain recordingExecutor used elsewhere in this file
// does NOT implement GroupExecutor, so it cannot detect that regression.
func TestKubernetesNamespaceNodeWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	row := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{row}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}

	var retractCalls, upsertCalls int
	for _, call := range executor.calls {
		switch call.Operation {
		case OperationCanonicalRetract:
			retractCalls++
			if !strings.Contains(call.Cypher, "DELETE rel") {
				t.Fatalf("retract Execute call must DELETE the stale edge:\n%s", call.Cypher)
			}
		case OperationCanonicalUpsert:
			upsertCalls++
		}
	}
	if retractCalls != 1 {
		t.Fatalf("sequential retract Execute calls = %d, want 1", retractCalls)
	}
	if upsertCalls != 0 {
		t.Fatalf("upsert Execute calls = %d, want 0 (this writer's upsert must dispatch through ExecuteGroup when available)", upsertCalls)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("ExecuteGroup calls = %d, want 1 (the upsert statement, never the retract)", len(executor.groupCalls))
	}
	for _, stmt := range executor.groupCalls[0] {
		if stmt.Operation == OperationCanonicalRetract {
			t.Fatalf("retract statement must never be dispatched through ExecuteGroup (grouped DELETEs under-apply on NornicDB v1.1.11):\n%s", stmt.Cypher)
		}
	}
}

func TestKubernetesNamespaceNodeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface. This test fails to compile if the method set drifts.
	var _ interface {
		WriteKubernetesNamespaceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	} = NewKubernetesNamespaceNodeWriter(&recordingExecutor{}, 0)
}

// TestKubernetesNamespaceNodeWriterEmitsRetractBeforeUpsert is the
// regression lock for codex review finding P1 (#5434): every write now emits
// a retractKubernetesNamespaceStaleTargetsEnvironmentCypher statement BEFORE
// the corresponding upsert statement, covering every row in the batch (bound
// and unbound alike), and dispatches it via sequential Execute (see
// TestKubernetesNamespaceNodeWriterRetractNeverGroups for the no-group
// dispatch proof). Before the fix the writer emitted no retract statement at
// all, so this test fails against the pre-fix writer on the very first
// assertion (len(calls) == 1, not 2).
func TestKubernetesNamespaceNodeWriterEmitsRetractBeforeUpsert(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewKubernetesNamespaceNodeWriter(executor, 0)

	row := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
	if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{row}, "reducer/kubernetes-namespaces"); err != nil {
		t.Fatalf("WriteKubernetesNamespaceNodes returned error: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 (retract then upsert)", len(executor.calls))
	}
	retractCall := executor.calls[0]
	upsertCall := executor.calls[1]

	if retractCall.Operation != OperationCanonicalRetract {
		t.Fatalf("calls[0].Operation = %q, want %q (retract must be emitted first)", retractCall.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(retractCall.Cypher, "DELETE rel") {
		t.Fatalf("retract statement must DELETE the stale edge:\n%s", retractCall.Cypher)
	}
	if !strings.Contains(retractCall.Cypher, "old_env.name <> row.environment") {
		t.Fatalf("retract statement must target only an edge to a DIFFERENT environment than this row's:\n%s", retractCall.Cypher)
	}
	if got := retractCall.Parameters["evidence_source"]; got != "reducer/kubernetes-namespaces" {
		t.Fatalf("retract evidence_source = %v, want reducer/kubernetes-namespaces", got)
	}
	if upsertCall.Operation != OperationCanonicalUpsert {
		t.Fatalf("calls[1].Operation = %q, want %q (upsert must follow the retract)", upsertCall.Operation, OperationCanonicalUpsert)
	}
}

// namespaceTargetsEnvironmentGraph is a minimal in-memory model of the
// (:KubernetesNamespace)-[:TARGETS_ENVIRONMENT]->(:Environment) edge SET
// (keyed by (uid, environment name), never a uid->single-env map, because a
// namespace re-bound to a new environment without a working retract
// accumulates a SECOND edge rather than replacing the first -- exactly the
// bug codex review finding P1 describes). apply() interprets a recorded
// Statement by matching its Cypher TEXT against the writer's own exported
// constants and replaying ONLY the generic MATCH/DELETE/MERGE semantics
// those specific statements need; it never re-implements
// WriteKubernetesNamespaceNodes's row-classification logic, so it proves the
// GRAPH STATE the real production statements produce, not a re-derivation of
// the code under test.
type namespaceTargetsEnvironmentEdgeKey struct {
	uid     string
	envName string
}

type namespaceTargetsEnvironmentGraph struct {
	edges map[namespaceTargetsEnvironmentEdgeKey]string // key -> evidence_source
}

func newNamespaceTargetsEnvironmentGraph() *namespaceTargetsEnvironmentGraph {
	return &namespaceTargetsEnvironmentGraph{edges: map[namespaceTargetsEnvironmentEdgeKey]string{}}
}

func (g *namespaceTargetsEnvironmentGraph) apply(t *testing.T, stmt Statement) {
	t.Helper()
	switch stmt.Cypher {
	case retractKubernetesNamespaceStaleTargetsEnvironmentCypher:
		evidenceSource, _ := stmt.Parameters["evidence_source"].(string)
		rows, _ := stmt.Parameters["rows"].([]map[string]any)
		for _, row := range rows {
			uid, _ := row["uid"].(string)
			newEnv, _ := row["environment"].(string)
			for key, existingEvidenceSource := range g.edges {
				if key.uid == uid && existingEvidenceSource == evidenceSource && key.envName != newEnv {
					delete(g.edges, key)
				}
			}
		}
	case canonicalKubernetesNamespaceWithEnvironmentUpsertCypher:
		rows, _ := stmt.Parameters["rows"].([]map[string]any)
		for _, row := range rows {
			uid, _ := row["uid"].(string)
			env, _ := row["environment"].(string)
			evidenceSource, _ := row["evidence_source"].(string)
			g.edges[namespaceTargetsEnvironmentEdgeKey{uid: uid, envName: env}] = evidenceSource
		}
	case canonicalKubernetesNamespaceUpsertCypher:
		// The no-environment variant never MERGEs or DELETEs an edge.
	default:
		t.Fatalf("namespaceTargetsEnvironmentGraph.apply: unrecognized statement cypher:\n%s", stmt.Cypher)
	}
}

// edgesForUID returns every environment name currently linked from uid, for
// asserting exactly-one-edge / zero-edge postconditions.
func (g *namespaceTargetsEnvironmentGraph) edgesForUID(uid string) []string {
	var envs []string
	for key := range g.edges {
		if key.uid == uid {
			envs = append(envs, key.envName)
		}
	}
	return envs
}

// TestKubernetesNamespaceNodeWriterTargetsEnvironmentTransitions is the
// graph-truth regression for codex review finding P1 (#5434): it drives the
// REAL production WriteKubernetesNamespaceNodes twice per subtest (mimicking
// two reducer generations for the same namespace) and replays every recorded
// statement through namespaceTargetsEnvironmentGraph to prove the resulting
// edge SET, not just the emitted Cypher text. Reverting the retract statement
// (or its old_env.name <> row.environment guard) makes both subtests fail:
// the bound->unbound subtest would still show a stale edge, and the
// prod->stage subtest would show two edges instead of exactly one.
func TestKubernetesNamespaceNodeWriterTargetsEnvironmentTransitions(t *testing.T) {
	t.Parallel()

	const evidenceSource = "reducer/kubernetes-namespaces"

	t.Run("bound to unbound removes the TARGETS_ENVIRONMENT edge", func(t *testing.T) {
		t.Parallel()

		executor := &recordingExecutor{}
		writer := NewKubernetesNamespaceNodeWriter(executor, 0)
		graph := newNamespaceTargetsEnvironmentGraph()

		boundRow := kubernetesNamespaceRow("payments-prod", "prod", "bound", "namespace_label")
		if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{boundRow}, evidenceSource); err != nil {
			t.Fatalf("first (bound) write: %v", err)
		}
		for _, call := range executor.calls {
			graph.apply(t, call)
		}
		if envs := graph.edgesForUID("payments-prod"); len(envs) != 1 || envs[0] != "prod" {
			t.Fatalf("setup: edges after first write = %v, want exactly [prod]", envs)
		}

		executor.calls = nil
		unboundRow := kubernetesNamespaceRow("payments-prod", "", "environment-unbound", "")
		if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{unboundRow}, evidenceSource); err != nil {
			t.Fatalf("second (unbound) write: %v", err)
		}
		for _, call := range executor.calls {
			graph.apply(t, call)
		}

		if envs := graph.edgesForUID("payments-prod"); len(envs) != 0 {
			t.Fatalf("bound->unbound transition must remove the TARGETS_ENVIRONMENT edge, got edges=%v", envs)
		}
	})

	t.Run("bound prod to bound stage leaves exactly one edge pointing at stage", func(t *testing.T) {
		t.Parallel()

		executor := &recordingExecutor{}
		writer := NewKubernetesNamespaceNodeWriter(executor, 0)
		graph := newNamespaceTargetsEnvironmentGraph()

		prodRow := kubernetesNamespaceRow("payments", "prod", "bound", "namespace_label")
		if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{prodRow}, evidenceSource); err != nil {
			t.Fatalf("first (prod) write: %v", err)
		}
		for _, call := range executor.calls {
			graph.apply(t, call)
		}
		if envs := graph.edgesForUID("payments"); len(envs) != 1 || envs[0] != "prod" {
			t.Fatalf("setup: edges after first write = %v, want exactly [prod]", envs)
		}

		executor.calls = nil
		stageRow := kubernetesNamespaceRow("payments", "stage", "bound", "namespace_label")
		if err := writer.WriteKubernetesNamespaceNodes(context.Background(), []map[string]any{stageRow}, evidenceSource); err != nil {
			t.Fatalf("second (stage) write: %v", err)
		}
		for _, call := range executor.calls {
			graph.apply(t, call)
		}

		envs := graph.edgesForUID("payments")
		if len(envs) != 1 {
			t.Fatalf("bound(prod)->bound(stage) transition must leave exactly ONE edge, got edges=%v (stale prod edge must not survive alongside the new stage edge)", envs)
		}
		if envs[0] != "stage" {
			t.Fatalf("surviving edge points at %q, want %q", envs[0], "stage")
		}
	})
}
