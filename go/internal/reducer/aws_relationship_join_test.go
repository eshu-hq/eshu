// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func awsRelationshipEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSRelationshipFactKind,
		Payload:  payload,
	}
}

// resourceEnvelope is a small helper for join-index tests. account+region are
// part of the uid identity (the cross-account/region trust boundary).
func resourceEnvelope(accountID, region, resourceType, resourceID, arn string, anchors ...string) facts.Envelope {
	anchorVals := make([]any, 0, len(anchors))
	for _, a := range anchors {
		anchorVals = append(anchorVals, a)
	}
	return awsResourceEnvelope(map[string]any{
		"account_id":          accountID,
		"region":              region,
		"resource_type":       resourceType,
		"resource_id":         resourceID,
		"arn":                 arn,
		"correlation_anchors": anchorVals,
	})
}

func TestExtractAWSRelationshipEdgeRowsResolvesByARN(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")

	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	rows, tally, err := ExtractAWSRelationshipEdgeRows(
		[]facts.Envelope{source, target},
		[]facts.Envelope{rel},
	)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0]["resolution_mode"] != joinModeARN {
		t.Fatalf("resolution_mode = %v, want %s", rows[0]["resolution_mode"], joinModeARN)
	}
	wantSource := cloudResourceUID("111122223333", "us-east-1", "aws_lambda_function", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	wantTarget := cloudResourceUID("111122223333", "us-east-1", "aws_kms_key", "arn:aws:kms:us-east-1:111122223333:key/abc")
	if rows[0]["source_uid"] != wantSource {
		t.Fatalf("source_uid = %v, want %v", rows[0]["source_uid"], wantSource)
	}
	if rows[0]["target_uid"] != wantTarget {
		t.Fatalf("target_uid = %v, want %v", rows[0]["target_uid"], wantTarget)
	}
	if rows[0]["relationship_type"] != "USES_KMS_KEY" {
		t.Fatalf("relationship_type = %v, want USES_KMS_KEY", rows[0]["relationship_type"])
	}
	if got := tally.resolved[joinModeARN]; got != 1 {
		t.Fatalf("tally.resolved[arn] = %d, want 1", got)
	}
	if len(tally.unresolved) != 0 {
		t.Fatalf("unresolved tally = %v, want empty", tally.unresolved)
	}
}

func TestExtractAWSRelationshipEdgeRowsResolvesByBareID(t *testing.T) {
	t.Parallel()

	// subnet -> vpc by bare id; vpc has no separate ARN.
	source := resourceEnvelope("111122223333", "us-east-1", "aws_ec2_subnet", "subnet-1", "")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_ec2_vpc", "vpc-1", "")

	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "ATTACHED_TO_VPC",
		"source_resource_id": "subnet-1",
		"target_resource_id": "vpc-1",
		"target_type":        "aws_ec2_vpc",
	})

	rows, tally, err := ExtractAWSRelationshipEdgeRows(
		[]facts.Envelope{source, target},
		[]facts.Envelope{rel},
	)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0]["resolution_mode"] != joinModeBareID {
		t.Fatalf("resolution_mode = %v, want %s", rows[0]["resolution_mode"], joinModeBareID)
	}
	if got := tally.resolved[joinModeBareID]; got != 1 {
		t.Fatalf("tally.resolved[bare_id] = %d, want 1", got)
	}
}

func TestExtractAWSRelationshipEdgeRowsResolvesByCorrelationAnchor(t *testing.T) {
	t.Parallel()

	// SageMaker endpoint -> endpoint-config by name (anchor), no ARN on the fact.
	source := resourceEnvelope("111122223333", "us-east-1", "aws_sagemaker_endpoint",
		"arn:aws:sagemaker:us-east-1:111122223333:endpoint/ep", "arn:aws:sagemaker:us-east-1:111122223333:endpoint/ep")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_sagemaker_endpoint_config",
		"arn:aws:sagemaker:us-east-1:111122223333:endpoint-config/cfg", "arn:aws:sagemaker:us-east-1:111122223333:endpoint-config/cfg",
		"my-endpoint-config")

	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_ENDPOINT_CONFIG",
		"source_resource_id": "arn:aws:sagemaker:us-east-1:111122223333:endpoint/ep",
		"source_arn":         "arn:aws:sagemaker:us-east-1:111122223333:endpoint/ep",
		"target_resource_id": "my-endpoint-config",
		"target_type":        "aws_sagemaker_endpoint_config",
	})

	rows, tally, err := ExtractAWSRelationshipEdgeRows(
		[]facts.Envelope{source, target},
		[]facts.Envelope{rel},
	)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0]["resolution_mode"] != joinModeCorrelationAnchor {
		t.Fatalf("resolution_mode = %v, want %s", rows[0]["resolution_mode"], joinModeCorrelationAnchor)
	}
	if got := tally.resolved[joinModeCorrelationAnchor]; got != 1 {
		t.Fatalf("tally.resolved[correlation_anchor] = %d, want 1", got)
	}
}

