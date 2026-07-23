// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// awsCloudImageBenchCorpus mirrors awsRelationshipBenchCorpus's shape: one
// Lambda function CloudResource plus one lambda_function_uses_image
// relationship with a resolved digest per iteration, so the join and
// extraction cost scale identically to the already-measured AWS relationship
// materialization corpus (issue #5450 prove-theory-first, CLAUDE.md).
func awsCloudImageBenchCorpus(count int) (resources, relationships []facts.Envelope) {
	resources = make([]facts.Envelope, 0, count)
	relationships = make([]facts.Envelope, 0, count)
	for i := 0; i < count; i++ {
		fnARN := fmt.Sprintf("arn:aws:lambda:us-east-1:111122223333:function:fn-%d", i)
		imageRef := fmt.Sprintf(
			"111122223333.dkr.ecr.us-east-1.amazonaws.com/fn-%d@sha256:%064d", i, i,
		)
		resources = append(resources, facts.Envelope{FactKind: facts.AWSResourceFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_lambda_function", "resource_id": fnARN, "arn": fnARN,
		}})
		relationships = append(relationships, facts.Envelope{FactKind: facts.AWSRelationshipFactKind, Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"relationship_type":  lambdaFunctionUsesImageRelationshipType,
			"source_resource_id": fnARN, "source_arn": fnARN,
			"target_resource_id": imageRef, "target_type": "container_image",
			"attributes": map[string]any{
				"package_type":       "Image",
				"resolved_image_uri": imageRef,
			},
		}})
	}
	return resources, relationships
}

// BenchmarkExtractAWSCloudImageEdgeRows measures the Eshu-owned join +
// extraction cost for the new CloudResource -> ContainerImage edge path (issue
// #5450 prove-theory-first): the O(R) source join-index build plus O(E)
// per-relationship digest-ref parsing, with no backend round trip. This is a
// NEW write path (no prior baseline to compare against), so the theory proof
// is the same-shape no-regression class the AWS resource node writer used
// (design doc §9a): compare this measured cost against the ALREADY-MEASURED,
// architecturally identical ExtractAWSRelationshipEdgeRows number at the same
// corpus size to show the new path adds no material extraction-side cost.
func BenchmarkExtractAWSCloudImageEdgeRows(b *testing.B) {
	const resourceCount = 5000
	resources, relationships := awsCloudImageBenchCorpus(resourceCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _, _, err := ExtractAWSCloudImageEdgeRows(resources, relationships)
		if err != nil {
			b.Fatalf("ExtractAWSCloudImageEdgeRows() error = %v, want nil", err)
		}
		if len(rows) != resourceCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), resourceCount)
		}
	}
}
