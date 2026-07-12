// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Security-group reachability edge retract coverage (C-14 #4367 retract-depth
// backfill): ALLOWS_INGRESS, ALLOWS_EGRESS, TO.
//
// Before this fix, SecurityGroupReachabilityWriter.RetractSecurityGroupReachability
// dispatched its two retract statements (the SG -> rule direction family and
// the rule -> endpoint TO family) through the shared dispatch() helper, which
// routes through ExecuteGroup (a managed Bolt transaction) whenever the
// executor implements GroupExecutor -- the executor shape cmd/reducer wires in
// production. On the pinned NornicDB v1.1.11, DELETE statements sharing one
// managed transaction under-apply. The writer already had a dispatchRetract
// helper (sequential Execute, never ExecuteGroup) for its ledger-anchored
// RetractSecurityGroupReachabilityByUIDs path (issue #4858/#4881); this fix
// routes the whole-scope RetractSecurityGroupReachability through the same
// helper instead of duplicating it.
//
// A second, independently probed shape lives in this same writer:
// retractSecurityGroupSGRuleEdgesCypher anchors its DELETE on an untyped
// relationship expansion, `MATCH (sg:CloudResource)-[rel]->(rule:
// SecurityGroupRule) ... DELETE rel`, rather than a typed/disjunction
// relationship pattern. This was probed directly against the pinned
// v1.1.11 container over the HTTP tx/commit auto-commit endpoint before this
// test was written: seeding one CloudResource-[:ALLOWS_INGRESS]->
// SecurityGroupRule edge and running the exact retract statement (with a
// scope_id/evidence_source WHERE clause) as a single auto-commit statement
// deleted it (count 1 -> 0), so the untyped-expansion shape itself is sound on
// this pinned version; only the dispatch-through-ExecuteGroup path was broken.
//
// The test drives the REAL production writer construction and methods
// (cypher.NewSecurityGroupReachabilityWriter.WriteSecurityGroupRuleNodes /
// WriteSecurityGroupSGRuleEdges / WriteSecurityGroupRuleEndpointEdges /
// RetractSecurityGroupReachability) against liveExecutor (a GroupExecutor,
// mirroring production's reducerNeo4jExecutor), with an out-of-scope survivor
// control security group and endpoint/rule node-survival assertions.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	sgReachEvidenceSource = "reducer/security-group-reachability"
	sgReachGenerationID   = "gen-1"

	sgReachSGIn     = "replay-cloud-edge:sgreach:sg-in"
	sgReachSGOut    = "replay-cloud-edge:sgreach:sg-out"
	sgReachScopeIn  = "replay-cloud-edge:sgreach:scope-in"
	sgReachScopeOut = "replay-cloud-edge:sgreach:scope-out"

	sgReachRuleIngressIn  = "replay-cloud-edge:sgreach:rule-ingress-in"
	sgReachRuleEgressIn   = "replay-cloud-edge:sgreach:rule-egress-in"
	sgReachRuleIngressOut = "replay-cloud-edge:sgreach:rule-ingress-out"
	sgReachRuleEgressOut  = "replay-cloud-edge:sgreach:rule-egress-out"

	sgReachCidrIn          = "replay-cloud-edge:sgreach:cidr-in"
	sgReachCidrOut         = "replay-cloud-edge:sgreach:cidr-out"
	sgReachDestResourceIn  = "replay-cloud-edge:sgreach:dest-in"
	sgReachDestResourceOut = "replay-cloud-edge:sgreach:dest-out"
)

