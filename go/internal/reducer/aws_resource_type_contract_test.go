// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

func TestReducerAWSResourceTypeConstantsMatchFactSchema(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		got  string
		want string
	}{
		"iam_role":                  {got: iamResourceTypeRole, want: awsv1.ResourceTypeIAMRole},
		"iam_user":                  {got: iamResourceTypeUser, want: awsv1.ResourceTypeIAMUser},
		"iam_policy":                {got: iamResourceTypePolicy, want: awsv1.ResourceTypeIAMPolicy},
		"iam_group":                 {got: iamResourceTypeGroup, want: awsv1.ResourceTypeIAMGroup},
		"iam_instance_profile":      {got: ec2UsesProfileResourceTypeInstanceProfile, want: awsv1.ResourceTypeIAMInstanceProfile},
		"ec2_instance":              {got: ec2UsesProfileResourceTypeInstance, want: awsv1.ResourceTypeEC2Instance},
		"s3_bucket":                 {got: s3LogsToResourceTypeBucket, want: awsv1.ResourceTypeS3Bucket},
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
