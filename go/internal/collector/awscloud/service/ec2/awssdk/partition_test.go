// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import "testing"

// TestEC2InstanceARNDerivesPartition pins that the synthesized EC2 instance ARN
// (used as a network-interface attachment target) inherits the partition of the
// instance's region instead of a hardcoded commercial `aws`. A hardcoded
// partition dangles the ENI->instance edge in aws-us-gov and aws-cn.
func TestEC2InstanceARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:ec2:us-east-1:123456789012:instance/i-abc"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:instance/i-abc"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:ec2:cn-north-1:123456789012:instance/i-abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ec2InstanceARN(tc.region, "123456789012", "i-abc"); got != tc.want {
				t.Fatalf("ec2InstanceARN(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}
