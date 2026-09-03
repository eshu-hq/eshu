// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// TestCanPerformResourceTypeConstantsMatchFactSchema pins the CAN_PERFORM
// catalog's expected resource_type tokens to the factschema constants the
// awscloud collector emits. A drift here silently stops target resolution: the
// resolver requires a matched scanned node classify as the catalog entry's
// expected type, so a token that no longer matches an emitted resource_type
// resolves nothing and every action in that service degrades to a counted skip
// rather than a visible failure.
//
// These rows moved here with the family in #6061; the root's
// aws_resource_type_contract_test.go keeps the tokens the root still owns.
func TestCanPerformResourceTypeConstantsMatchFactSchema(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		got  string
		want string
	}{
		"can_perform_s3_bucket":     {got: iamCanPerformResourceTypeS3Bucket, want: awsv1.ResourceTypeS3Bucket},
		"can_perform_kms_key":       {got: iamCanPerformResourceTypeKMSKey, want: awsv1.ResourceTypeKMSKey},
		"can_perform_secret":        {got: iamCanPerformResourceTypeSecret, want: awsv1.ResourceTypeSecretsManagerSecret},
		"can_perform_ssm_parameter": {got: iamCanPerformResourceTypeSSMParam, want: awsv1.ResourceTypeSSMParameter},
		"can_perform_dynamodb":      {got: iamCanPerformResourceTypeDynamoDB, want: awsv1.ResourceTypeDynamoDBTable},
		"can_perform_ec2_instance":  {got: iamCanPerformResourceTypeEC2Instance, want: awsv1.ResourceTypeEC2Instance},
		"can_perform_rds_instance":  {got: iamCanPerformResourceTypeRDSInstance, want: awsv1.ResourceTypeRDSDBInstance},
		"can_perform_lambda":        {got: iamCanPerformResourceTypeLambdaFunc, want: awsv1.ResourceTypeLambdaFunction},
	}
	for name, tt := range tests {
		name, tt := name, tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Fatalf("resource type = %q, want factschema token %q", tt.got, tt.want)
			}
		})
	}
}
