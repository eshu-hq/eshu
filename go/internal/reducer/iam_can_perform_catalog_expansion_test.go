// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	canPerformLambdaFunctionARN = "arn:aws:lambda:us-east-1:111122223333:function:orders-api"
	canPerformEC2InstanceARN    = "arn:aws:ec2:us-east-1:111122223333:instance/i-0abc123"
	canPerformRDSInstanceARN    = "arn:aws:rds:us-east-1:111122223333:db:orders"
)

// TestIAMCanPerformCatalogShipsPR4eVocabulary pins the reviewed PR4e expansion
// set. Each action has an already-scanned CloudResource target family and keeps
// the exact-ARN / single-glob resolution model.
func TestIAMCanPerformCatalogShipsPR4eVocabulary(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"s3:listbucket":                 iamCanPerformResourceTypeS3Bucket,
		"kms:generatedatakey":           iamCanPerformResourceTypeKMSKey,
		"secretsmanager:putsecretvalue": iamCanPerformResourceTypeSecret,
		"ssm:getparameters":             iamCanPerformResourceTypeSSMParam,
		"dynamodb:query":                iamCanPerformResourceTypeDynamoDB,
		"dynamodb:scan":                 iamCanPerformResourceTypeDynamoDB,
		"dynamodb:putitem":              iamCanPerformResourceTypeDynamoDB,
		"dynamodb:updateitem":           iamCanPerformResourceTypeDynamoDB,
		"dynamodb:deleteitem":           iamCanPerformResourceTypeDynamoDB,
		"ec2:stopinstances":             iamCanPerformResourceTypeEC2Instance,
		"rds:stopdbinstance":            iamCanPerformResourceTypeRDSInstance,
		"lambda:invokefunction":         iamCanPerformResourceTypeLambdaFunc,
	}
	byAction := iamCanPerformCatalogByAction()
	for action, typ := range want {
		entry, ok := byAction[action]
		if !ok {
			t.Fatalf("PR4e action %q missing from catalog", action)
		}
		if entry.ExpectedResourceType != typ {
			t.Fatalf("PR4e action %q expected type = %q, want %q", action, entry.ExpectedResourceType, typ)
		}
	}
}

// TestIAMCanPerformPR4eActionsResolveExpectedResourceTypes proves every PR4e
// action resolves only when its resource ARN points at the reviewed CloudResource
// family. The writer row shape is unchanged: actions still merge as edge
// properties on the static CAN_PERFORM edge.
func TestIAMCanPerformPR4eActionsResolveExpectedResourceTypes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		action       string
		resourceType string
		resourceARN  string
	}{
		{"s3 list bucket", "s3:listbucket", iamCanPerformResourceTypeS3Bucket, canPerformBucketARN},
		{"kms generate data key", "kms:generatedatakey", iamCanPerformResourceTypeKMSKey, canPerformKMSKeyARN},
		{"secret put value", "secretsmanager:putsecretvalue", iamCanPerformResourceTypeSecret, canPerformSecretARN},
		{"ssm get parameters", "ssm:getparameters", iamCanPerformResourceTypeSSMParam, "arn:aws:ssm:us-east-1:111122223333:parameter/app/db"},
		{"dynamodb query", "dynamodb:query", iamCanPerformResourceTypeDynamoDB, canPerformTableARN},
		{"dynamodb scan", "dynamodb:scan", iamCanPerformResourceTypeDynamoDB, canPerformTableARN},
		{"dynamodb put item", "dynamodb:putitem", iamCanPerformResourceTypeDynamoDB, canPerformTableARN},
		{"dynamodb update item", "dynamodb:updateitem", iamCanPerformResourceTypeDynamoDB, canPerformTableARN},
		{"dynamodb delete item", "dynamodb:deleteitem", iamCanPerformResourceTypeDynamoDB, canPerformTableARN},
		{"ec2 stop instances", "ec2:stopinstances", iamCanPerformResourceTypeEC2Instance, canPerformEC2InstanceARN},
		{"rds stop db instance", "rds:stopdbinstance", iamCanPerformResourceTypeRDSInstance, canPerformRDSInstanceARN},
		{"lambda invoke function", "lambda:invokefunction", iamCanPerformResourceTypeLambdaFunc, canPerformLambdaFunctionARN},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resources := []facts.Envelope{
				attackerNode(),
				canPerformNode(tc.resourceType, tc.resourceARN),
			}
			perms := []facts.Envelope{
				escalationPermissionEnvelope(attackerUserARN, "Allow", []string{tc.action}, []string{tc.resourceARN}),
			}

			result := ExtractIAMCanPerformEdges(resources, perms)
			edge := canPerformEdgeFor(
				result.Edges,
				uidOf(iamResourceTypeUser, attackerUserARN),
				canPerformUID(tc.resourceType, tc.resourceARN),
			)
			if edge == nil {
				t.Fatalf("%s must emit CAN_PERFORM edge; rows=%v tally=%+v", tc.action, result.Edges, result.Tally)
			}
			if got := edge["actions"].([]string); len(got) != 1 || got[0] != tc.action {
				t.Fatalf("actions = %v, want [%s]", got, tc.action)
			}
			if result.EdgesByMode[iamCanPerformResolutionExactARN] != 1 {
				t.Fatalf("edges-by-mode = %v, want one exact_arn", result.EdgesByMode)
			}
		})
	}
}

func TestIAMCanPerformPR4eActionRefusesWrongResourceType(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"lambda:invokefunction"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("lambda action must not resolve to an S3 bucket target; rows=%v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}
