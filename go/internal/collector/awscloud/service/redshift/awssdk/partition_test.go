// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestRedshiftSynthesizedARNsDerivePartition pins that every Redshift ARN the
// adapter synthesizes (cluster, parameter group, subnet group, snapshot) carries
// the partition of the scan boundary's region instead of a hardcoded commercial
// `aws`. The provisioned Redshift shapes omit these ARNs, so the boundary is the
// partition source; a hardcoded partition dangles the node identity in
// aws-us-gov and aws-cn.
func TestRedshiftSynthesizedARNsDerivePartition(t *testing.T) {
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
			checks := []struct {
				what string
				got  string
				want string
			}{
				{what: "cluster", got: clusterARN(boundary, "wh"), want: "arn:" + p + ":redshift:" + reg + ":" + account + ":cluster:wh"},
				{what: "parameterGroup", got: parameterGroupARN(boundary, "pg"), want: "arn:" + p + ":redshift:" + reg + ":" + account + ":parametergroup:pg"},
				{what: "subnetGroup", got: subnetGroupARN(boundary, "sng"), want: "arn:" + p + ":redshift:" + reg + ":" + account + ":subnetgroup:sng"},
				{what: "snapshot", got: snapshotARN(boundary, "wh", "snap1"), want: "arn:" + p + ":redshift:" + reg + ":" + account + ":snapshot:wh/snap1"},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Fatalf("%s ARN = %q, want %q", c.what, c.got, c.want)
				}
			}
		})
	}
}
