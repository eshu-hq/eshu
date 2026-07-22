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
			name:         "aws_ecs_task_definition",
			resourceType: "aws_ecs_task_definition",
			attributes: map[string]any{
				"family":                "supply-chain-demo",
				"revision":              float64(4),
				"container_definitions": `[{"name":"app","environment":[{"name":"SECRET","value":"shh"}]}]`,
				"arn":                   "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:4",
			},
			want: map[string]any{
				"tf_attr_family":   "supply-chain-demo",
				"tf_attr_revision": float64(4),
			},
		},
		{
			name:         "aws_ecs_service",
			resourceType: "aws_ecs_service",
			attributes: map[string]any{
				"task_definition": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:4",
				"desired_count":   float64(2),
			},
			want: map[string]any{
				"tf_attr_task_definition": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:4",
			},
		},
		{
			name:         "aws_db_instance endpoint",
			resourceType: "aws_db_instance",
			attributes: map[string]any{
				"engine":         "postgres",
				"engine_version": "15.4",
				"instance_class": "db.t3.medium",
				"endpoint":       "supply-chain-demo.abc123.us-east-1.rds.amazonaws.com:5432",
			},
			want: map[string]any{
				"tf_attr_engine":         "postgres",
				"tf_attr_engine_version": "15.4",
				"tf_attr_instance_class": "db.t3.medium",
				"tf_attr_endpoint":       "supply-chain-demo.abc123.us-east-1.rds.amazonaws.com:5432",
			},
		},
		{
			name:         "aws_lambda_function version and image_uri",
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
			want: map[string]any{
				"tf_attr_runtime":     "python3.12",
				"tf_attr_handler":     "index.handler",
				"tf_attr_memory_size": float64(512),
				"tf_attr_timeout":     float64(30),
				"tf_attr_version":     "3",
				"tf_attr_image_uri":   "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
			},
		},
		{
			name:         "aws_rds_cluster",
			resourceType: "aws_rds_cluster",
			attributes: map[string]any{
				"endpoint":        "supply-chain-demo.cluster-abc123.us-east-1.rds.amazonaws.com",
				"reader_endpoint": "supply-chain-demo.cluster-ro-abc123.us-east-1.rds.amazonaws.com",
			},
			want: map[string]any{
				"tf_attr_endpoint":        "supply-chain-demo.cluster-abc123.us-east-1.rds.amazonaws.com",
				"tf_attr_reader_endpoint": "supply-chain-demo.cluster-ro-abc123.us-east-1.rds.amazonaws.com",
			},
		},
		{
			name:         "aws_lb",
			resourceType: "aws_lb",
			attributes: map[string]any{
				"dns_name": "supply-chain-demo-123456789.us-east-1.elb.amazonaws.com",
			},
			want: map[string]any{
				"tf_attr_dns_name": "supply-chain-demo-123456789.us-east-1.elb.amazonaws.com",
			},
		},
		{
			name:         "aws_elasticache_replication_group",
			resourceType: "aws_elasticache_replication_group",
			attributes: map[string]any{
				"primary_endpoint_address": "supply-chain-demo.abc123.ng.0001.use1.cache.amazonaws.com",
				"cache_nodes": []any{
					map[string]any{"id": "0001"},
				},
			},
			want: map[string]any{
				"tf_attr_primary_endpoint_address": "supply-chain-demo.abc123.ng.0001.use1.cache.amazonaws.com",
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

// TestPromoteTerraformResourceAttributesExcludesSensitiveOrUnbounded is the
// #5446 redaction regression guard for the three attributes considered and
// deliberately rejected during that allowlist extension: a JSON policy-shaped
// document, a redundant version-qualified duplicate, and a non-scalar list.
// See terraformAttributePromotionAllowlist's doc comment for the reasoning
// behind each exclusion.
func TestPromoteTerraformResourceAttributesExcludesSensitiveOrUnbounded(t *testing.T) {
	t.Parallel()

	taskDefGot := promoteTerraformResourceAttributes("aws_ecs_task_definition", map[string]any{
		"family":                "supply-chain-demo",
		"revision":              float64(4),
		"container_definitions": `[{"name":"app","environment":[{"name":"SECRET","value":"shh"}]}]`,
	})
	if _, ok := taskDefGot["tf_attr_container_definitions"]; ok {
		t.Fatalf("promoted container_definitions JSON document: %#v", taskDefGot)
	}
	if taskDefGot["tf_attr_family"] != "supply-chain-demo" {
		t.Fatalf("allowlisted family dropped alongside excluded container_definitions: %#v", taskDefGot)
	}

	lambdaGot := promoteTerraformResourceAttributes("aws_lambda_function", map[string]any{
		"version":       "3",
		"qualified_arn": "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo:3",
	})
	if _, ok := lambdaGot["tf_attr_qualified_arn"]; ok {
		t.Fatalf("promoted non-allowlisted qualified_arn attribute: %#v", lambdaGot)
	}
	if lambdaGot["tf_attr_version"] != "3" {
		t.Fatalf("allowlisted version dropped alongside excluded qualified_arn: %#v", lambdaGot)
	}

	cacheGot := promoteTerraformResourceAttributes("aws_elasticache_replication_group", map[string]any{
		"primary_endpoint_address": "supply-chain-demo.abc123.ng.0001.use1.cache.amazonaws.com",
		"cache_nodes": []any{
			map[string]any{"id": "0001"},
		},
	})
	if _, ok := cacheGot["tf_attr_cache_nodes"]; ok {
		t.Fatalf("promoted non-allowlisted composite cache_nodes attribute: %#v", cacheGot)
	}
	if cacheGot["tf_attr_primary_endpoint_address"] == nil {
		t.Fatalf("allowlisted primary_endpoint_address dropped: %#v", cacheGot)
	}

	// Guard the allowlist definition itself so a future edit cannot silently
	// reopen any of the three exclusions.
	for resourceType, attrs := range terraformAttributePromotionAllowlist {
		for _, attr := range attrs {
			if attr == "container_definitions" {
				t.Fatalf("terraformAttributePromotionAllowlist[%s] allowlists the JSON policy-shaped container_definitions attribute", resourceType)
			}
			if attr == "qualified_arn" {
				t.Fatalf("terraformAttributePromotionAllowlist[%s] allowlists the redundant qualified_arn attribute", resourceType)
			}
			if attr == "cache_nodes" {
				t.Fatalf("terraformAttributePromotionAllowlist[%s] allowlists the composite cache_nodes attribute", resourceType)
			}
		}
	}
}

// TestPromoteTerraformResourceAttributesRejectsListValuedAttributes is P2
// finding F4: an allowlisted attribute whose raw value is unexpectedly a
// list ([]string or []any) must never be promoted as a list property.
// canonicalGraphPropertyValue (reused here for scalar normalization) also
// accepts []string/[]any for the generic entity-metadata path, so
// promoteTerraformResourceAttributes must gate on scalar kinds itself
// before calling it, or a malformed/future-drifted attribute value would
// silently promote a list, contradicting this function's "proven scalar"
// doc claim.
func TestPromoteTerraformResourceAttributesRejectsListValuedAttributes(t *testing.T) {
	t.Parallel()

	gotStringSlice := promoteTerraformResourceAttributes("aws_instance", map[string]any{
		"instance_type": []string{"t3.micro", "t3.small"},
		"ami":           "ami-0abcdef1234567890",
	})
	if _, ok := gotStringSlice["tf_attr_instance_type"]; ok {
		t.Fatalf("promoted a []string-valued attribute as a property: %#v", gotStringSlice)
	}
	if gotStringSlice["tf_attr_ami"] != "ami-0abcdef1234567890" {
		t.Fatalf("bounded sibling attribute was dropped alongside the rejected list: %#v", gotStringSlice)
	}

	gotAnySlice := promoteTerraformResourceAttributes("aws_instance", map[string]any{
		"instance_type": []any{"t3.micro", "t3.small"},
	})
	if _, ok := gotAnySlice["tf_attr_instance_type"]; ok {
		t.Fatalf("promoted a []any-valued attribute as a property: %#v", gotAnySlice)
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
