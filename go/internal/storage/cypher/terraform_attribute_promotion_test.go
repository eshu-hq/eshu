// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"
)

// TestPromoteTerraformResourceAttributesAllowlistedScalars proves the
// bounded, prefixed scalar flattening for every promotable resource type in
// terraformAttributePromotionAllowlist, including the nested list-of-one-map
// shape terraform state JSON uses for MaxItems=1 blocks (versioning,
// server_side_encryption_configuration).
func TestPromoteTerraformResourceAttributesAllowlistedScalars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceType string
		attributes   map[string]any
		want         map[string]any
	}{
		{
			name:         "aws_instance",
			resourceType: "aws_instance",
			attributes: map[string]any{
				"instance_type": "t3.micro",
				"ami":           "ami-0abcdef1234567890",
				"arn":           "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc",
			},
			want: map[string]any{
				"tf_attr_instance_type": "t3.micro",
				"tf_attr_ami":           "ami-0abcdef1234567890",
			},
		},
		{
			name:         "aws_db_instance",
			resourceType: "aws_db_instance",
			attributes: map[string]any{
				"engine":         "postgres",
				"engine_version": "15.4",
				"instance_class": "db.t3.medium",
			},
			want: map[string]any{
				"tf_attr_engine":         "postgres",
				"tf_attr_engine_version": "15.4",
				"tf_attr_instance_class": "db.t3.medium",
			},
		},
		{
			name:         "aws_lambda_function",
			resourceType: "aws_lambda_function",
			attributes: map[string]any{
				"runtime":     "python3.12",
				"handler":     "index.handler",
				"memory_size": float64(512),
				"timeout":     float64(30),
			},
			want: map[string]any{
				"tf_attr_runtime":     "python3.12",
				"tf_attr_handler":     "index.handler",
				"tf_attr_memory_size": float64(512),
				"tf_attr_timeout":     float64(30),
			},
		},
		{
			name:         "aws_s3_bucket nested MaxItems=1 blocks",
			resourceType: "aws_s3_bucket",
			attributes: map[string]any{
				"acl": "private",
				"versioning": []any{
					map[string]any{"enabled": true},
				},
				"server_side_encryption_configuration": []any{
					map[string]any{
						"rule": []any{
							map[string]any{
								"apply_server_side_encryption_by_default": []any{
									map[string]any{"sse_algorithm": "aws:kms"},
								},
							},
						},
					},
				},
			},
			want: map[string]any{
				"tf_attr_acl":                "private",
				"tf_attr_versioning_enabled": true,
				"tf_attr_server_side_encryption_configuration_rule_apply_server_side_encryption_by_default_sse_algorithm": "aws:kms",
			},
		},
		{
			name:         "aws_iam_role_policy_attachment policy_arn",
			resourceType: "aws_iam_role_policy_attachment",
			attributes: map[string]any{
				"policy_arn": "arn:aws:iam::123456789012:policy/example",
			},
			want: map[string]any{
				"tf_attr_policy_arn": "arn:aws:iam::123456789012:policy/example",
			},
		},
		{
			name:         "unknown resource type promotes nothing",
			resourceType: "aws_glue_job",
			attributes: map[string]any{
				"name": "some-job",
			},
			want: nil,
		},
		{
			name:         "missing attribute in an allowlisted resource type is skipped",
			resourceType: "aws_instance",
			attributes: map[string]any{
				"instance_type": "t3.micro",
			},
			want: map[string]any{
				"tf_attr_instance_type": "t3.micro",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := promoteTerraformResourceAttributes(tt.resourceType, tt.attributes)
			if len(got) != len(tt.want) {
				t.Fatalf("promoteTerraformResourceAttributes(%s) = %#v, want %#v", tt.resourceType, got, tt.want)
			}
			for key, wantValue := range tt.want {
				if gotValue := got[key]; gotValue != wantValue {
					t.Fatalf("promoteTerraformResourceAttributes(%s)[%s] = %#v (%T), want %#v (%T)",
						tt.resourceType, key, gotValue, gotValue, wantValue, wantValue)
				}
			}
		})
	}
}

// TestPromoteTerraformResourceAttributesNonAllowlistedFieldExcluded proves a
// resource type's non-allowlisted attribute never reaches the graph even
// when the resource type itself is promotable.
func TestPromoteTerraformResourceAttributesNonAllowlistedFieldExcluded(t *testing.T) {
	t.Parallel()

	got := promoteTerraformResourceAttributes("aws_instance", map[string]any{
		"instance_type":        "t3.micro",
		"user_data":            "#!/bin/bash\necho hello",
		"iam_instance_profile": "profile-role",
	})
	if _, ok := got["tf_attr_user_data"]; ok {
		t.Fatalf("promoted non-allowlisted user_data attribute: %#v", got)
	}
	if _, ok := got["tf_attr_iam_instance_profile"]; ok {
		t.Fatalf("promoted non-allowlisted iam_instance_profile attribute: %#v", got)
	}
	if got["tf_attr_instance_type"] != "t3.micro" {
		t.Fatalf("allowlisted instance_type dropped: %#v", got)
	}
}

