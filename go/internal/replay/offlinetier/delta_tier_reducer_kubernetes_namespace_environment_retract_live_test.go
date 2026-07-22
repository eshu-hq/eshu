// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// KubernetesNamespace TARGETS_ENVIRONMENT stale-edge retract and
// node-property clear coverage (codex review finding P1 + follow-up,
// #5434).
//
// KubernetesNamespaceNodeWriter.WriteKubernetesNamespaceNodes MERGEs a
// (:KubernetesNamespace)-[:TARGETS_ENVIRONMENT]->(:Environment) edge for
// environment-bound rows and clears the node's environment/evidence_class
// properties (never the edge) for unbound rows, but never retracted a
// PRE-EXISTING TARGETS_ENVIRONMENT edge: a namespace that lost its
// recognized environment label kept asserting the old environment forever,
// and a namespace re-bound from one environment to another (e.g. prod ->
// stage) accumulated a SECOND edge instead of replacing the first, since
// MERGE only matches an edge to the SAME target node. The fix adds
// retractKubernetesNamespaceStaleTargetsEnvironmentCypher, dispatched via
// KubernetesNamespaceNodeWriter.dispatchRetract -- sequential Execute calls,
// NEVER ExecuteGroup -- mirroring AzureCloudResourceEdgeWriter.dispatchRetract
// (evidence-4367-cloud-edge-retract.md): on the pinned NornicDB v1.1.11 a
// relationship DELETE dispatched through ExecuteGroup (the real production
// reducer executor for this writer, cmd/reducer's
// reducerNeo4jExecutor.ExecuteGroup) can under-apply even as the sole
// statement in the group, while the identical statement run auto-commit
// deletes correctly.
//
// Proving that fix live also surfaced a second, related defect: the
// sibling canonicalKubernetesNamespaceUpsertCypher (#5434's own code) used a
// trailing REMOVE n.environment, n.evidence_class to clear those node
// properties, and REMOVE fails outright under the SAME ExecuteGroup managed-
// transaction path with Neo.ClientError.Statement.SyntaxError. Fixed by
// replacing REMOVE with SET n.environment = null, n.evidence_class = null --
// the openCypher-standard property-delete form, proven live below to apply
// correctly under ExecuteGroup, consistent with every other property clear
// in this writer already using SET.
//
// This test drives the REAL production write path
// (cypher.KubernetesNamespaceNodeWriter.WriteKubernetesNamespaceNodes) twice
// per subtest -- mimicking two reducer generations for the same namespace --
// through the exact production dispatch shape: exec (liveExecutor) is passed
// directly as the writer's Executor, so the upsert statements dispatch
// through exec.ExecuteGroup (one managed Bolt transaction, matching
// reducerNeo4jExecutor.ExecuteGroup) and the retract statement dispatches
// through exec.Execute (autocommit), exactly as cmd/reducer wires this
// writer in production.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, eshu-correlation-truth.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	nsEnvRetractMarker = "replay-ns-env-retract"
	nsEnvRetractUID1   = nsEnvRetractMarker + ":payments-prod"
	nsEnvRetractUID2   = nsEnvRetractMarker + ":payments"
	// nsEnvRetractEnvProd / nsEnvRetractEnvStage are marker-scoped Environment
	// names (not the bare canonical "prod"/"stage") so this test's Environment
	// nodes can never collide with -- or get deleted alongside -- another
	// test's canonical Environment node sharing the same lean container. The
	// writer under test never validates its "environment" input against
	// environment.IsKnownToken (that gate lives upstream in the reducer's
	// namespaceEnvironmentFromLabels extraction, which this test bypasses by
	// calling WriteKubernetesNamespaceNodes directly), so any string works.
	nsEnvRetractEnvProd  = nsEnvRetractMarker + ":prod"
	nsEnvRetractEnvStage = nsEnvRetractMarker + ":stage"
)

// nsEnvRetractRow builds one KubernetesNamespaceNodeWriter row for the given
// uid and environment (empty string for an unbound row), matching the shape
// reducer.kubernetesNamespaceNodeRow projects in production.
func nsEnvRetractRow(uid, environment string) map[string]any {
	state := "environment-unbound"
	evidenceClass := ""
	if environment != "" {
		state = "bound"
		evidenceClass = "namespace_label"
	}
	return map[string]any{
		"uid":                 uid,
		"cluster_id":          "replay-cluster",
		"namespace":           uid,
		"labels":              []string{"team=payments"},
		"correlation_anchors": []string{uid},
		"environment":         environment,
		"environment_state":   state,
		"evidence_class":      evidenceClass,
		"source_fact_id":      "fact-1",
		"stable_fact_key":     "key-1",
		"source_system":       "kubernetes_live",
		"source_record_id":    "rec-1",
		"source_confidence":   "reported",
		"collector_kind":      "kubernetes_live",
	}
}

// TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth proves, on a
// real NornicDB, that a namespace's TARGETS_ENVIRONMENT edge is correctly
// retracted-then-replaced across two reducer generations:
//
//   - bound(prod) -> unbound: the TARGETS_ENVIRONMENT edge is gone (not left
//     stale, asserting an environment the current generation no longer
//     supports).
//   - bound(prod) -> bound(stage): exactly ONE TARGETS_ENVIRONMENT edge
//     survives, pointing at stage -- not two, not stale prod.
func TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the KubernetesNamespace environment-retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupNsEnvRetractScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupNsEnvRetractScope(cleanCtx, t, exec)
	})

	writer := cypher.NewKubernetesNamespaceNodeWriter(exec, 0)
	const evidenceSource = "reducer/kubernetes-namespaces"

	edgeQ := "MATCH (:KubernetesNamespace {uid: $uid})-[r:TARGETS_ENVIRONMENT]->(e:Environment) RETURN e.name AS env"
	nodeQ := "MATCH (n:KubernetesNamespace {uid: $uid}) RETURN count(n)"
	nodePropsQ := "MATCH (n:KubernetesNamespace {uid: $uid}) RETURN n.environment AS environment, n.evidence_class AS evidence_class, n.environment_state AS environment_state"

	t.Run("bound to unbound removes the edge and clears node properties", func(t *testing.T) {
		if err := writer.WriteKubernetesNamespaceNodes(ctx, []map[string]any{nsEnvRetractRow(nsEnvRetractUID1, nsEnvRetractEnvProd)}, evidenceSource); err != nil {
			t.Fatalf("first (bound) write: %v", err)
		}
		assertNamespaceEnvironmentEdges(ctx, t, exec, edgeQ, nsEnvRetractUID1, []string{nsEnvRetractEnvProd}, "write: bound edge present")
		// Positive control: the node-property read itself returns the bound
		// values, so the negative assertion below is trusted to actually read
		// through to graph state, not a query that always returns empty.
		assertNamespaceNodeProperties(ctx, t, exec, nodePropsQ, nsEnvRetractUID1, nsEnvRetractEnvProd, "namespace_label", "bound", "write: bound node properties present")

		// canonicalKubernetesNamespaceUpsertCypher clears n.environment /
		// n.evidence_class with "SET ... = null" (not REMOVE): on the pinned
		// NornicDB v1.1.11, a bare REMOVE dispatched through the Bolt
		// driver's managed ExecuteWrite transaction (the real production
		// reducer path -- reducerNeo4jExecutor.ExecuteGroup) fails with
		// Neo.ClientError.Statement.SyntaxError, even though the identical
		// statement succeeds via plain autocommit Run -- confirmed with a
		// minimal Bolt-driver probe before choosing SET ... = null. This
		// write, and the node-property assertions below, are the live proof
		// that SET ... = null correctly clears the properties under the
		// SAME managed-transaction path the edge retract also proves.
		if err := writer.WriteKubernetesNamespaceNodes(ctx, []map[string]any{nsEnvRetractRow(nsEnvRetractUID1, "")}, evidenceSource); err != nil {
			t.Fatalf("second (unbound) write: %v", err)
		}
		assertNamespaceEnvironmentEdges(ctx, t, exec, edgeQ, nsEnvRetractUID1, nil, "retract: bound->unbound edge gone")
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"uid": nsEnvRetractUID1}, 1, "node survives: bound->unbound")
		assertNamespaceNodeProperties(ctx, t, exec, nodePropsQ, nsEnvRetractUID1, "", "", "environment-unbound", "write: bound->unbound clears environment/evidence_class node properties")
	})

	t.Run("bound prod to bound stage leaves exactly one edge pointing at stage", func(t *testing.T) {
		if err := writer.WriteKubernetesNamespaceNodes(ctx, []map[string]any{nsEnvRetractRow(nsEnvRetractUID2, nsEnvRetractEnvProd)}, evidenceSource); err != nil {
			t.Fatalf("first (prod) write: %v", err)
		}
		assertNamespaceEnvironmentEdges(ctx, t, exec, edgeQ, nsEnvRetractUID2, []string{nsEnvRetractEnvProd}, "write: prod edge present")

		if err := writer.WriteKubernetesNamespaceNodes(ctx, []map[string]any{nsEnvRetractRow(nsEnvRetractUID2, nsEnvRetractEnvStage)}, evidenceSource); err != nil {
			t.Fatalf("second (stage) write: %v", err)
		}
		assertNamespaceEnvironmentEdges(ctx, t, exec, edgeQ, nsEnvRetractUID2, []string{nsEnvRetractEnvStage}, "retract+write: prod->stage leaves exactly one edge at stage")
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"uid": nsEnvRetractUID2}, 1, "node survives: prod->stage")
	})
}

