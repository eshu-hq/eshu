// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized inheritance edge retract coverage (C-14 #4367 retract
// axis), the inheritance sibling of the #5116 code-call retract fix.
//
// The inheritance retract Cyphers matched their child node by a node-label
// DISJUNCTION (MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol
// {repo_id})). On NornicDB a node-label disjunction in a MATCH returns zero rows,
// so the retract deleted NOTHING and stale INHERITS/IMPLEMENTS/OVERRIDES/ALIASES
// edges survived every reprojection. The fix fans the retract out to one
// single-label statement per child label, run sequentially (NornicDB v1.1.11
// also drops labels on an unlabeled scan and under-applies grouped multi-DELETE
// transactions — see the #5116 code-call fix).
//
// The test drives the production write and retract paths
// (cypher.EdgeWriter.WriteEdges / RetractEdges for reducer.DomainInheritanceEdges).
// It writes INHERITS (Class->Class) and IMPLEMENTS (Class->Interface) edges in
// one repo scope plus an out-of-scope INHERITS edge, retracts the in-scope repo,
// and asserts the in-scope edges are gone (0), the out-of-scope edge survives (1,
// a scoped retract not a wipe), and every endpoint node survives. Before the fix
// the in-scope assertions fail because the broken disjunction retracts nothing.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	inheritEdgeInScopeRepoID  = "replay-inherit-edge:inscope"
	inheritEdgeOutScopeRepoID = "replay-inherit-edge:outscope"
	inheritEdgeInScopePath    = "inherit-edge/in/mod.py"
	inheritEdgeOutScopePath   = "inherit-edge/out/mod.py"
	inheritEdgeEvidenceSource = "reducer/inheritance"

	inhChildA  = "inherit-edge:cls:childA"
	inhParentA = "inherit-edge:cls:parentA"
	inhChildB  = "inherit-edge:cls:childB"
	inhParentB = "inherit-edge:iface:parentB"
	inhChildC  = "inherit-edge:cls:childC"
	inhParentC = "inherit-edge:cls:parentC"
	inhClsOvrC = "inherit-edge:cls:overrideChild"
	inhClsOvrP = "inherit-edge:cls:overrideParent"
	inhFnAliC  = "inherit-edge:fn:aliasChild"
	inhFnAliP  = "inherit-edge:fn:aliasParent"
)

