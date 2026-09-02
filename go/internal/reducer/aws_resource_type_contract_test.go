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
		"iam_role":             {got: iamResourceTypeRole, want: awsv1.ResourceTypeIAMRole},
		"iam_user":             {got: iamResourceTypeUser, want: awsv1.ResourceTypeIAMUser},
		"iam_policy":           {got: iamResourceTypePolicy, want: awsv1.ResourceTypeIAMPolicy},
		"iam_group":            {got: iamResourceTypeGroup, want: awsv1.ResourceTypeIAMGroup},
		"iam_instance_profile": {got: ec2UsesProfileResourceTypeInstanceProfile, want: awsv1.ResourceTypeIAMInstanceProfile},
		"ec2_instance":         {got: ec2UsesProfileResourceTypeInstance, want: awsv1.ResourceTypeEC2Instance},
		"s3_bucket":            {got: s3LogsToResourceTypeBucket, want: awsv1.ResourceTypeS3Bucket},
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
