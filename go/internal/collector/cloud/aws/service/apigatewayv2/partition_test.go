// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apigatewayv2

import "testing"

// TestAPIARNDerivesPartition pins that the synthesized API Gateway v2 API ARN
// inherits the partition of the scan boundary's region instead of a hardcoded
// commercial `aws`. The v2 control-plane id carries no ARN, so the region is the
// partition source; a hardcoded partition dangles the API identity in
// aws-us-gov and aws-cn.
func TestAPIARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:apigateway:us-east-1::/apis/abc123"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:apigateway:us-gov-west-1::/apis/abc123"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:apigateway:cn-north-1::/apis/abc123"},
		{name: "blank region falls back to commercial", region: "", want: "arn:aws:apigateway:::/apis/abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := apiARN(tc.region, "abc123"); got != tc.want {
				t.Fatalf("apiARN(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}
