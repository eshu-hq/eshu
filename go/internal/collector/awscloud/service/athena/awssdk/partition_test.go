// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestWorkGroupARNDerivesPartition pins that the synthesized Athena workgroup ARN
// inherits the partition of the scan boundary's region instead of a hardcoded
// commercial `aws`. The Athena APIs return no workgroup ARN, so the boundary is
// the partition source; a hardcoded partition dangles the workgroup identity in
// aws-us-gov and aws-cn.
func TestWorkGroupARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:athena:us-east-1:123456789012:workgroup/primary"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:athena:us-gov-west-1:123456789012:workgroup/primary"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:athena:cn-north-1:123456789012:workgroup/primary"},
		{name: "blank region falls back to commercial", region: "", want: "arn:aws:athena::123456789012:workgroup/primary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region, AccountID: "123456789012"}
			if got := workGroupARN(boundary, "primary"); got != tc.want {
				t.Fatalf("workGroupARN(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}

// TestDataCatalogARNDerivesPartition pins the same partition-derivation contract
// for the Athena data-catalog ARN form.
func TestDataCatalogARNDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:athena:us-east-1:123456789012:datacatalog/AwsDataCatalog"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:athena:us-gov-west-1:123456789012:datacatalog/AwsDataCatalog"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:athena:cn-north-1:123456789012:datacatalog/AwsDataCatalog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region, AccountID: "123456789012"}
			if got := dataCatalogARN(boundary, "AwsDataCatalog"); got != tc.want {
				t.Fatalf("dataCatalogARN(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}
