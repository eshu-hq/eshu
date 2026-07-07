// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

func TestAWSCloudResourceTypeConstantsMatchFactSchema(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		got  string
		want string
	}{
		"iam_role":             {got: ResourceTypeIAMRole, want: awsv1.ResourceTypeIAMRole},
		"iam_user":             {got: ResourceTypeIAMUser, want: awsv1.ResourceTypeIAMUser},
		"iam_group":            {got: ResourceTypeIAMGroup, want: awsv1.ResourceTypeIAMGroup},
		"iam_policy":           {got: ResourceTypeIAMPolicy, want: awsv1.ResourceTypeIAMPolicy},
		"iam_instance_profile": {got: ResourceTypeIAMInstanceProfile, want: awsv1.ResourceTypeIAMInstanceProfile},
		"iam_principal":        {got: ResourceTypeIAMPrincipal, want: awsv1.ResourceTypeIAMPrincipal},
		"ec2_vpc":              {got: ResourceTypeEC2VPC, want: awsv1.ResourceTypeEC2VPC},
		"ec2_subnet":           {got: ResourceTypeEC2Subnet, want: awsv1.ResourceTypeEC2Subnet},
		"ec2_security_group":   {got: ResourceTypeEC2SecurityGroup, want: awsv1.ResourceTypeEC2SecurityGroup},
		"ec2_security_group_rule": {
			got:  ResourceTypeEC2SecurityGroupRule,
			want: awsv1.ResourceTypeEC2SecurityGroupRule,
		},
		"ec2_network_interface": {got: ResourceTypeEC2NetworkInterface, want: awsv1.ResourceTypeEC2NetworkInterface},
		"ec2_volume":            {got: ResourceTypeEC2Volume, want: awsv1.ResourceTypeEC2Volume},
		"ec2_instance":          {got: ResourceTypeEC2Instance, want: awsv1.ResourceTypeEC2Instance},
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