// TestReducerInheritanceEdgeRetractGraphTruth proves the inheritance edge retract
// path (INHERITS/IMPLEMENTS/OVERRIDES/ALIASES) deletes only the in-scope edges
// on a real NornicDB.
// It is the failing-then-green regression for the #4367 inheritance slice.
func TestReducerInheritanceEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the inheritance-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupInheritEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupInheritEdgeScope(cleanCtx, t, exec)
	})

	seedInheritEdgeNodes(ctx, t, exec)

	writer := cypher.NewEdgeWriter(exec, 0)

	// Production write path: typed endpoints route to exact-label MATCH Cyphers
	// (NornicDB matches single labels), so these edges are actually created.
	writeRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "inherits-in", RepositoryID: inheritEdgeInScopeRepoID, Payload: map[string]any{
			"relationship_type": "INHERITS",
			"child_entity_id":   inhChildA, "child_entity_type": "Class",
			"parent_entity_id": inhParentA, "parent_entity_type": "Class",
		}},
		{IntentID: "implements-in", RepositoryID: inheritEdgeInScopeRepoID, Payload: map[string]any{
			"relationship_type": "IMPLEMENTS",
			"child_entity_id":   inhChildB, "child_entity_type": "Class",
			"parent_entity_id": inhParentB, "parent_entity_type": "Interface",
		}},
		// OVERRIDES mirrors the reducer's class-level trait-override emission
		// (Class child); ALIASES mirrors the method-level emission (Function
		// child), so together they cover both child shapes production writes.
		{IntentID: "overrides-in", RepositoryID: inheritEdgeInScopeRepoID, Payload: map[string]any{
			"relationship_type": "OVERRIDES",
			"child_entity_id":   inhClsOvrC, "child_entity_type": "Class",
			"parent_entity_id": inhClsOvrP, "parent_entity_type": "Class",
		}},
		{IntentID: "aliases-in", RepositoryID: inheritEdgeInScopeRepoID, Payload: map[string]any{
			"relationship_type": "ALIASES",
			"child_entity_id":   inhFnAliC, "child_entity_type": "Function",
			"parent_entity_id": inhFnAliP, "parent_entity_type": "Function",
		}},
		{IntentID: "inherits-out", RepositoryID: inheritEdgeOutScopeRepoID, Payload: map[string]any{
			"relationship_type": "INHERITS",
			"child_entity_id":   inhChildC, "child_entity_type": "Class",
			"parent_entity_id": inhParentC, "parent_entity_type": "Class",
		}},
	}
	if err := writer.WriteEdges(ctx, reducer.DomainInheritanceEdges, writeRows, inheritEdgeEvidenceSource); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}

	inheritsQ := "MATCH (:Class {uid: $s})-[r:INHERITS]->(:Class {uid: $t}) RETURN count(r)"
	implementsQ := "MATCH (:Class {uid: $s})-[r:IMPLEMENTS]->(:Interface {uid: $t}) RETURN count(r)"
	overridesQ := "MATCH (:Class {uid: $s})-[r:OVERRIDES]->(:Class {uid: $t}) RETURN count(r)"
	aliasesQ := "MATCH (:Function {uid: $s})-[r:ALIASES]->(:Function {uid: $t}) RETURN count(r)"
	nodeQ := "MATCH (n {uid: $u}) RETURN count(n)"
	inInherits := map[string]any{"s": inhChildA, "t": inhParentA}
	inImplements := map[string]any{"s": inhChildB, "t": inhParentB}
	inOverrides := map[string]any{"s": inhClsOvrC, "t": inhClsOvrP}
	inAliases := map[string]any{"s": inhFnAliC, "t": inhFnAliP}
	outInherits := map[string]any{"s": inhChildC, "t": inhParentC}

	assertEdgeCount(ctx, t, exec, inheritsQ, inInherits, 1, "write: in-scope INHERITS present")
	assertEdgeCount(ctx, t, exec, implementsQ, inImplements, 1, "write: in-scope IMPLEMENTS present")
	assertEdgeCount(ctx, t, exec, overridesQ, inOverrides, 1, "write: in-scope OVERRIDES present")
	assertEdgeCount(ctx, t, exec, aliasesQ, inAliases, 1, "write: in-scope ALIASES present")
	assertEdgeCount(ctx, t, exec, inheritsQ, outInherits, 1, "write: out-of-scope INHERITS present")

	// Production retract path: repo-scoped rows route to per-child-label
	// statements, run sequentially (#5116/#4367).
	retractRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "retract", RepositoryID: inheritEdgeInScopeRepoID},
	}
	if err := writer.RetractEdges(ctx, reducer.DomainInheritanceEdges, retractRows, inheritEdgeEvidenceSource); err != nil {
		t.Fatalf("RetractEdges: %v", err)
	}

	// Fix: in-scope inheritance edges of every rel-type must be gone.
	assertEdgeCount(ctx, t, exec, inheritsQ, inInherits, 0, "retract: in-scope INHERITS gone")
	assertEdgeCount(ctx, t, exec, implementsQ, inImplements, 0, "retract: in-scope IMPLEMENTS gone")
	assertEdgeCount(ctx, t, exec, overridesQ, inOverrides, 0, "retract: in-scope OVERRIDES gone")
	assertEdgeCount(ctx, t, exec, aliasesQ, inAliases, 0, "retract: in-scope ALIASES gone")
	// Scoped retract, not a wipe: the out-of-scope repo's edge survives.
	assertEdgeCount(ctx, t, exec, inheritsQ, outInherits, 1, "retract: out-of-scope INHERITS survives")
	// Edge retract must never delete endpoint nodes.
	for _, uid := range []string{
		inhChildA, inhParentA, inhChildB, inhParentB, inhChildC, inhParentC,
		inhClsOvrC, inhClsOvrP, inhFnAliC, inhFnAliP,
	} {
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": uid}, 1, "node survives: "+uid)
	}
}

// seedInheritEdgeNodes creates the child/parent nodes the write path MATCHes by
// exact label + uid. In-scope children carry the in-scope repo_id so the
// repo-scoped retract binds them; the out-of-scope child carries a different
// repo_id so its edge must survive the retract.
func seedInheritEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (:Class {uid: $childA, repo_id: $in, path: $inPath}),
       (:Class {uid: $parentA, repo_id: $in, path: $inPath}),
       (:Class {uid: $childB, repo_id: $in, path: $inPath}),
       (:Interface {uid: $parentB, repo_id: $in, path: $inPath}),
       (:Class {uid: $clsOvrC, repo_id: $in, path: $inPath}),
       (:Class {uid: $clsOvrP, repo_id: $in, path: $inPath}),
       (:Function {uid: $fnAliC, repo_id: $in, path: $inPath}),
       (:Function {uid: $fnAliP, repo_id: $in, path: $inPath}),
       (:Class {uid: $childC, repo_id: $out, path: $outPath}),
       (:Class {uid: $parentC, repo_id: $out, path: $outPath})`,
		Parameters: map[string]any{
			"clsOvrC": inhClsOvrC,
			"clsOvrP": inhClsOvrP,
			"fnAliC":  inhFnAliC,
			"fnAliP":  inhFnAliP,
			"childA":  inhChildA,
			"parentA": inhParentA,
			"childB":  inhChildB,
			"parentB": inhParentB,
			"childC":  inhChildC,
			"parentC": inhParentC,
			"in":      inheritEdgeInScopeRepoID,
			"out":     inheritEdgeOutScopeRepoID,
			"inPath":  inheritEdgeInScopePath,
			"outPath": inheritEdgeOutScopePath,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed inherit-edge nodes: %v", err)
	}
}

// cleanupInheritEdgeScope removes every node this test creates, in both repo
// scopes, so a rerun starts clean and leaves no residue for sibling tests.
func cleanupInheritEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n) WHERE n.repo_id IN [$in, $out] DETACH DELETE n`,
		Parameters: map[string]any{"in": inheritEdgeInScopeRepoID, "out": inheritEdgeOutScopeRepoID},
	}); err != nil {
		t.Fatalf("cleanup inherit-edge scope: %v", err)
	}
}