// assertNamespaceEnvironmentEdges reads every TARGETS_ENVIRONMENT edge's
// target Environment.name from the given namespace uid and asserts the set
// matches want exactly (order-independent, duplicate-sensitive), so a stale
// second edge from a broken retract fails the assertion.
func assertNamespaceEnvironmentEdges(ctx context.Context, t *testing.T, exec liveExecutor, cypherText, uid string, want []string, msg string) {
	t.Helper()
	rows, err := exec.Run(ctx, cypherText, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("%s: query error: %v", msg, err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		name, _ := row["env"].(string)
		got = append(got, name)
	}
	if len(got) != len(want) {
		t.Fatalf("%s: TARGETS_ENVIRONMENT edges = %v, want %v", msg, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: TARGETS_ENVIRONMENT edges = %v, want %v", msg, got, want)
		}
	}
}

// assertNamespaceNodeProperties asserts the KubernetesNamespace node at uid
// has n.environment/n.evidence_class matching wantEnvironment/
// wantEvidenceClass and n.environment_state matching wantState. An empty
// wantEnvironment or wantEvidenceClass means the property must be
// null/absent -- the postcondition
// canonicalKubernetesNamespaceUpsertCypher's "SET n.environment = null,
// n.evidence_class = null" is responsible for under the real production
// managed-transaction ExecuteGroup dispatch path (codex review follow-up,
// #5434): a REMOVE-based clear was proven live to fail under that path (see
// evidence-5434-namespace-environment-retract.md), so this assertion is the
// live proof the SET ... = null replacement actually clears node-property
// truth, not just the TARGETS_ENVIRONMENT edge.
func assertNamespaceNodeProperties(ctx context.Context, t *testing.T, exec liveExecutor, cypherText, uid, wantEnvironment, wantEvidenceClass, wantState, msg string) {
	t.Helper()
	rows, err := exec.Run(ctx, cypherText, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("%s: query error: %v", msg, err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s: node read returned %d rows, want 1", msg, len(rows))
	}
	row := rows[0]

	if wantEnvironment == "" {
		if row["environment"] != nil {
			t.Fatalf("%s: n.environment = %v, want null/absent", msg, row["environment"])
		}
	} else if got, _ := row["environment"].(string); got != wantEnvironment {
		t.Fatalf("%s: n.environment = %v, want %q", msg, row["environment"], wantEnvironment)
	}

	if wantEvidenceClass == "" {
		if row["evidence_class"] != nil {
			t.Fatalf("%s: n.evidence_class = %v, want null/absent", msg, row["evidence_class"])
		}
	} else if got, _ := row["evidence_class"].(string); got != wantEvidenceClass {
		t.Fatalf("%s: n.evidence_class = %v, want %q", msg, row["evidence_class"], wantEvidenceClass)
	}

	if got, _ := row["environment_state"].(string); got != wantState {
		t.Fatalf("%s: n.environment_state = %v, want %q", msg, row["environment_state"], wantState)
	}
}

// cleanupNsEnvRetractScope removes every KubernetesNamespace node this test
// creates and its marker-scoped Environment nodes, so a rerun starts clean.
// The Environment names are marker-scoped (never the bare canonical
// "prod"/"stage"), so an exact-match DETACH DELETE is safe -- no other test
// in this package or a shared container can reference these names.
func cleanupNsEnvRetractScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n:KubernetesNamespace) WHERE n.uid STARTS WITH $marker DETACH DELETE n`,
			Parameters: map[string]any{"marker": nsEnvRetractMarker},
		},
		{
			Cypher:     `MATCH (e:Environment) WHERE e.name IN $envs DETACH DELETE e`,
			Parameters: map[string]any{"envs": []string{nsEnvRetractEnvProd, nsEnvRetractEnvStage}},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup kubernetes namespace environment retract scope: %v", err)
		}
	}
}