// TestReducerSecurityGroupReachabilityEdgeRetractGraphTruth proves the
// ALLOWS_INGRESS, ALLOWS_EGRESS, and TO retract paths delete only the
// in-scope edges (never the SecurityGroupRule or endpoint nodes) on a real
// NornicDB.
func TestReducerSecurityGroupReachabilityEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the security-group reachability retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupSecurityGroupReachabilityScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupSecurityGroupReachabilityScope(cleanCtx, t, exec)
	})

	seedSecurityGroupReachabilityNodes(ctx, t, exec)

	writer := cypher.NewSecurityGroupReachabilityWriter(exec, 0)

	// Rule nodes are not scope-stamped; write every rule for both scopes in one
	// call, mirroring the production handler's write-nodes-before-edges order.
	if err := writer.WriteSecurityGroupRuleNodes(ctx, []map[string]any{
		{"uid": sgReachRuleIngressIn, "sg_uid": sgReachSGIn, "direction": "ingress", "ip_protocol": "tcp", "from_port": 443, "to_port": 443, "name": "in-ingress", "source_kind": "cidr_ipv4", "is_internet": true},
		{"uid": sgReachRuleEgressIn, "sg_uid": sgReachSGIn, "direction": "egress", "ip_protocol": "tcp", "from_port": 443, "to_port": 443, "name": "in-egress", "source_kind": "cloud_resource", "is_internet": false},
		{"uid": sgReachRuleIngressOut, "sg_uid": sgReachSGOut, "direction": "ingress", "ip_protocol": "tcp", "from_port": 443, "to_port": 443, "name": "out-ingress", "source_kind": "cidr_ipv4", "is_internet": true},
		{"uid": sgReachRuleEgressOut, "sg_uid": sgReachSGOut, "direction": "egress", "ip_protocol": "tcp", "from_port": 443, "to_port": 443, "name": "out-egress", "source_kind": "cloud_resource", "is_internet": false},
	}, sgReachEvidenceSource); err != nil {
		t.Fatalf("WriteSecurityGroupRuleNodes: %v", err)
	}

	writeSGRuleEdges := func(scopeID string, rows []map[string]any) {
		t.Helper()
		if err := writer.WriteSecurityGroupSGRuleEdges(ctx, rows, scopeID, sgReachGenerationID, sgReachEvidenceSource); err != nil {
			t.Fatalf("WriteSecurityGroupSGRuleEdges(%s): %v", scopeID, err)
		}
	}
	writeSGRuleEdges(sgReachScopeIn, []map[string]any{
		{"sg_uid": sgReachSGIn, "rule_uid": sgReachRuleIngressIn, "relationship_type": "ALLOWS_INGRESS"},
		{"sg_uid": sgReachSGIn, "rule_uid": sgReachRuleEgressIn, "relationship_type": "ALLOWS_EGRESS"},
	})
	writeSGRuleEdges(sgReachScopeOut, []map[string]any{
		{"sg_uid": sgReachSGOut, "rule_uid": sgReachRuleIngressOut, "relationship_type": "ALLOWS_INGRESS"},
		{"sg_uid": sgReachSGOut, "rule_uid": sgReachRuleEgressOut, "relationship_type": "ALLOWS_EGRESS"},
	})

	writeEndpointEdges := func(scopeID string, rows []map[string]any) {
		t.Helper()
		if err := writer.WriteSecurityGroupRuleEndpointEdges(ctx, rows, scopeID, sgReachGenerationID, sgReachEvidenceSource); err != nil {
			t.Fatalf("WriteSecurityGroupRuleEndpointEdges(%s): %v", scopeID, err)
		}
	}
	writeEndpointEdges(sgReachScopeIn, []map[string]any{
		{"rule_uid": sgReachRuleIngressIn, "target_uid": sgReachCidrIn, "target_label": "CidrBlock"},
		{"rule_uid": sgReachRuleEgressIn, "target_uid": sgReachDestResourceIn, "target_label": "CloudResource"},
	})
	writeEndpointEdges(sgReachScopeOut, []map[string]any{
		{"rule_uid": sgReachRuleIngressOut, "target_uid": sgReachCidrOut, "target_label": "CidrBlock"},
		{"rule_uid": sgReachRuleEgressOut, "target_uid": sgReachDestResourceOut, "target_label": "CloudResource"},
	})

	ingressQ := "MATCH (:CloudResource {uid: $sg})-[r:ALLOWS_INGRESS]->(:SecurityGroupRule {uid: $rule}) RETURN count(r)"
	egressQ := "MATCH (:CloudResource {uid: $sg})-[r:ALLOWS_EGRESS]->(:SecurityGroupRule {uid: $rule}) RETURN count(r)"
	toQ := "MATCH (:SecurityGroupRule {uid: $rule})-[r:TO]->({uid: $target}) RETURN count(r)"

	inIngress := map[string]any{"sg": sgReachSGIn, "rule": sgReachRuleIngressIn}
	outIngress := map[string]any{"sg": sgReachSGOut, "rule": sgReachRuleIngressOut}
	inEgress := map[string]any{"sg": sgReachSGIn, "rule": sgReachRuleEgressIn}
	outEgress := map[string]any{"sg": sgReachSGOut, "rule": sgReachRuleEgressOut}
	inToCidr := map[string]any{"rule": sgReachRuleIngressIn, "target": sgReachCidrIn}
	outToCidr := map[string]any{"rule": sgReachRuleIngressOut, "target": sgReachCidrOut}
	inToDest := map[string]any{"rule": sgReachRuleEgressIn, "target": sgReachDestResourceIn}
	outToDest := map[string]any{"rule": sgReachRuleEgressOut, "target": sgReachDestResourceOut}

	assertEdgeCount(ctx, t, exec, ingressQ, inIngress, 1, "write: in-scope ALLOWS_INGRESS present")
	assertEdgeCount(ctx, t, exec, ingressQ, outIngress, 1, "write: out-of-scope ALLOWS_INGRESS present")
	assertEdgeCount(ctx, t, exec, egressQ, inEgress, 1, "write: in-scope ALLOWS_EGRESS present")
	assertEdgeCount(ctx, t, exec, egressQ, outEgress, 1, "write: out-of-scope ALLOWS_EGRESS present")
	assertEdgeCount(ctx, t, exec, toQ, inToCidr, 1, "write: in-scope TO(CidrBlock) present")
	assertEdgeCount(ctx, t, exec, toQ, outToCidr, 1, "write: out-of-scope TO(CidrBlock) present")
	assertEdgeCount(ctx, t, exec, toQ, inToDest, 1, "write: in-scope TO(CloudResource) present")
	assertEdgeCount(ctx, t, exec, toQ, outToDest, 1, "write: out-of-scope TO(CloudResource) present")

	if err := writer.RetractSecurityGroupReachability(ctx, []string{sgReachScopeIn}, sgReachGenerationID, sgReachEvidenceSource); err != nil {
		t.Fatalf("RetractSecurityGroupReachability: %v", err)
	}

	assertEdgeCount(ctx, t, exec, ingressQ, inIngress, 0, "retract: in-scope ALLOWS_INGRESS gone")
	assertEdgeCount(ctx, t, exec, egressQ, inEgress, 0, "retract: in-scope ALLOWS_EGRESS gone")
	assertEdgeCount(ctx, t, exec, toQ, inToCidr, 0, "retract: in-scope TO(CidrBlock) gone")
	assertEdgeCount(ctx, t, exec, toQ, inToDest, 0, "retract: in-scope TO(CloudResource) gone")

	// Scoped retract, not a wipe: out-of-scope edges survive.
	assertEdgeCount(ctx, t, exec, ingressQ, outIngress, 1, "retract: out-of-scope ALLOWS_INGRESS survives")
	assertEdgeCount(ctx, t, exec, egressQ, outEgress, 1, "retract: out-of-scope ALLOWS_EGRESS survives")
	assertEdgeCount(ctx, t, exec, toQ, outToCidr, 1, "retract: out-of-scope TO(CidrBlock) survives")
	assertEdgeCount(ctx, t, exec, toQ, outToDest, 1, "retract: out-of-scope TO(CloudResource) survives")

	// Edge retract never deletes SG, rule, or endpoint nodes.
	for _, q := range []struct {
		cypherText string
		key        string
	}{
		{"MATCH (n:CloudResource {uid: $u}) RETURN count(n)", sgReachSGIn},
		{"MATCH (n:SecurityGroupRule {uid: $u}) RETURN count(n)", sgReachRuleIngressIn},
		{"MATCH (n:SecurityGroupRule {uid: $u}) RETURN count(n)", sgReachRuleEgressIn},
		{"MATCH (n:CidrBlock {uid: $u}) RETURN count(n)", sgReachCidrIn},
		{"MATCH (n:CloudResource {uid: $u}) RETURN count(n)", sgReachDestResourceIn},
	} {
		assertEdgeCount(ctx, t, exec, q.cypherText, map[string]any{"u": q.key}, 1, "node survives: "+q.key)
	}
}

