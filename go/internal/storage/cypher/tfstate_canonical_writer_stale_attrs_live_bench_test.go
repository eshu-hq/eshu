// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

// terraformResourceBenchBatchSize matches DefaultBatchSize (500) so these
// benchmarks measure the same batch shape production traffic uses.
const terraformResourceBenchBatchSize = 500

func terraformResourceBenchRows(n int, includeInstanceType bool) []interface{} {
	rows := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		attrs := map[string]interface{}{
			"tf_attr_ami": "ami-0abcdef1234567890",
		}
		if includeInstanceType {
			attrs["tf_attr_instance_type"] = "t3.micro"
		}
		rows = append(rows, map[string]interface{}{
			"uid":     fmt.Sprintf("bench-tf-resource-%d", i),
			"address": fmt.Sprintf("aws_instance.web_%d", i),
			"attrs":   attrs,
		})
	}
	return rows
}

func terraformResourceBenchUIDs(n int) []string {
	uids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		uids = append(uids, fmt.Sprintf("bench-tf-resource-%d", i))
	}
	return uids
}

func terraformResourceBenchCleanup(tb testing.TB, runner *boltRetractTestRunner, n int) {
	tb.Helper()
	if err := boltWriteStatement(
		context.Background(),
		runner,
		`MATCH (r:TerraformResource) WHERE r.uid STARTS WITH "bench-tf-resource-" DETACH DELETE r`,
		nil,
	); err != nil {
		tb.Errorf("cleanup live terraform resource bench nodes: %v", err)
	}
}

// BenchmarkTerraformResourceUpsertOnlyLive measures the upsert statement
// alone (canonicalTerraformStateResourceUpsertCypher, unchanged shape from
// before #5441 review round 8) against a real Bolt-connected backend, for a
// full DefaultBatchSize (500-row) batch. Opt-in via ESHU_CYPHER_BOLT_DSN,
// matching this package's other live benchmarks/tests.
//
// This is the committed, reproducible counterpart to the throwaway-harness
// numbers in docs/internal/evidence/5441-edge-node-properties.md (#5441
// review round 9 P2: "the cited benchmarks are not committed" -- fixed
// here). Unlike the throwaway harness, this measures real network round
// trips, not only in-process Go/data-structure cost.
func BenchmarkTerraformResourceUpsertOnlyLive(b *testing.B) {
	runner := openBoltTestRunner(b)
	b.Cleanup(func() { runner.close(context.Background()) })
	b.Cleanup(func() { terraformResourceBenchCleanup(b, runner, terraformResourceBenchBatchSize) })

	rows := terraformResourceBenchRows(terraformResourceBenchBatchSize, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := runner.runCypherSingle(ctx, Statement{
			Cypher:     canonicalTerraformStateResourceUpsertCypher,
			Parameters: map[string]any{"rows": rows},
		}); err != nil {
			b.Fatalf("upsert statement failed: %v", err)
		}
	}
}

// BenchmarkTerraformResourceRemoveOnlyLive isolates the added standalone
// REMOVE statement's own cost (#5441 review round 9's fix, replacing the
// non-functional fused REMOVE+SET shape) for a full 500-uid batch.
func BenchmarkTerraformResourceRemoveOnlyLive(b *testing.B) {
	runner := openBoltTestRunner(b)
	b.Cleanup(func() { runner.close(context.Background()) })
	b.Cleanup(func() { terraformResourceBenchCleanup(b, runner, terraformResourceBenchBatchSize) })

	rows := terraformResourceBenchRows(terraformResourceBenchBatchSize, true)
	ctx := context.Background()
	if err := runner.runCypherSingle(ctx, Statement{
		Cypher:     canonicalTerraformStateResourceUpsertCypher,
		Parameters: map[string]any{"rows": rows},
	}); err != nil {
		b.Fatalf("seed upsert failed: %v", err)
	}

	uids := terraformResourceBenchUIDs(terraformResourceBenchBatchSize)
	removeCypher := terraformStateResourceAttributeRemoveCypherByType["aws_instance"]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := runner.runCypherSingle(ctx, Statement{
			Cypher:     removeCypher,
			Parameters: map[string]any{"uids": uids},
		}); err != nil {
			b.Fatalf("remove statement failed: %v", err)
		}
	}
}

// BenchmarkTerraformResourceRemoveThenUpsertLive measures the shipped
// two-statement sequence in production order: the standalone REMOVE
// statement, then the upsert -- for a full 500-row/uid batch. This is the
// number that answers "what does the #5441 review round 9 fix cost in
// production," including one real Bolt round trip per statement (not
// captured by the in-process throwaway harness this benchmark replaces as
// the citable source).
func BenchmarkTerraformResourceRemoveThenUpsertLive(b *testing.B) {
	runner := openBoltTestRunner(b)
	b.Cleanup(func() { runner.close(context.Background()) })
	b.Cleanup(func() { terraformResourceBenchCleanup(b, runner, terraformResourceBenchBatchSize) })

	rows := terraformResourceBenchRows(terraformResourceBenchBatchSize, false)
	uids := terraformResourceBenchUIDs(terraformResourceBenchBatchSize)
	removeCypher := terraformStateResourceAttributeRemoveCypherByType["aws_instance"]
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := runner.runCypherSingle(ctx, Statement{
			Cypher:     removeCypher,
			Parameters: map[string]any{"uids": uids},
		}); err != nil {
			b.Fatalf("remove statement failed: %v", err)
		}
		if err := runner.runCypherSingle(ctx, Statement{
			Cypher:     canonicalTerraformStateResourceUpsertCypher,
			Parameters: map[string]any{"rows": rows},
		}); err != nil {
			b.Fatalf("upsert statement failed: %v", err)
		}
	}
}
