// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"

	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

func TestAWSCloudResourceTypeConstantsMatchFactSchema(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		got  string
		want string
	}{
		"iam_role":             {got: catalog.ResourceTypeIAMRole, want: awsv1.ResourceTypeIAMRole},
		"iam_user":             {got: catalog.ResourceTypeIAMUser, want: awsv1.ResourceTypeIAMUser},
		"iam_group":            {got: catalog.ResourceTypeIAMGroup, want: awsv1.ResourceTypeIAMGroup},
		"iam_policy":           {got: catalog.ResourceTypeIAMPolicy, want: awsv1.ResourceTypeIAMPolicy},
		"iam_instance_profile": {got: catalog.ResourceTypeIAMInstanceProfile, want: awsv1.ResourceTypeIAMInstanceProfile},
		"iam_principal":        {got: catalog.ResourceTypeIAMPrincipal, want: awsv1.ResourceTypeIAMPrincipal},
		"ec2_vpc":              {got: catalog.ResourceTypeEC2VPC, want: awsv1.ResourceTypeEC2VPC},
		"ec2_subnet":           {got: catalog.ResourceTypeEC2Subnet, want: awsv1.ResourceTypeEC2Subnet},
		"ec2_security_group":   {got: catalog.ResourceTypeEC2SecurityGroup, want: awsv1.ResourceTypeEC2SecurityGroup},
		"ec2_security_group_rule": {
			got:  catalog.ResourceTypeEC2SecurityGroupRule,
			want: awsv1.ResourceTypeEC2SecurityGroupRule,
		},
		"ec2_network_interface": {got: catalog.ResourceTypeEC2NetworkInterface, want: awsv1.ResourceTypeEC2NetworkInterface},
		"ec2_volume":            {got: catalog.ResourceTypeEC2Volume, want: awsv1.ResourceTypeEC2Volume},
		"ec2_instance":          {got: catalog.ResourceTypeEC2Instance, want: awsv1.ResourceTypeEC2Instance},
		"ec2_ami":               {got: catalog.ResourceTypeEC2AMI, want: awsv1.ResourceTypeEC2AMI},
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