// seedSecurityGroupReachabilityNodes creates the SG CloudResource anchors and
// the CidrBlock/CloudResource endpoint nodes the write templates MATCH. The
// SecurityGroupRule nodes are MERGEd by WriteSecurityGroupRuleNodes itself and
// are not seeded here.
func seedSecurityGroupReachabilityNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE
       (:CloudResource {uid: $sgIn, marker: $marker}),
       (:CloudResource {uid: $sgOut, marker: $marker}),
       (:CidrBlock {uid: $cidrIn, marker: $marker}),
       (:CidrBlock {uid: $cidrOut, marker: $marker}),
       (:CloudResource {uid: $destIn, marker: $marker}),
       (:CloudResource {uid: $destOut, marker: $marker})`,
		Parameters: map[string]any{
			"sgIn": sgReachSGIn, "sgOut": sgReachSGOut,
			"cidrIn": sgReachCidrIn, "cidrOut": sgReachCidrOut,
			"destIn": sgReachDestResourceIn, "destOut": sgReachDestResourceOut,
			"marker": sgReachEvidenceSource,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed security-group reachability nodes: %v", err)
	}
}

// cleanupSecurityGroupReachabilityScope removes every node this test creates,
// including the write-MERGEd SecurityGroupRule nodes (not marker-tagged, since
// the rule-node upsert template only SETs its own fixed property set).
func cleanupSecurityGroupReachabilityScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": sgReachEvidenceSource},
		},
		{
			Cypher: `MATCH (r:SecurityGroupRule) WHERE r.uid IN $uids DETACH DELETE r`,
			Parameters: map[string]any{"uids": []string{
				sgReachRuleIngressIn, sgReachRuleEgressIn, sgReachRuleIngressOut, sgReachRuleEgressOut,
			}},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup security-group reachability scope: %v", err)
		}
	}
}
