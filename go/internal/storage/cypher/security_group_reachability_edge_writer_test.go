// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

const sgReachabilityEvidence = "reducer/security-group-reachability"

func sgRuleNodeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":         "rule-" + string(rune('a'+i)),
			"sg_uid":      "sg-" + string(rune('a'+i)),
			"direction":   "ingress",
			"ip_protocol": "tcp",
			"from_port":   "22",
			"to_port":     "22",
			"source_kind": "cidr_ipv4",
			"is_internet": false,
		})
	}
	return rows
}

func TestSecurityGroupRuleNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	if err := writer.WriteSecurityGroupRuleNodes(context.Background(), nil, sgReachabilityEvidence); err != nil {
		t.Fatalf("WriteSecurityGroupRuleNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestSecurityGroupRuleNodeWriterMergesOnUIDOnly(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	if err := writer.WriteSecurityGroupRuleNodes(context.Background(), sgRuleNodeRows(1), sgReachabilityEvidence); err != nil {
		t.Fatalf("WriteSecurityGroupRuleNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MERGE (r:SecurityGroupRule {uid: row.uid})") {
		t.Fatalf("rule node MERGE must key on uid only:\n%s", cypher)
	}
	if !strings.Contains(cypher, "r.from_port = row.from_port") || !strings.Contains(cypher, "r.ip_protocol = row.ip_protocol") {
		t.Fatalf("rule node must SET port/proto props for API readback:\n%s", cypher)
	}
}

func sgRuleEdgeRows() []map[string]any {
	return []map[string]any{
		{"sg_uid": "sg-a", "rule_uid": "rule-a", "relationship_type": "ALLOWS_INGRESS"},
		{"sg_uid": "sg-b", "rule_uid": "rule-b", "relationship_type": "ALLOWS_EGRESS"},
	}
}

func TestSecurityGroupSGRuleEdgeWriterUsesStaticDirectionType(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 500)
	if err := writer.WriteSecurityGroupSGRuleEdges(context.Background(), sgRuleEdgeRows(), "scope-1", "gen-1", sgReachabilityEvidence); err != nil {
		t.Fatalf("WriteSecurityGroupSGRuleEdges returned error: %v", err)
	}
	// Two relationship types -> two grouped statements.
	if len(executor.groupCalls) != 1 || len(executor.groupCalls[0]) != 2 {
		t.Fatalf("expected one atomic group of two type-split statements, got %v", executor.groupCalls)
	}
	joined := executor.groupCalls[0][0].Cypher + "\n" + executor.groupCalls[0][1].Cypher
	if !strings.Contains(joined, "MATCH (sg:CloudResource {uid: row.sg_uid})") {
		t.Fatalf("SG edge must MATCH the SecurityGroup CloudResource by uid:\n%s", joined)
	}
	if !strings.Contains(joined, "MATCH (rule:SecurityGroupRule {uid: row.rule_uid})") {
		t.Fatalf("SG edge must MATCH the SecurityGroupRule by uid:\n%s", joined)
	}
	if !strings.Contains(joined, "MERGE (sg)-[rel:ALLOWS_INGRESS]->(rule)") {
		t.Fatalf("ingress edge must use static ALLOWS_INGRESS type:\n%s", joined)
	}
	if !strings.Contains(joined, "MERGE (sg)-[rel:ALLOWS_EGRESS]->(rule)") {
		t.Fatalf("egress edge must use static ALLOWS_EGRESS type:\n%s", joined)
	}
	if strings.Contains(joined, "MERGE (sg:CloudResource") || strings.Contains(joined, "MERGE (rule:SecurityGroupRule") {
		t.Fatalf("SG edge must not fabricate endpoint nodes:\n%s", joined)
	}
}

func TestSecurityGroupSGRuleEdgeWriterRejectsUnknownRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 500)
	rows := []map[string]any{
		{"sg_uid": "sg-a", "rule_uid": "rule-a", "relationship_type": "ALLOWS_INGRESS; DROP"},
	}
	if err := writer.WriteSecurityGroupSGRuleEdges(context.Background(), rows, "scope-1", "gen-1", sgReachabilityEvidence); err == nil {
		t.Fatal("expected rejection of an out-of-vocabulary relationship type (injection guard)")
	}
}

func sgToEdgeRows() []map[string]any {
	return []map[string]any{
		{"rule_uid": "rule-a", "target_uid": "cidr-a", "target_label": "CidrBlock"},
		{"rule_uid": "rule-b", "target_uid": "sg-b", "target_label": "CloudResource"},
		{"rule_uid": "rule-c", "target_uid": "pl-c", "target_label": "PrefixList"},
	}
}

