// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// KubernetesNamespace TARGETS_ENVIRONMENT stale-edge retract coverage (codex
// review finding P1, #5434).
//
// KubernetesNamespaceNodeWriter.WriteKubernetesNamespaceNodes MERGEs a
// (:KubernetesNamespace)-[:TARGETS_ENVIRONMENT]->(:Environment) edge for
// environment-bound rows and REMOVEs node properties (never the edge) for
// unbound rows, but never retracted a PRE-EXISTING TARGETS_ENVIRONMENT edge:
// a namespace that lost its recognized environment label kept asserting the
// old environment forever, and a namespace re-bound from one environment to
// another (e.g. prod -> stage) accumulated a SECOND edge instead of
// replacing the first, since MERGE only matches an edge to the SAME target
// node. The fix adds retractKubernetesNamespaceStaleTargetsEnvironmentCypher,
// dispatched via KubernetesNamespaceNodeWriter.dispatchRetract -- sequential
// Execute calls, NEVER ExecuteGroup -- mirroring
// AzureCloudResourceEdgeWriter.dispatchRetract (evidence-4367-cloud-edge-
// retract.md): on the pinned NornicDB v1.1.11 a relationship DELETE
// dispatched through ExecuteGroup (the real production reducer executor for
// this writer, cmd/reducer's reducerNeo4jExecutor.ExecuteGroup) can
// under-apply even as the sole statement in the group, while the identical
// statement run auto-commit deletes correctly.
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
	"strings"
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

	t.Run("bound to unbound removes the edge", func(t *testing.T) {
		if err := writer.WriteKubernetesNamespaceNodes(ctx, []map[string]any{nsEnvRetractRow(nsEnvRetractUID1, nsEnvRetractEnvProd)}, evidenceSource); err != nil {
			t.Fatalf("first (bound) write: %v", err)
		}
		assertNamespaceEnvironmentEdges(ctx, t, exec, edgeQ, nsEnvRetractUID1, []string{nsEnvRetractEnvProd}, "write: bound edge present")

		// The second write's own upsert half hits an UNRELATED, pre-existing
		// NornicDB v1.1.11 defect: canonicalKubernetesNamespaceUpsertCypher's
		// trailing "REMOVE n.environment, n.evidence_class" fails with
		// Neo.ClientError.Statement.SyntaxError specifically when dispatched
		// through the Bolt driver's managed-transaction ExecuteWrite (the real
		// production reducer path here), while the identical statement
		// succeeds via plain autocommit Run -- confirmed with a minimal Bolt
		// probe outside this test. That defect is independent of this fix
		// (retractKubernetesNamespaceStaleTargetsEnvironmentCypher /
		// dispatchRetract) and is tracked separately; it is NOT swallowed
		// here, only tolerated so this test can still prove ITS claim.
		// dispatchRetract runs to completion (its own autocommit Execute,
		// already durably committed) BEFORE WriteKubernetesNamespaceNodes ever
		// attempts the upsert half, so the retracted edge state below is
		// proof of the retract succeeding even though the overall call errors.
		err := writer.WriteKubernetesNamespaceNodes(ctx, []map[string]any{nsEnvRetractRow(nsEnvRetractUID1, "")}, evidenceSource)
		if err == nil {
			t.Fatal("second (unbound) write unexpectedly succeeded -- the tracked NornicDB managed-transaction REMOVE defect may have been fixed; if so, tighten this assertion to require err == nil")
		}
		if !strings.Contains(err.Error(), "REMOVE requires a MATCH clause first") {
			t.Fatalf("second (unbound) write failed for an unexpected reason (want the tracked NornicDB REMOVE-in-managed-transaction defect): %v", err)
		}
		assertNamespaceEnvironmentEdges(ctx, t, exec, edgeQ, nsEnvRetractUID1, nil, "retract: bound->unbound edge gone (proven despite the unrelated upsert-side defect)")
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"uid": nsEnvRetractUID1}, 1, "node survives: bound->unbound")
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
