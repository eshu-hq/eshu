// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// awsRelationshipBenchCorpus builds a realistic single-region scan corpus:
// resourceCount CloudResource-eligible facts and one relationship fact per
// resource (each function -> its own KMS key by ARN). It returns the resource
// and relationship envelopes separately so the benchmark mirrors the handler's
// split load.
func awsRelationshipBenchCorpus(resourceCount int) (resources, relationships []facts.Envelope) {
	resources = make([]facts.Envelope, 0, resourceCount*2)
	relationships = make([]facts.Envelope, 0, resourceCount)
	for i := 0; i < resourceCount; i++ {
		fnARN := fmt.Sprintf("arn:aws:lambda:us-east-1:111122223333:function:fn-%d", i)
		keyARN := fmt.Sprintf("arn:aws:kms:us-east-1:111122223333:key/key-%d", i)
		resources = append(
			resources,
			facts.Envelope{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
				"account_id": "111122223333", "region": "us-east-1",
				"resource_type": "aws_lambda_function", "resource_id": fnARN, "arn": fnARN,
			}},
			facts.Envelope{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
				"account_id": "111122223333", "region": "us-east-1",
				"resource_type": "aws_kms_key", "resource_id": keyARN, "arn": keyARN,
			}},
		)
		relationships = append(
			relationships,
			facts.Envelope{FactKind: facts.AWSRelationshipFactKind, Payload: map[string]any{
				"account_id": "111122223333", "region": "us-east-1",
				"relationship_type":  "USES_KMS_KEY",
				"source_resource_id": fnARN, "source_arn": fnARN,
				"target_resource_id": keyARN, "target_arn": keyARN,
				"target_type": "aws_kms_key",
			}},
		)
	}
	return resources, relationships
}

// BenchmarkExtractAWSRelationshipEdgeRows measures the bounded join: an O(R)
// index build plus O(E) in-memory map resolution producing the resolved edge
// rows. There is no per-edge graph round trip and no N+1 Cypher — the cost must
// scale linearly with the corpus, proving the design's performance contract
// (§8) on the Eshu-owned resolution path.
func BenchmarkExtractAWSRelationshipEdgeRows(b *testing.B) {
	const resourceCount = 5000
	resources, relationships := awsRelationshipBenchCorpus(resourceCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _, _, err := ExtractAWSRelationshipEdgeRows(resources, relationships)
		if err != nil {
			b.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
		}
		if len(rows) != resourceCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), resourceCount)
		}
	}
}

// BenchmarkBuildCloudResourceJoinIndex isolates the index build so a regression
// in the O(R) index construction is visible separately from edge resolution.
func BenchmarkBuildCloudResourceJoinIndex(b *testing.B) {
	const resourceCount = 5000
	resources, _ := awsRelationshipBenchCorpus(resourceCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index, _, err := buildCloudResourceJoinIndex(resources)
		if err != nil {
			b.Fatalf("buildCloudResourceJoinIndex() error = %v, want nil", err)
		}
		if len(index.byARN) != resourceCount*2 {
			b.Fatalf("len(byARN) = %d, want %d", len(index.byARN), resourceCount*2)
		}
	}
}