func TestSecurityGroupToEdgeWriterUsesValidatedTargetLabel(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 500)
	if err := writer.WriteSecurityGroupRuleEndpointEdges(context.Background(), sgToEdgeRows(), "scope-1", "gen-1", sgReachabilityEvidence); err != nil {
		t.Fatalf("WriteSecurityGroupRuleEndpointEdges returned error: %v", err)
	}
	// Three distinct target labels -> three label-split statements in one group.
	if len(executor.groupCalls) != 1 || len(executor.groupCalls[0]) != 3 {
		t.Fatalf("expected one atomic group of three label-split statements, got %v", executor.groupCalls)
	}
	var joined string
	for _, stmt := range executor.groupCalls[0] {
		joined += stmt.Cypher + "\n"
	}
	if !strings.Contains(joined, "MATCH (rule:SecurityGroupRule {uid: row.rule_uid})") {
		t.Fatalf("TO edge must MATCH the rule by uid:\n%s", joined)
	}
	if !strings.Contains(joined, "MATCH (target:CidrBlock {uid: row.target_uid})") ||
		!strings.Contains(joined, "MATCH (target:CloudResource {uid: row.target_uid})") ||
		!strings.Contains(joined, "MATCH (target:PrefixList {uid: row.target_uid})") {
		t.Fatalf("TO edge must MATCH each validated endpoint label:\n%s", joined)
	}
	if !strings.Contains(joined, "MERGE (rule)-[rel:TO]->(target)") {
		t.Fatalf("TO edge must use the static TO relationship type:\n%s", joined)
	}
	if strings.Contains(joined, "MERGE (target:") {
		t.Fatalf("TO edge must not fabricate endpoint nodes:\n%s", joined)
	}
}

func TestSecurityGroupToEdgeWriterRejectsUnknownTargetLabel(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 500)
	rows := []map[string]any{
		{"rule_uid": "rule-a", "target_uid": "x", "target_label": "EvilLabel"},
	}
	if err := writer.WriteSecurityGroupRuleEndpointEdges(context.Background(), rows, "scope-1", "gen-1", sgReachabilityEvidence); err == nil {
		t.Fatal("expected rejection of an out-of-vocabulary target label (injection guard)")
	}
}

func TestSecurityGroupReachabilityRetractScopesByEvidenceAndScope(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 500)
	if err := writer.RetractSecurityGroupReachability(context.Background(), []string{"scope-1"}, "gen-2", sgReachabilityEvidence); err != nil {
		t.Fatalf("RetractSecurityGroupReachability returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("expected one atomic retract group, got %v", executor.groupCalls)
	}
	var joined string
	for _, stmt := range executor.groupCalls[0] {
		joined += stmt.Cypher + "\n"
	}
	// Retract must scope by the edge's own scope_id + evidence_source (the
	// endpoint and SG nodes are cross-scope canonical and carry no scope_id).
	if !strings.Contains(joined, "rel.scope_id IN $scope_ids") || !strings.Contains(joined, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must filter on edge scope_id + evidence_source:\n%s", joined)
	}
	// Both edge families (SG->rule and rule->endpoint) must be retracted.
	if !strings.Contains(joined, "(:SecurityGroup") && !strings.Contains(joined, "(sg:CloudResource)-[rel]->(rule:SecurityGroupRule)") {
		t.Fatalf("retract must remove SG->rule edges:\n%s", joined)
	}
	if !strings.Contains(joined, "(rule:SecurityGroupRule)-[rel:TO]->") {
		t.Fatalf("retract must remove rule->endpoint TO edges:\n%s", joined)
	}
}

func TestSecurityGroupRuleNodeWriterSetsNameProperty(t *testing.T) {
	t.Parallel()

	// The networking inventory projection selects n.name for display. The writer
	// must SET r.name from row.name so SecurityGroupRule nodes carry a non-empty
	// human-readable name (regression for empty networking name on the Nodes page).
	executor := &recordingExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	rows := []map[string]any{{
		"uid":         "rule-a",
		"sg_uid":      "sg-a",
		"direction":   "ingress",
		"ip_protocol": "tcp",
		"from_port":   "443",
		"to_port":     "443",
		"source_kind": "cidr_ipv4",
		"is_internet": true,
		"name":        "ingress/tcp/443-443",
	}}
	if err := writer.WriteSecurityGroupRuleNodes(context.Background(), rows, sgReachabilityEvidence); err != nil {
		t.Fatalf("WriteSecurityGroupRuleNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "r.name = row.name") {
		t.Fatalf("rule node writer must SET r.name = row.name for display readback:\n%s", cypher)
	}
}

func TestSecurityGroupReachabilityRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 500)
	if err := writer.RetractSecurityGroupReachability(context.Background(), nil, "gen-2", sgReachabilityEvidence); err != nil {
		t.Fatalf("RetractSecurityGroupReachability returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("empty scope set must be a no-op, got %v", executor.groupCalls)
	}
}