// TestPromoteTerraformResourceAttributesRedactsIAMPolicyDocuments is the
// redaction regression guard: aws_iam_role.assume_role_policy and
// aws_iam_policy.policy — the two free-form IAM policy JSON documents the
// drift allowlist compares in memory but never persists — must never be
// promotable onto a graph node, regardless of what the caller supplies.
// Promoting them would persist multi-KB blobs plus AWS account IDs and
// principal ARNs into a queryable store, a materially different risk
// profile than the drift path's in-memory-only comparison.
func TestPromoteTerraformResourceAttributesRedactsIAMPolicyDocuments(t *testing.T) {
	t.Parallel()

	policyDoc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"sts:AssumeRole"}]}`

	roleGot := promoteTerraformResourceAttributes("aws_iam_role", map[string]any{
		"assume_role_policy": policyDoc,
		"name":               "example-role",
	})
	if len(roleGot) != 0 {
		t.Fatalf("aws_iam_role promoted attributes, want none (policy documents must never be promoted): %#v", roleGot)
	}

	policyGot := promoteTerraformResourceAttributes("aws_iam_policy", map[string]any{
		"policy": policyDoc,
		"name":   "example-policy",
	})
	if len(policyGot) != 0 {
		t.Fatalf("aws_iam_policy promoted attributes, want none (policy documents must never be promoted): %#v", policyGot)
	}

	// Guard the allowlist definition itself, not just this function's
	// behavior, so a future edit that adds an aws_iam_role or aws_iam_policy
	// entry fails loudly here instead of silently reopening the redaction
	// gap.
	if _, ok := terraformAttributePromotionAllowlist["aws_iam_role"]; ok {
		t.Fatalf("terraformAttributePromotionAllowlist must not carry an aws_iam_role entry")
	}
	if _, ok := terraformAttributePromotionAllowlist["aws_iam_policy"]; ok {
		t.Fatalf("terraformAttributePromotionAllowlist must not carry an aws_iam_policy entry")
	}
	for resourceType, attrs := range terraformAttributePromotionAllowlist {
		for _, attr := range attrs {
			if attr == "assume_role_policy" || attr == "policy" {
				t.Fatalf("terraformAttributePromotionAllowlist[%s] allowlists a policy-document attribute %q", resourceType, attr)
			}
		}
	}
}

// TestPromoteTerraformResourceAttributesDropsOversizeValues is the defense-
// in-depth size cap: an allowlisted attribute whose value exceeds the cap is
// dropped entirely, never truncated (a truncated policy-shaped value could
// still leak a damaging prefix).
func TestPromoteTerraformResourceAttributesDropsOversizeValues(t *testing.T) {
	t.Parallel()

	oversize := strings.Repeat("a", terraformAttributePromotionValueSizeCapBytes+1)
	got := promoteTerraformResourceAttributes("aws_instance", map[string]any{
		"instance_type": "t3.micro",
		"ami":           oversize,
	})
	if _, ok := got["tf_attr_ami"]; ok {
		t.Fatalf("oversize ami value was promoted instead of dropped: len=%d", len(oversize))
	}
	if got["tf_attr_instance_type"] != "t3.micro" {
		t.Fatalf("bounded sibling attribute was dropped alongside the oversize one: %#v", got)
	}

	// The cap boundary itself must still promote.
	atCap := strings.Repeat("b", terraformAttributePromotionValueSizeCapBytes)
	gotAtCap := promoteTerraformResourceAttributes("aws_instance", map[string]any{
		"ami": atCap,
	})
	if gotAtCap["tf_attr_ami"] != atCap {
		t.Fatalf("value exactly at the size cap was dropped, want promoted unmodified")
	}
}

// TestPromoteTerraformResourceAttributesEmptyInputs covers nil/empty
// attributes and an empty resource type.
func TestPromoteTerraformResourceAttributesEmptyInputs(t *testing.T) {
	t.Parallel()

	if got := promoteTerraformResourceAttributes("aws_instance", nil); len(got) != 0 {
		t.Fatalf("nil attributes promoted %#v, want none", got)
	}
	if got := promoteTerraformResourceAttributes("", map[string]any{"instance_type": "t3.micro"}); len(got) != 0 {
		t.Fatalf("empty resource type promoted %#v, want none", got)
	}
	if got := promoteTerraformResourceAttributes("aws_instance", map[string]any{}); len(got) != 0 {
		t.Fatalf("empty attributes promoted %#v, want none", got)
	}
}
