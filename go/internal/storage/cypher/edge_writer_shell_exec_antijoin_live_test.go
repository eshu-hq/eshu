// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// legacyShellCommandCleanupCypher is a verbatim copy of the pre-#5310
// cleanupOrphanShellCommandsCypher (see git history of
// edge_writer_shell_exec.go before this branch). It is reproduced here, not
// imported, specifically to demonstrate on the live backend that the
// `COUNT { (target)--() } = 0` relationship-existence predicate is a
// permanently-true tautology that deletes every in-scope ShellCommand,
// connected or not -- the RED reproduction the #5310 anti-join fixes.
const legacyShellCommandCleanupCypher = `UNWIND $repo_ids AS repo_id
MATCH (target:ShellCommand {repo_id: repo_id})
WHERE target.evidence_source = $evidence_source
  AND COUNT { (target)--() } = 0
DELETE target`

// TestLiveShellCommandAntiJoinPreservesConnectedNode is the committed live
// discriminating regression for #5310. It first reproduces the historical bug
// (the origin/main `COUNT { (target)--() } = 0` cleanup deletes a ShellCommand
// that still has a relationship, because the predicate is a tautology on the
// pinned NornicDB backends), then proves the new S1/S2 anti-join cleanup
// (EdgeWriter.RetractEdges -> cleanupOrphanShellCommands) preserves the
// connected ShellCommand while deleting the genuine orphan.
//
// The connected ShellCommand's surviving edge is stamped with a DIFFERENT
// evidence_source than the one being retracted, proving the S2 connected-keys
// read correctly treats "has any relationship" as backend-scope connectivity
// -- not scoped to the retract's own evidence_source -- exactly like the edge
// DELETE step that runs immediately before it (which only removes
// same-evidence-source edges and deliberately leaves other-evidence-source
// edges in place).
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. bolt://127.0.0.1:17689 or
// bolt://127.0.0.1:17690). When unset the test skips.
func TestLiveShellCommandAntiJoinPreservesConnectedNode(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	repoID := "5310-repo-" + suffix
	connectedUID := "5310-connected-" + suffix
	orphanUID := "5310-orphan-" + suffix
	peerFnUID := "5310-peer-fn-" + suffix
	scopeEvidenceSource := "reducer/shell-exec-5310-live"
	otherEvidenceSource := "reducer/shell-exec-5310-other"

	cleanupScope := func(ctx context.Context) error {
		return boltWriteStatement(
			ctx, runner,
			`MATCH (n) WHERE n.uid IN $uids DETACH DELETE n`,
			map[string]any{"uids": []string{connectedUID, orphanUID, peerFnUID}},
		)
	}
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := cleanupScope(cleanCtx); err != nil {
			t.Errorf("cleanup live shell command anti-join proof: %v", err)
		}
	})
	// Best-effort pre-clean in case a prior run crashed before cleanup.
	_ = cleanupScope(ctx)

	seedShellCommand := func(uid string) {
		if err := boltWriteStatement(
			ctx, runner,
			`MERGE (n:ShellCommand {uid: $uid}) SET n.repo_id = $repo_id, n.evidence_source = $evidence_source`,
			map[string]any{"uid": uid, "repo_id": repoID, "evidence_source": scopeEvidenceSource},
		); err != nil {
			t.Fatalf("seed ShellCommand %s: %v", uid, err)
		}
	}
	seedShellCommand(connectedUID)
	seedShellCommand(orphanUID)

	// connectShellCommand links peerFnUID to targetUID under
	// otherEvidenceSource. It MERGEs the Function node first (its own
	// statement), then MERGEs the relationship against two already-existing
	// MATCHed nodes: chaining three MERGE clauses
	// (MERGE node MERGE node MERGE relationship) in a single statement was
	// probed live and silently drops the relationship MERGE on the pinned
	// NornicDB backends even though both node MERGEs individually succeed --
	// a shape distinct from, and less reliable than, the
	// MATCH-existing-node/MERGE-relationship pattern
	// batchCanonicalShellExecUpsertCypher already uses in production.
	connectShellCommand := func(targetUID string) {
		if err := boltWriteStatement(
			ctx, runner,
			`MERGE (f:Function {uid: $fn_uid})`,
			map[string]any{"fn_uid": peerFnUID},
		); err != nil {
			t.Fatalf("merge Function %s: %v", peerFnUID, err)
		}
		if err := boltWriteStatement(
			ctx, runner,
			`MATCH (f:Function {uid: $fn_uid})
MATCH (t:ShellCommand {uid: $target_uid})
MERGE (f)-[rel:EXECUTES_SHELL]->(t)
SET rel.evidence_source = $other_evidence_source`,
			map[string]any{"fn_uid": peerFnUID, "target_uid": targetUID, "other_evidence_source": otherEvidenceSource},
		); err != nil {
			t.Fatalf("connect ShellCommand %s: %v", targetUID, err)
		}
	}
	// The connected ShellCommand keeps a relationship from a Function under a
	// DIFFERENT evidence source than the one being retracted -- an edge the
	// prior BuildRetractShellExecEdges step (scoped to scopeEvidenceSource)
	// would not have deleted.
	connectShellCommand(connectedUID)

	// --- RED: reproduce the origin/main COUNT{}=0 tautology deleting the
	// connected node ---
	//
	// The legacy statement must run through the same autocommit path
	// production statements use (boltTestExecutor.Execute -> runCypherSingle,
	// matching Executor.Execute), not the grouped/managed-transaction path
	// (boltWriteStatement -> runCypherGroup) used for test seeding: a single
	// DELETE issued inside a managed transaction under-applies on the pinned
	// NornicDB backends (see evidence-4367-content-edge-retract-sequential.md
	// problem 2) and would silently mask the COUNT{}=0 tautology this
	// subtest exists to demonstrate.
	legacyExecutor := &boltTestExecutor{runner: runner}
	t.Run("historical_count_predicate_deletes_connected_node", func(t *testing.T) {
		if err := legacyExecutor.Execute(ctx, Statement{
			Cypher: legacyShellCommandCleanupCypher,
			Parameters: map[string]any{
				"repo_ids":        []string{repoID},
				"evidence_source": scopeEvidenceSource,
			},
		}); err != nil {
			t.Fatalf("legacy cleanup statement errored: %v", err)
		}
		connectedCount, err := boltCount(ctx, runner,
			`MATCH (n:ShellCommand {uid: $uid}) RETURN count(n) AS count`,
			map[string]any{"uid": connectedUID})
		if err != nil {
			t.Fatalf("read back connected node after legacy cleanup: %v", err)
		}
		if connectedCount != 0 {
			t.Fatalf("RED reproduction failed: legacy COUNT{}=0 cleanup unexpectedly preserved the connected node (count=%d) -- the historical predicate is supposed to be a tautology that deletes it too", connectedCount)
		}
		t.Logf("confirmed: legacy COUNT{}=0 cleanup deleted the connected ShellCommand -- origin/main over-deletes connected nodes")

		// Re-seed both nodes (and the relationship) for the GREEN half below.
		seedShellCommand(connectedUID)
		seedShellCommand(orphanUID)
		connectShellCommand(connectedUID)
	})

	// --- GREEN: the new anti-join cleanup preserves the connected node and
	// deletes the genuine orphan ---
	writer := NewEdgeWriter(&boltTestExecutor{runner: runner}, 0)
	writer.Reader = &boltOrphanSweepReader{runner: runner}

	rows := []reducer.SharedProjectionIntentRow{
		{RepositoryID: repoID, Payload: map[string]any{"repo_id": repoID}},
	}
	if err := writer.RetractEdges(ctx, reducer.DomainShellExec, rows, scopeEvidenceSource); err != nil {
		t.Fatalf("RetractEdges: %v", err)
	}

	assertBoltCount(t, ctx, runner,
		`MATCH (n:ShellCommand {uid: $uid}) RETURN count(n) AS count`,
		map[string]any{"uid": connectedUID}, 1, "connected ShellCommand preserved")
	assertBoltCount(t, ctx, runner,
		`MATCH (n:ShellCommand {uid: $uid}) RETURN count(n) AS count`,
		map[string]any{"uid": orphanUID}, 0, "orphan ShellCommand deleted")
	// The surviving other-evidence-source edge must still be there too --
	// proves the cleanup did not silently DETACH DELETE the connected node.
	assertBoltCount(t, ctx, runner,
		`MATCH (:Function {uid: $fn_uid})-[r:EXECUTES_SHELL]->(:ShellCommand {uid: $target_uid}) RETURN count(r) AS count`,
		map[string]any{"fn_uid": peerFnUID, "target_uid": connectedUID}, 1, "other-evidence-source edge preserved")
}
