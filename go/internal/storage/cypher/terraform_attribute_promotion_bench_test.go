// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "testing"

// BenchmarkPromoteTerraformResourceAttributes measures
// promoteTerraformResourceAttributes' per-call cost across the pre-#5446
// allowlist shape (aws_instance, a 2-attribute-path entry) and the largest
// post-#5446 entry (aws_lambda_function, a 6-attribute-path entry after
// #5446 added version/image_uri to the pre-existing 4), plus an
// unrecognized-type miss (the map-lookup-only fast path every non-Terraform
// or non-allowlisted resource row takes). This is the #5446 Prove-The-
// Theory-First proof for the allowlist extension: the function's cost is
// O(len(allowlist[resourceType])) attribute-path walks after one O(1) map
// lookup, so adding entries to OTHER resource types can never affect
// aws_instance's own cost, and growing one entry's own attribute count by a
// few paths is bounded by a few more terraformAttributePathValue calls, not
// a new order of growth.
func BenchmarkPromoteTerraformResourceAttributes(b *testing.B) {
	cases := []struct {
		name         string
		resourceType string
		attributes   map[string]any
	}{
		{
			name:         "aws_instance_2attr_preexisting_shape",
			resourceType: "aws_instance",
			attributes: map[string]any{
				"instance_type": "t3.micro",
				"ami":           "ami-0abcdef1234567890",
				"arn":           "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc",
				"user_data":     "#!/bin/bash\necho hello",
			},
		},
		{
			name:         "aws_lambda_function_6attr_post_5446_shape",
			resourceType: "aws_lambda_function",
			attributes: map[string]any{
				"runtime":       "python3.12",
				"handler":       "index.handler",
				"memory_size":   float64(512),
				"timeout":       float64(30),
				"version":       "3",
				"image_uri":     "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
				"qualified_arn": "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo:3",
			},
		},
		{
			name:         "unrecognized_type_map_lookup_miss",
			resourceType: "aws_glue_job",
			attributes: map[string]any{
				"name": "some-job",
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = promoteTerraformResourceAttributes(tc.resourceType, tc.attributes)
			}
		})
	}
}
