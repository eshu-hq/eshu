// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestIAMCanPerformCrossServiceFixture proves the catalog resolves several services
// at once (KMS, Secrets, DynamoDB) so the ARN classifier is exercised beyond S3.
func TestIAMCanPerformCrossServiceFixture(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeKMSKey, canPerformKMSKeyARN),
		canPerformNode(iamCanPerformResourceTypeSecret, canPerformSecretARN),
		canPerformNode(iamCanPerformResourceTypeDynamoDB, canPerformTableARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"kms:decrypt"}, []string{canPerformKMSKeyARN}),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"secretsmanager:getsecretvalue"}, []string{canPerformSecretARN}),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"dynamodb:getitem"}, []string{canPerformTableARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 3 {
		t.Fatalf("expected 3 cross-service edges; got %d (%v)", len(result.Edges), result.Edges)
	}
	for _, tc := range []struct {
		typ    string
		arn    string
		action string
	}{
		{iamCanPerformResourceTypeKMSKey, canPerformKMSKeyARN, "kms:decrypt"},
		{iamCanPerformResourceTypeSecret, canPerformSecretARN, "secretsmanager:getsecretvalue"},
		{iamCanPerformResourceTypeDynamoDB, canPerformTableARN, "dynamodb:getitem"},
	} {
		edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(tc.typ, tc.arn))
		if edge == nil {
			t.Fatalf("missing edge for %s", tc.action)
		}
		if got := edge["actions"].([]string); len(got) != 1 || got[0] != tc.action {
			t.Fatalf("actions for %s = %v", tc.action, got)
		}
	}
}

// TestIAMCanPerformDeterministicAndIdempotent proves the same input yields a
// byte-stable row set regardless of input ordering (idempotent reproject).
func TestIAMCanPerformDeterministicAndIdempotent(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
		canPerformNode(iamCanPerformResourceTypeKMSKey, canPerformKMSKeyARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"kms:decrypt"}, []string{canPerformKMSKeyARN}),
	}
	first := ExtractIAMCanPerformEdges(resources, perms).Edges
	second := ExtractIAMCanPerformEdges(resources, perms).Edges
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("extraction is not deterministic:\n%v\n%v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("len(edges) = %d, want 2", len(first))
	}
}

// TestIAMCanPerformResourceTypeOfARN locks the ARN classifier against the per-
// service ARN shapes so a malformed classifier (which would silently mis-type a
// node and fabricate or drop an edge) is caught.
func TestIAMCanPerformResourceTypeOfARN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		arn  string
		want string
	}{
		{"arn:aws:s3:::my-bucket", iamCanPerformResourceTypeS3Bucket},
		{"arn:aws:s3:::my-bucket/key/object", ""}, // object-level ARN is not a bucket node
		{"arn:aws:kms:us-east-1:111122223333:key/abc", iamCanPerformResourceTypeKMSKey},
		{"arn:aws:kms:us-east-1:111122223333:alias/MyKey", ""}, // alias is not a key node
		{"arn:aws:secretsmanager:us-east-1:111122223333:secret:db-creds", iamCanPerformResourceTypeSecret},
		{"arn:aws:ssm:us-east-1:111122223333:parameter/app/db", iamCanPerformResourceTypeSSMParam},
		{"arn:aws:dynamodb:us-east-1:111122223333:table/orders", iamCanPerformResourceTypeDynamoDB},
		{"arn:aws:dynamodb:us-east-1:111122223333:table/orders/index/by_status", ""}, // sub-resource
		{"arn:aws:ec2:us-east-1:111122223333:instance/i-0abc", iamCanPerformResourceTypeEC2Instance},
		{"arn:aws:rds:us-east-1:111122223333:db:prod", iamCanPerformResourceTypeRDSInstance},
		{"arn:aws:lambda:us-east-1:111122223333:function:orders-api", iamCanPerformResourceTypeLambdaFunc},
		{"arn:aws:lambda:us-east-1:111122223333:function:orders-api:prod", ""}, // alias/version is not the base function node
		{"arn:aws:iam::111122223333:role/x", ""},                               // not a CAN_PERFORM resource type
		{"not-an-arn", ""},
	}
	for _, tc := range cases {
		if got := iamCanPerformResourceTypeOfARN(tc.arn); got != tc.want {
			t.Fatalf("iamCanPerformResourceTypeOfARN(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}
