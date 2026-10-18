// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// DSN-gated live proofs for the #6228 retract-registry contract
// (docs/internal/design/6228-materialized-edge-retract-registry-contract.md).
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. neo4j://127.0.0.1:7687).
// When unset every test below skips; the hermetic registry guard in
// edge/materialized carries the contract in normal CI.
//
// Every test seeds through raw Cypher with 6228-prefixed uids (unique per
// test, so parallel runs against one shared backend stay isolated) and
// retracts through the REAL writer method over the boltTestExecutor dispatch
// path — the exact path the reducer uses. A foreign edge uses a real type
// owned by another family (USES) with the SAME evidence_source: under the old
// bare-[rel] retracts that edge was deleted with the family's own edges; under
// the named alternations it must survive. A sibling carrying the family's own
// type but a FOREIGN evidence_source (or a foreign scope) must likewise
// survive, proving the predicates still scope the delete.

// TestLiveSecurityGroupSGRuleRetractSparesForeignType is the kind-C
// foreign-edge regression for security_group_sg_rule: the SG -> rule retract
// names [rel:ALLOWS_INGRESS|ALLOWS_EGRESS], so a USES edge sharing the
// endpoints and evidence_source survives while the owned edge is reaped.
func TestLiveSecurityGroupSGRuleRetractSparesForeignType(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())
	ctx := context.Background()

	const (
		sgUID      = "6228-sg-foreign-sg"
		ruleUID    = "6228-sg-foreign-rule"
		sibSGUID   = "6228-sg-foreign-sib-sg"
		sibRuleUID = "6228-sg-foreign-sib-rule"
		scopeID    = "6228-sg-foreign-scope"
		source     = "6228-sg-foreign-source"
		sibSource  = "6228-sg-foreign-sib-source"
	)

	for _, seed := range []struct {
		cypher string
		params map[string]any
	}{
		{`MERGE (:CloudResource {uid: $uid})`, map[string]any{"uid": sgUID}},
		{`MERGE (:SecurityGroupRule {uid: $uid})`, map[string]any{"uid": ruleUID}},
		{`MERGE (:CloudResource {uid: $uid})`, map[string]any{"uid": sibSGUID}},
		{`MERGE (:SecurityGroupRule {uid: $uid})`, map[string]any{"uid": sibRuleUID}},
		{
			strings.Join([]string{
				`MATCH (sg:CloudResource {uid: $sg_uid})`,
				`MATCH (rule:SecurityGroupRule {uid: $rule_uid})`,
				`MERGE (sg)-[rel:ALLOWS_INGRESS]->(rule)`,
				`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
			}, " "),
			map[string]any{"sg_uid": sgUID, "rule_uid": ruleUID, "scope_id": scopeID, "source": source, "gen": "gen-1"},
		},
		{
			strings.Join([]string{
				`MATCH (sg:CloudResource {uid: $sg_uid})`,
				`MATCH (rule:SecurityGroupRule {uid: $rule_uid})`,
				`MERGE (sg)-[rel:USES]->(rule)`,
				`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
			}, " "),
			map[string]any{"sg_uid": sgUID, "rule_uid": ruleUID, "scope_id": scopeID, "source": source, "gen": "gen-1"},
		},
		{
			strings.Join([]string{
				`MATCH (sg:CloudResource {uid: $sg_uid})`,
				`MATCH (rule:SecurityGroupRule {uid: $rule_uid})`,
				`MERGE (sg)-[rel:ALLOWS_EGRESS]->(rule)`,
				`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
			}, " "),
			map[string]any{"sg_uid": sibSGUID, "rule_uid": sibRuleUID, "scope_id": scopeID, "source": sibSource, "gen": "gen-1"},
		},
	} {
		if err := boltWriteStatement(ctx, runner, seed.cypher, seed.params); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	executor := &boltTestExecutor{runner: runner}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	if err := writer.RetractSecurityGroupReachability(ctx, []string{scopeID}, "gen-2", source); err != nil {
		t.Fatalf("RetractSecurityGroupReachability returned error: %v", err)
	}

	count := func(cypher string, params map[string]any) int64 {
		t.Helper()
		n, err := boltCount(ctx, runner, cypher, params)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	sgParams := map[string]any{"sg_uid": sgUID, "rule_uid": ruleUID}
	if n := count(`MATCH (:CloudResource {uid: $sg_uid})-[rel:ALLOWS_INGRESS]->(:SecurityGroupRule {uid: $rule_uid}) RETURN count(rel) AS count`, sgParams); n != 0 {
		t.Errorf("owned ALLOWS_INGRESS count = %d, want 0 (retract must reap it)", n)
	}
	if n := count(`MATCH (:CloudResource {uid: $sg_uid})-[rel:USES]->(:SecurityGroupRule {uid: $rule_uid}) RETURN count(rel) AS count`, sgParams); n != 1 {
		t.Errorf("foreign USES count = %d, want 1 (named alternation must spare the same-source foreign type the bare [rel] deleted)", n)
	}
	if n := count(`MATCH (:CloudResource {uid: $sg_uid})-[rel:ALLOWS_EGRESS]->(:SecurityGroupRule {uid: $rule_uid}) RETURN count(rel) AS count`,
		map[string]any{"sg_uid": sibSGUID, "rule_uid": sibRuleUID}); n != 1 {
		t.Errorf("foreign-source sibling ALLOWS_EGRESS count = %d, want 1 (evidence_source predicate must scope the delete)", n)
	}
}

// TestLiveSecurityGroupSGRuleByUIDsRetractSparesForeignType is the kind-C
// foreign-edge regression for the ledger-anchored security_group_sg_rule
// variant (F1: the ByUIDs const carried a bare [rel] while the guard held
// only the full-scope retract). It seeds the same shape as
// TestLiveSecurityGroupSGRuleRetractSparesForeignType and retracts through
// the REAL RetractSecurityGroupReachabilityByUIDs writer method: the owned
// edge under a listed uid is reaped, the same-source foreign-type edge
// survives the named alternation, the foreign-source sibling survives the
// evidence_source predicate, and an owned edge under an unlisted uid
// survives the ledger anchor.
func TestLiveSecurityGroupSGRuleByUIDsRetractSparesForeignType(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())
	ctx := context.Background()

	const (
		sgUID       = "6228-sg-byuids-sg"
		ruleUID     = "6228-sg-byuids-rule"
		sibSGUID    = "6228-sg-byuids-sib-sg"
		sibRuleUID  = "6228-sg-byuids-sib-rule"
		otherSGUID  = "6228-sg-byuids-other-sg"
		otherRuleID = "6228-sg-byuids-other-rule"
		scopeID     = "6228-sg-byuids-scope"
		source      = "6228-sg-byuids-source"
		sibSource   = "6228-sg-byuids-sib-source"
	)

	for _, seed := range []struct {
		cypher string
		params map[string]any
	}{
		{`MERGE (:CloudResource {uid: $uid})`, map[string]any{"uid": sgUID}},
		{`MERGE (:SecurityGroupRule {uid: $uid})`, map[string]any{"uid": ruleUID}},
		{`MERGE (:CloudResource {uid: $uid})`, map[string]any{"uid": sibSGUID}},
		{`MERGE (:SecurityGroupRule {uid: $uid})`, map[string]any{"uid": sibRuleUID}},
		{`MERGE (:CloudResource {uid: $uid})`, map[string]any{"uid": otherSGUID}},
		{`MERGE (:SecurityGroupRule {uid: $uid})`, map[string]any{"uid": otherRuleID}},
		{
			strings.Join([]string{
				`MATCH (sg:CloudResource {uid: $sg_uid})`,
				`MATCH (rule:SecurityGroupRule {uid: $rule_uid})`,
				`MERGE (sg)-[rel:ALLOWS_INGRESS]->(rule)`,
				`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
			}, " "),
			map[string]any{"sg_uid": sgUID, "rule_uid": ruleUID, "scope_id": scopeID, "source": source, "gen": "gen-1"},
		},
		{
			strings.Join([]string{
				`MATCH (sg:CloudResource {uid: $sg_uid})`,
				`MATCH (rule:SecurityGroupRule {uid: $rule_uid})`,
				`MERGE (sg)-[rel:USES]->(rule)`,
				`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
			}, " "),
			map[string]any{"sg_uid": sgUID, "rule_uid": ruleUID, "scope_id": scopeID, "source": source, "gen": "gen-1"},
		},
		{
			strings.Join([]string{
				`MATCH (sg:CloudResource {uid: $sg_uid})`,
				`MATCH (rule:SecurityGroupRule {uid: $rule_uid})`,
				`MERGE (sg)-[rel:ALLOWS_EGRESS]->(rule)`,
				`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
			}, " "),
			map[string]any{"sg_uid": sibSGUID, "rule_uid": sibRuleUID, "scope_id": scopeID, "source": sibSource, "gen": "gen-1"},
		},
		{
			strings.Join([]string{
				`MATCH (sg:CloudResource {uid: $sg_uid})`,
				`MATCH (rule:SecurityGroupRule {uid: $rule_uid})`,
				`MERGE (sg)-[rel:ALLOWS_INGRESS]->(rule)`,
				`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
			}, " "),
			map[string]any{"sg_uid": otherSGUID, "rule_uid": otherRuleID, "scope_id": scopeID, "source": source, "gen": "gen-1"},
		},
	} {
		if err := boltWriteStatement(ctx, runner, seed.cypher, seed.params); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	executor := &boltTestExecutor{runner: runner}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	if err := writer.RetractSecurityGroupReachabilityByUIDs(ctx, []string{sgUID, sibSGUID}, []string{scopeID}, source); err != nil {
		t.Fatalf("RetractSecurityGroupReachabilityByUIDs returned error: %v", err)
	}

	count := func(cypher string, params map[string]any) int64 {
		t.Helper()
		n, err := boltCount(ctx, runner, cypher, params)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	sgParams := map[string]any{"sg_uid": sgUID, "rule_uid": ruleUID}
	if n := count(`MATCH (:CloudResource {uid: $sg_uid})-[rel:ALLOWS_INGRESS]->(:SecurityGroupRule {uid: $rule_uid}) RETURN count(rel) AS count`, sgParams); n != 0 {
		t.Errorf("owned ALLOWS_INGRESS count = %d, want 0 (ByUIDs retract must reap it)", n)
	}
	if n := count(`MATCH (:CloudResource {uid: $sg_uid})-[rel:USES]->(:SecurityGroupRule {uid: $rule_uid}) RETURN count(rel) AS count`, sgParams); n != 1 {
		t.Errorf("foreign USES count = %d, want 1 (named alternation must spare the same-source foreign type the bare [rel] deleted)", n)
	}
	if n := count(`MATCH (:CloudResource {uid: $sg_uid})-[rel:ALLOWS_EGRESS]->(:SecurityGroupRule {uid: $rule_uid}) RETURN count(rel) AS count`,
		map[string]any{"sg_uid": sibSGUID, "rule_uid": sibRuleUID}); n != 1 {
		t.Errorf("foreign-source sibling ALLOWS_EGRESS count = %d, want 1 (evidence_source predicate must scope the delete)", n)
	}
	if n := count(`MATCH (:CloudResource {uid: $sg_uid})-[rel:ALLOWS_INGRESS]->(:SecurityGroupRule {uid: $rule_uid}) RETURN count(rel) AS count`,
		map[string]any{"sg_uid": otherSGUID, "rule_uid": otherRuleID}); n != 1 {
		t.Errorf("unlisted-uid ALLOWS_INGRESS count = %d, want 1 (ledger anchor must scope the delete to listed uids)", n)
	}
}

// TestLiveObservabilityCoverageRetractSparesForeignType is the kind-C
// foreign-edge regression for observability_coverage: both retract variants
// name the five-type AWS_COVERS_* alternation, so a USES edge sharing the
// endpoints and evidence_source survives while the owned edge is reaped.
func TestLiveObservabilityCoverageRetractSparesForeignType(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())
	ctx := context.Background()

	const (
		obsUID    = "6228-obs-foreign-obs"
		tgtUID    = "6228-obs-foreign-tgt"
		sibObsUID = "6228-obs-foreign-sib-obs"
		sibTgtUID = "6228-obs-foreign-sib-tgt"
		scopeID   = "6228-obs-foreign-scope"
		source    = "6228-obs-foreign-source"
		sibSource = "6228-obs-foreign-sib-source"
	)

	for _, uid := range []string{obsUID, tgtUID, sibObsUID, sibTgtUID} {
		if err := boltWriteStatement(ctx, runner, `MERGE (:CloudResource {uid: $uid})`, map[string]any{"uid": uid}); err != nil {
			t.Fatalf("seed node %q: %v", uid, err)
		}
	}
	seedEdge := func(obs, tgt, relType, scope, src string) {
		t.Helper()
		if err := boltWriteStatement(ctx, runner, strings.Join([]string{
			`MATCH (obs:CloudResource {uid: $obs_uid})`,
			`MATCH (tgt:CloudResource {uid: $tgt_uid})`,
			`MERGE (obs)-[rel:` + relType + `]->(tgt)`,
			`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
		}, " "), map[string]any{
			"obs_uid": obs, "tgt_uid": tgt, "scope_id": scope, "source": src, "gen": "gen-1",
		}); err != nil {
			t.Fatalf("seed %s edge: %v", relType, err)
		}
	}
	seedEdge(obsUID, tgtUID, "AWS_COVERS_alarm", scopeID, source)
	seedEdge(obsUID, tgtUID, "USES", scopeID, source)
	seedEdge(sibObsUID, sibTgtUID, "AWS_COVERS_dashboard", scopeID, sibSource)

	executor := &boltTestExecutor{runner: runner}
	writer := NewObservabilityCoverageEdgeWriter(executor, 0)
	if err := writer.RetractObservabilityCoverageEdges(ctx, []string{scopeID}, "gen-2", source); err != nil {
		t.Fatalf("RetractObservabilityCoverageEdges returned error: %v", err)
	}

	count := func(relType, obs, tgt string) int64 {
		t.Helper()
		n, err := boltCount(ctx, runner,
			`MATCH (:CloudResource {uid: $obs_uid})-[rel:`+relType+`]->(:CloudResource {uid: $tgt_uid}) RETURN count(rel) AS count`,
			map[string]any{"obs_uid": obs, "tgt_uid": tgt})
		if err != nil {
			t.Fatalf("count %s: %v", relType, err)
		}
		return n
	}
	if n := count("AWS_COVERS_alarm", obsUID, tgtUID); n != 0 {
		t.Errorf("owned AWS_COVERS_alarm count = %d, want 0 (retract must reap it)", n)
	}
	if n := count("USES", obsUID, tgtUID); n != 1 {
		t.Errorf("foreign USES count = %d, want 1 (named alternation must spare the same-source foreign type the bare [rel] deleted)", n)
	}
	if n := count("AWS_COVERS_dashboard", sibObsUID, sibTgtUID); n != 1 {
		t.Errorf("foreign-source sibling AWS_COVERS_dashboard count = %d, want 1 (evidence_source predicate must scope the delete)", n)
	}
}

