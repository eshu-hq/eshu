// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedeploy

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestCodeDeploySynthesizedARNsDerivePartition pins that every ARN the
// observation layer synthesizes (application, deployment group, deployment
// config, ECS service target, Lambda function target, deployment) carries the
// partition of the scan boundary's region instead of a hardcoded commercial
// `aws`. The CodeDeploy list/batch APIs return no ARNs, so the boundary is the
// partition source; a hardcoded partition dangles the node identity and the
// ECS/Lambda deployment-target edges in aws-us-gov and aws-cn.
func TestCodeDeploySynthesizedARNsDerivePartition(t *testing.T) {
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
				{what: "application", got: applicationARN(boundary, "app"), want: "arn:" + p + ":codedeploy:" + reg + ":" + account + ":application:app"},
				{what: "deploymentGroup", got: deploymentGroupARN(boundary, "app", "grp"), want: "arn:" + p + ":codedeploy:" + reg + ":" + account + ":deploymentgroup:app/grp"},
				{what: "deploymentConfig", got: deploymentConfigARN(boundary, "cfg"), want: "arn:" + p + ":codedeploy:" + reg + ":" + account + ":deploymentconfig:cfg"},
				{what: "ecsService", got: ecsServiceARN(boundary, "cl", "svc"), want: "arn:" + p + ":ecs:" + reg + ":" + account + ":service/cl/svc"},
				{what: "lambdaFunction", got: lambdaFunctionARN(boundary, "fn"), want: "arn:" + p + ":lambda:" + reg + ":" + account + ":function:fn"},
				{what: "deployment", got: deploymentARN(boundary, "d-1"), want: "arn:" + p + ":codedeploy:" + reg + ":" + account + ":deployment:d-1"},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Fatalf("%s ARN = %q, want %q", c.what, c.got, c.want)
				}
			}
		})
	}
}
