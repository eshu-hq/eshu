// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchSGRuleNodeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":         fmt.Sprintf("rule-%d", i),
			"sg_uid":      fmt.Sprintf("sg-%d", i%64),
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

func benchSGRuleEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		relType := "ALLOWS_INGRESS"
		if i%2 == 0 {
			relType = "ALLOWS_EGRESS"
		}
		rows = append(rows, map[string]any{
			"sg_uid":            fmt.Sprintf("sg-%d", i%64),
			"rule_uid":          fmt.Sprintf("rule-%d", i),
			"relationship_type": relType,
		})
	}
	return rows
}

func benchSGToEdgeRows(n int) []map[string]any {
	labels := []string{"CidrBlock", "CloudResource", "PrefixList"}
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"rule_uid":     fmt.Sprintf("rule-%d", i),
			"target_uid":   fmt.Sprintf("ep-%d", i),
			"target_label": labels[i%len(labels)],
		})
	}
	return rows
}

// BenchmarkSecurityGroupReachabilityWriter measures the statement-construction
// and batching cost of the reachability writer for a realistic
// per-scope-generation rule count, across all three write surfaces (rule nodes,
// SG->rule direction edges grouped by relationship type, and rule->endpoint TO
// edges grouped by target label). The backend executor is a no-op so the
// benchmark isolates Eshu-owned write-path work (batched MERGE row shaping and
// token-grouping) from graph round trips, proving the write side has no N+1 and
// stays in the same shape class as the proven COVERS / RUNS_IMAGE edge writers.
func BenchmarkSecurityGroupReachabilityWriter(b *testing.B) {
	ruleNodes := benchSGRuleNodeRows(5000)
	sgEdges := benchSGRuleEdgeRows(5000)
	toEdges := benchSGToEdgeRows(5000)
	writer := NewSecurityGroupReachabilityWriter(noopGroupExecutor{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteSecurityGroupRuleNodes(ctx, ruleNodes, "reducer/security-group-reachability"); err != nil {
			b.Fatalf("WriteSecurityGroupRuleNodes: %v", err)
		}
		if err := writer.WriteSecurityGroupSGRuleEdges(ctx, sgEdges, "scope-1", "gen-1", "reducer/security-group-reachability"); err != nil {
			b.Fatalf("WriteSecurityGroupSGRuleEdges: %v", err)
		}
		if err := writer.WriteSecurityGroupRuleEndpointEdges(ctx, toEdges, "scope-1", "gen-1", "reducer/security-group-reachability"); err != nil {
			b.Fatalf("WriteSecurityGroupRuleEndpointEdges: %v", err)
		}
	}
}
