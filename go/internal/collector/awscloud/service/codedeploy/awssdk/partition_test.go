// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestClientSynthesizedARNsDerivePartition pins that the adapter-side
// application and deployment-group ARNs (used to tag-target CodeDeploy
// resources) carry the partition of the scan boundary's region instead of a
// hardcoded commercial `aws`. The CodeDeploy APIs return no ARNs, so the
// boundary is the partition source; a hardcoded partition addresses the wrong
// partition's resource in aws-us-gov and aws-cn.
func TestClientSynthesizedARNsDerivePartition(t *testing.T) {
	regions := []struct {
		name      string
		region    string
		partition string
	}{
		{name: "commercial", region: "us-east-1", partition: "aws"},
		{name: "govcloud", region: "us-gov-west-1", partition: "aws-us-gov"},
		{name: "china", region: "cn-north-1", partition: "aws-cn"},
		{name: "blank falls back to commercial", region: "", partition: "aws"},
	}
	const account = "123456789012"
	for _, r := range regions {
		t.Run(r.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: r.region, AccountID: account}
			p := r.partition
			reg := r.region
			if got, want := applicationARN(boundary, "app"), "arn:"+p+":codedeploy:"+reg+":"+account+":application:app"; got != want {
				t.Fatalf("applicationARN = %q, want %q", got, want)
			}
			if got, want := deploymentGroupARN(boundary, "app", "grp"), "arn:"+p+":codedeploy:"+reg+":"+account+":deploymentgroup:app/grp"; got != want {
				t.Fatalf("deploymentGroupARN = %q, want %q", got, want)
			}
		})
	}
}
