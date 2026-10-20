// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apigateway

import "testing"

// TestRestAPIARNDerivesPartition pins that the synthesized REST API ARN inherits
// the partition of the scan boundary's region instead of a hardcoded commercial
// `aws`. API Gateway control-plane IDs carry no ARN, so the region is the
// partition source; a hardcoded partition dangles the API's graph identity in
// aws-us-gov and aws-cn.
func TestRestAPIARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:apigateway:us-east-1::/restapis/abc123"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:apigateway:us-gov-west-1::/restapis/abc123"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:apigateway:cn-north-1::/restapis/abc123"},
		{name: "blank region falls back to commercial", region: "", want: "arn:aws:apigateway:::/restapis/abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := restAPIARN(tc.region, "abc123"); got != tc.want {
				t.Fatalf("restAPIARN(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}

// TestV2APIARNDerivesPartition pins the same partition-derivation contract for
// the HTTP/WebSocket (v2) API ARN form.
func TestV2APIARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:apigateway:us-east-1::/apis/abc123"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:apigateway:us-gov-west-1::/apis/abc123"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:apigateway:cn-north-1::/apis/abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := v2APIARN(tc.region, "abc123"); got != tc.want {
				t.Fatalf("v2APIARN(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}
