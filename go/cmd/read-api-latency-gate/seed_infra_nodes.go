// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "fmt"

var (
	seedProviders    = []string{"aws", "gcp", "azure"}
	seedEnvironments = []string{"production", "staging"}
)

// seedProvider and seedEnvironment give a seeded infra entity its low
// cardinality dimensions (3 providers, 2 environments), so the infra
// aggregate's by-provider/by-environment grouping has real buckets. Graph
// nodes and content_entities rows both use them, so the two stores agree.
func seedProvider(i int) string    { return seedProviders[i%len(seedProviders)] }
func seedEnvironment(i int) string { return seedEnvironments[i%len(seedEnvironments)] }

// seedInfraID is the identity shared by a seeded graph node and its
// content_entities row.
func seedInfraID(label string, i int) string {
	return fmt.Sprintf("%s-seed-%d", label, i)
}

// infraLabelNeedsIdentity reports whether a label's seeded nodes must carry a
// real node identity (uid, resource_type, source_fact_id). CloudResource does:
// eshu-api's startup owner-ledger backfill pages every CloudResource node and
// rejects one missing any of them, which fails API startup. The label also has a
// uid UNIQUE constraint in the graph schema, so its nodes are written in the
// small batches constrained labels need (see iacGraphSeedBatchSize).
func infraLabelNeedsIdentity(label string) bool {
	return label == "CloudResource"
}

// infraNodeRows builds the parameter rows for one bulk CREATE batch covering
// the inclusive index range r. Every value is computed here: NornicDB stores a
// Cypher expression inside a CREATE property map as literal text, so a CASE
// there yields a unique string per node instead of a real provider name.
func infraNodeRows(label string, r idRange) []map[string]any {
	rows := make([]map[string]any, 0, r.Last-r.First+1)
	for i := r.First; i <= r.Last; i++ {
		row := map[string]any{
			"id":            seedInfraID(label, i),
			"provider":      seedProvider(i),
			"environment":   seedEnvironment(i),
			"source_system": label,
		}
		if infraLabelNeedsIdentity(label) {
			row["uid"] = seedInfraID(label, i)
			row["resource_type"] = "aws_instance"
			row["source_fact_id"] = fmt.Sprintf("seed-fact-%d", i)
		}
		rows = append(rows, row)
	}
	return rows
}