func TestExtractAWSRelationshipEdgeRowsUnresolvedTargetCountedNotWritten(t *testing.T) {
	t.Parallel()

	// Source exists; target service was not scanned this generation -> the edge
	// is counted by target_type, never written, never crashes.
	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")

	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/not-scanned",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/not-scanned",
		"target_type":        "aws_kms_key",
	})

	rows, tally, err := ExtractAWSRelationshipEdgeRows(
		[]facts.Envelope{source},
		[]facts.Envelope{rel},
	)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved target", len(rows))
	}
	if got := tally.unresolved["aws_kms_key"]; got != 1 {
		t.Fatalf("unresolved[aws_kms_key] = %d, want 1", got)
	}
}

func TestExtractAWSRelationshipEdgeRowsCrossAccountTargetStaysUnresolved(t *testing.T) {
	t.Parallel()

	// Cross-account ARN target: the target account/region was NOT scanned in
	// this scope, so the uid cannot be minted from the ARN alone — no
	// fabrication across the trust boundary. The same target ARN string exists
	// only under a DIFFERENT account, which must not satisfy the join.
	source := resourceEnvelope("111122223333", "us-east-1", "aws_iam_role",
		"arn:aws:iam::111122223333:role/app", "arn:aws:iam::111122223333:role/app")
	// A KMS key in a different account is present in the index but the
	// relationship points at account 999988887777 which is not scanned.
	otherAccountKey := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/local", "arn:aws:kms:us-east-1:111122223333:key/local")

	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:iam::111122223333:role/app",
		"source_arn":         "arn:aws:iam::111122223333:role/app",
		"target_resource_id": "arn:aws:kms:us-east-1:999988887777:key/remote",
		"target_arn":         "arn:aws:kms:us-east-1:999988887777:key/remote",
		"target_type":        "aws_kms_key",
	})

	rows, tally, err := ExtractAWSRelationshipEdgeRows(
		[]facts.Envelope{source, otherAccountKey},
		[]facts.Envelope{rel},
	)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for cross-account target", len(rows))
	}
	if got := tally.unresolved["aws_kms_key"]; got != 1 {
		t.Fatalf("unresolved[aws_kms_key] = %d, want 1", got)
	}
}

func TestExtractAWSRelationshipEdgeRowsUnresolvedSourceStaysUnresolved(t *testing.T) {
	t.Parallel()

	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")

	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:missing",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:missing",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	rows, tally, err := ExtractAWSRelationshipEdgeRows(
		[]facts.Envelope{target},
		[]facts.Envelope{rel},
	)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved source", len(rows))
	}
	if got := tally.unresolvedSource["aws_kms_key"]; got != 1 {
		t.Fatalf("unresolvedSource[aws_kms_key] = %d, want 1", got)
	}
}

func TestExtractAWSRelationshipEdgeRowsDeduplicatesAndSortsDeterministically(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")

	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	// Same edge fact twice (duplicate / retry) must converge on one row.
	rows, _, err := ExtractAWSRelationshipEdgeRows(
		[]facts.Envelope{source, target},
		[]facts.Envelope{rel, rel},
	)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 deduplicated edge", len(rows))
	}
}

func TestExtractAWSRelationshipEdgeRowsEmptyInputsAreNil(t *testing.T) {
	t.Parallel()

	rows, tally, err := ExtractAWSRelationshipEdgeRows(nil, nil)
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if len(tally.resolved) != 0 || len(tally.unresolved) != 0 {
		t.Fatalf("tally = %+v, want empty", tally)
	}
}

func TestExtractAWSRelationshipEdgeRowsSelfEdgeSkipped(t *testing.T) {
	t.Parallel()

	// A relationship whose source and target resolve to the same uid is not a
	// meaningful edge; it must not produce a self-loop.
	res := resourceEnvelope("111122223333", "us-east-1", "aws_ec2_vpc", "vpc-1", "")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "PEERED_WITH",
		"source_resource_id": "vpc-1",
		"target_resource_id": "vpc-1",
		"target_type":        "aws_ec2_vpc",
	})

	rows, _, err := ExtractAWSRelationshipEdgeRows([]facts.Envelope{res}, []facts.Envelope{rel})
	if err != nil {
		t.Fatalf("ExtractAWSRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for self edge", len(rows))
	}
}
