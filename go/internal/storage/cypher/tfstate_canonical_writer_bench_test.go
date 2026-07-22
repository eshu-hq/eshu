// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// tfstateSyntheticResourceRows builds n synthetic TerraformStateResourceRow
// values, each carrying a provider binding (#5446) and a promotable
// tf_attr_instance_type attribute (#5441), the two node-property sources
// buildTerraformStateStatements' resource phase must thread through on every
// row.
func tfstateSyntheticResourceRows(n int) []projector.TerraformStateResourceRow {
	rows := make([]projector.TerraformStateResourceRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, projector.TerraformStateResourceRow{
			UID:                   fmt.Sprintf("tf-resource-uid-%d", i),
			Address:               fmt.Sprintf("aws_instance.web_%d", i),
			Mode:                  "managed",
			ResourceType:          "aws_instance",
			Name:                  fmt.Sprintf("web_%d", i),
			ProviderAddress:       "provider[\"registry.terraform.io/hashicorp/aws\"]",
			Lineage:               "lineage-bench",
			Serial:                1,
			BackendKind:           "s3",
			LocatorHash:           "locator-hash-bench",
			SourceConfidence:      facts.SourceConfidenceObserved,
			CollectorKind:         "terraform_state",
			Provider:              "aws",
			ProviderSourceAddress: "registry.terraform.io/hashicorp/aws",
			Attributes: map[string]any{
				"instance_type": "t3.micro",
				"ami":           "ami-0abcdef1234567890",
			},
		})
	}
	return rows
}

// BenchmarkBuildTerraformStateStatementsSyntheticCorpus measures
// buildTerraformStateStatements' statement-count and wall-clock cost on a
// synthetic 10k-resource materialization (#5446 Prove-The-Theory-First
// proof for the two additive, non-hot-path-shape changes in this PR: three
// more fixed r.provider*/SET keys folded into the existing per-batch
// UNWIND/MERGE/SET resource-upsert template, and the extended tf_attr_*
// allowlist consumed through the unchanged promoteTerraformResourceAttributes
// call already on this path). Neither change adds a new statement, a new
// MERGE, or a new traversal -- both are additional map/string keys evaluated
// once per row inside the SAME per-batch loop this function already ran
// before #5446 -- so the theory under test is "no new O(n) or O(n^2) cost
// shape," not a specific latency target. b.ReportMetric surfaces the derived
// statements-per-resource ratio so a future regression that changes the
// batching shape (not just adds properties) is visible in the benchmark
// output.
func BenchmarkBuildTerraformStateStatementsSyntheticCorpus(b *testing.B) {
	const resourceCount = 10_000
	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:                 "tf-scope-bench",
		GenerationID:            "tf-generation-bench",
		TerraformStateResources: tfstateSyntheticResourceRows(resourceCount),
	}

	b.ReportAllocs()
	b.ResetTimer()
	var lastCount int
	for i := 0; i < b.N; i++ {
		statements := writer.buildTerraformStateStatements(mat)
		lastCount = len(statements)
	}
	b.StopTimer()
	b.ReportMetric(float64(lastCount), "statements/op")
	b.ReportMetric(float64(lastCount)/float64(resourceCount), "statements_per_resource")
}
