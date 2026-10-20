// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestIdentityPoolARNDerivesPartition pins that the synthesized Cognito identity
// pool ARN inherits the partition of the scan boundary's region instead of a
// hardcoded commercial `aws`. The cognito-identity APIs return only the bare
// pool id, so the boundary is the partition source; a hardcoded partition
// dangles the identity-pool node in aws-us-gov and aws-cn.
func TestIdentityPoolARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:cognito-identity:us-east-1:123456789012:identitypool/us-east-1:pool"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:cognito-identity:us-gov-west-1:123456789012:identitypool/us-east-1:pool"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:cognito-identity:cn-north-1:123456789012:identitypool/us-east-1:pool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region, AccountID: "123456789012"}
			if got := identityPoolARN(boundary, "us-east-1:pool"); got != tc.want {
				t.Fatalf("identityPoolARN(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}