// TestLiveCodeTaintEvidenceRetractScopesToSourceAndScope proves the kind-A
// node-detach retract deletes exactly this writer's evidence nodes: the owned
// node (and its edges, including a foreign-type edge cascading with the node)
// goes, while same-scope foreign-source and same-source foreign-scope
// siblings survive with their edges.
func TestLiveCodeTaintEvidenceRetractScopesToSourceAndScope(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())
	ctx := context.Background()

	const (
		funcUID   = "6228-taint-scope-func"
		ownUID    = "6228-taint-scope-own"
		sibSrcUID = "6228-taint-scope-sib-src"
		sibScpUID = "6228-taint-scope-sib-scope"
		scopeA    = "6228-taint-scope-a"
		scopeB    = "6228-taint-scope-b"
		sourceS   = "6228-taint-scope-source"
		sourceF   = "6228-taint-scope-foreign-source"
	)

	if err := boltWriteStatement(ctx, runner, `MERGE (:Function {uid: $uid})`, map[string]any{"uid": funcUID}); err != nil {
		t.Fatalf("seed Function: %v", err)
	}
	seedNode := func(nodeUID, scope, src string) {
		t.Helper()
		if err := boltWriteStatement(ctx, runner, strings.Join([]string{
			`MATCH (f:Function {uid: $func_uid})`,
			`MERGE (ev:CodeTaintEvidence {uid: $node_uid})`,
			`SET ev.scope_id = $scope_id, ev.evidence_source = $source, ev.generation_id = $gen`,
			`MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)`,
			`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
		}, " "), map[string]any{
			"func_uid": funcUID, "node_uid": nodeUID, "scope_id": scope, "source": src, "gen": "gen-1",
		}); err != nil {
			t.Fatalf("seed node %q: %v", nodeUID, err)
		}
	}
	seedNode(ownUID, scopeA, sourceS)
	seedNode(sibSrcUID, scopeA, sourceF)
	seedNode(sibScpUID, scopeB, sourceS)
	// A foreign-type edge hanging off the doomed node: DETACH DELETE cascades
	// it with the node. It is seeded to document the cascade, not to survive.
	if err := boltWriteStatement(ctx, runner, strings.Join([]string{
		`MATCH (f:Function {uid: $func_uid})`,
		`MATCH (ev:CodeTaintEvidence {uid: $node_uid})`,
		`MERGE (f)-[rel:USES]->(ev)`,
		`SET rel.scope_id = $scope_id, rel.evidence_source = $source`,
	}, " "), map[string]any{
		"func_uid": funcUID, "node_uid": ownUID, "scope_id": scopeA, "source": sourceS,
	}); err != nil {
		t.Fatalf("seed foreign edge: %v", err)
	}

	executor := &boltTestExecutor{runner: runner}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.RetractCodeTaintEvidence(ctx, []string{scopeA}, "gen-2", sourceS); err != nil {
		t.Fatalf("RetractCodeTaintEvidence returned error: %v", err)
	}

	countNodes := func(nodeUID string) int64 {
		t.Helper()
		n, err := boltCount(ctx, runner,
			`MATCH (n:CodeTaintEvidence {uid: $uid}) RETURN count(n) AS count`,
			map[string]any{"uid": nodeUID})
		if err != nil {
			t.Fatalf("count node %q: %v", nodeUID, err)
		}
		return n
	}
	if n := countNodes(ownUID); n != 0 {
		t.Errorf("owned node count = %d, want 0", n)
	}
	if n := countNodes(sibSrcUID); n != 1 {
		t.Errorf("foreign-source sibling count = %d, want 1", n)
	}
	if n := countNodes(sibScpUID); n != 1 {
		t.Errorf("foreign-scope sibling count = %d, want 1", n)
	}
	if n, err := boltCount(ctx, runner,
		`MATCH (:Function {uid: $func_uid})-[rel:USES]->(:CodeTaintEvidence {uid: $node_uid}) RETURN count(rel) AS count`,
		map[string]any{"func_uid": funcUID, "node_uid": ownUID}); err != nil {
		t.Fatalf("count foreign edge on detached node: %v", err)
	} else if n != 0 {
		t.Errorf("foreign edge on detached node count = %d, want 0 (cascade)", n)
	}
}

// TestLiveIncidentRoutingEvidenceRetractScopesToSourceAndScope proves the
// kind-A node-detach retract for the multi-type evidence family: the owned
// incident/routing nodes and their routing edge go, while same-scope
// foreign-source and same-source foreign-scope siblings survive.
func TestLiveIncidentRoutingEvidenceRetractScopesToSourceAndScope(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())
	ctx := context.Background()

	const (
		scopeA  = "6228-routing-scope-a"
		scopeB  = "6228-routing-scope-b"
		sourceS = "6228-routing-scope-source"
		sourceF = "6228-routing-scope-foreign-source"
	)

	seedPair := func(tag, scope, src string) {
		t.Helper()
		incidentUID := "6228-routing-" + tag + "-incident"
		routingUID := "6228-routing-" + tag + "-routing"
		if err := boltWriteStatement(ctx, runner, strings.Join([]string{
			`MERGE (incident:IncidentRoutingEvidence {uid: $incident_uid})`,
			`SET incident.scope_id = $scope_id, incident.evidence_source = $source, incident.generation_id = $gen`,
			`MERGE (routing:IncidentRoutingEvidence {uid: $routing_uid})`,
			`SET routing.scope_id = $scope_id, routing.evidence_source = $source, routing.generation_id = $gen`,
			`MERGE (incident)-[rel:HAS_INTENDED_ROUTING]->(routing)`,
			`SET rel.scope_id = $scope_id, rel.evidence_source = $source, rel.generation_id = $gen`,
		}, " "), map[string]any{
			"incident_uid": incidentUID, "routing_uid": routingUID,
			"scope_id": scope, "source": src, "gen": "gen-1",
		}); err != nil {
			t.Fatalf("seed pair %q: %v", tag, err)
		}
	}
	seedPair("own", scopeA, sourceS)
	seedPair("sibsrc", scopeA, sourceF)
	seedPair("sibscope", scopeB, sourceS)

	executor := &boltTestExecutor{runner: runner}
	writer := NewIncidentRoutingEvidenceWriter(executor, 0)
	if err := writer.RetractIncidentRoutingEvidence(ctx, []string{scopeA}, "gen-2", sourceS); err != nil {
		t.Fatalf("RetractIncidentRoutingEvidence returned error: %v", err)
	}

	countTag := func(tag string) int64 {
		t.Helper()
		n, err := boltCount(ctx, runner,
			`MATCH (n:IncidentRoutingEvidence) WHERE n.uid STARTS WITH $prefix RETURN count(n) AS count`,
			map[string]any{"prefix": "6228-routing-" + tag})
		if err != nil {
			t.Fatalf("count pair %q: %v", tag, err)
		}
		return n
	}
	if n := countTag("own"); n != 0 {
		t.Errorf("owned pair count = %d, want 0", n)
	}
	if n := countTag("sibsrc"); n != 2 {
		t.Errorf("foreign-source sibling pair count = %d, want 2", n)
	}
	if n := countTag("sibscope"); n != 2 {
		t.Errorf("foreign-scope sibling pair count = %d, want 2", n)
	}
}
