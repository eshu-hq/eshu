// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redshift

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestSecurityGroupARNDerivesPartition pins that the synthesized EC2
// security-group ARN (the Redshift cluster -> security-group join target)
// inherits the partition of the scan boundary's region instead of a hardcoded
// commercial `aws`. EC2 reports the bare group id, so the boundary is the
// partition source; a hardcoded partition dangles the edge in aws-us-gov and
// aws-cn. A value already shaped as an ARN is passed through unchanged.
func TestSecurityGroupARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name    string
		region  string
		groupID string
		want    string
	}{
		{name: "commercial", region: "us-east-1", groupID: "sg-123", want: "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123"},
		{name: "govcloud", region: "us-gov-west-1", groupID: "sg-123", want: "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:security-group/sg-123"},
		{name: "china", region: "cn-north-1", groupID: "sg-123", want: "arn:aws-cn:ec2:cn-north-1:123456789012:security-group/sg-123"},
		{name: "already an arn is passed through", region: "us-gov-west-1", groupID: "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:security-group/sg-9", want: "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:security-group/sg-9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region, AccountID: "123456789012"}
			if got := securityGroupARN(boundary, tc.groupID); got != tc.want {
				t.Fatalf("securityGroupARN(%q, %q) = %q, want %q", tc.region, tc.groupID, got, tc.want)
			}
		})
	}
}
