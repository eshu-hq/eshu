// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfconfigstate

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestAttributeAllowlistIsNonEmpty(t *testing.T) {
	t.Parallel()

	types := AllowlistResourceTypes()
	if len(types) == 0 {
		t.Fatal("AllowlistResourceTypes() is empty, want non-empty seed")
	}
	if len(types) < 5 {
		t.Fatalf("AllowlistResourceTypes() = %d, want >= 5 seed entries", len(types))
	}
}

func TestAttributeAllowlistResourceTypesAreSorted(t *testing.T) {
	t.Parallel()

	types := AllowlistResourceTypes()
	if !sort.StringsAreSorted(types) {
		t.Fatalf("AllowlistResourceTypes() = %v, want sorted", types)
	}
}

func TestAttributeAllowlistHasNoDuplicateAttributes(t *testing.T) {
	t.Parallel()

	for _, rt := range AllowlistResourceTypes() {
		attrs := AllowlistFor(rt)
		seen := map[string]struct{}{}
		for _, a := range attrs {
			if strings.TrimSpace(a) == "" {
				t.Fatalf("AllowlistFor(%q) contains a blank attribute", rt)
			}
			if _, ok := seen[a]; ok {
				t.Fatalf("AllowlistFor(%q) contains duplicate attribute %q", rt, a)
			}
			seen[a] = struct{}{}
		}
	}
}

func TestAllowlistForUnknownTypeReturnsNil(t *testing.T) {
	t.Parallel()

	if got := AllowlistFor("aws_made_up_thing"); got != nil {
		t.Fatalf("AllowlistFor(unknown) = %v, want nil", got)
	}
}

func TestAllowlistForReturnsCopy(t *testing.T) {
	t.Parallel()

	got := AllowlistFor("aws_s3_bucket")
	if len(got) == 0 {
		t.Fatal("AllowlistFor(aws_s3_bucket) = empty, want non-empty")
	}
	got[0] = "MUTATED"

	again := AllowlistFor("aws_s3_bucket")
	if again[0] == "MUTATED" {
		t.Fatal("AllowlistFor returned a shared slice; mutation leaked across calls")
	}
}

func TestAttributeAllowlistCoversV1Surface(t *testing.T) {
	t.Parallel()

	expectations := map[string][]string{
		"aws_s3_bucket": {
			"acl",
			"versioning.enabled",
			"server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm",
		},
		"aws_instance":                   {"instance_type", "ami"},
		"aws_lambda_function":            {"runtime", "handler", "memory_size", "timeout"},
		"aws_db_instance":                {"engine", "engine_version", "instance_class"},
		"aws_iam_role":                   {"assume_role_policy"},
		"aws_iam_policy":                 {"policy"},
		"aws_iam_role_policy_attachment": {"policy_arn"},
	}

	for resourceType, want := range expectations {
		got := AllowlistFor(resourceType)
		gotSet := make(map[string]struct{}, len(got))
		for _, a := range got {
			gotSet[a] = struct{}{}
		}
		for _, attr := range want {
			if _, ok := gotSet[attr]; !ok {
				t.Errorf("AllowlistFor(%q) missing %q; have %v", resourceType, attr, got)
			}
		}
	}
}

// TestAttributeAllowlistMatchesContractsTable locks the classifier table
// byte-for-byte to the contracts-owned drift allowlist
// (sdk/go/factschema/terraformstate/v1 DriftAttributeAllowlist). Both sides
// carry the same literals until AllowlistFor delegates to the contracts
// module after the eshu-sdk split publishes the file; any divergence on
// either side fails here first so the handoff cannot silently skew what
// drift can fire versus what satellites may emit.
func TestAttributeAllowlistMatchesContractsTable(t *testing.T) {
	t.Parallel()

	want := map[string][]string{
		"aws_s3_bucket": {
			"versioning.enabled",
			"acl",
			"server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm",
		},
		"aws_instance": {
			"instance_type",
			"ami",
		},
		"aws_db_instance": {
			"engine",
			"engine_version",
			"instance_class",
		},
		"aws_lambda_function": {
			"runtime",
			"handler",
			"memory_size",
			"timeout",
		},
		"aws_iam_role": {
			"assume_role_policy",
		},
		"aws_iam_policy": {
			"policy",
		},
		"aws_iam_role_policy_attachment": {
			"policy_arn",
		},
	}
	for resourceType, wantAttrs := range want {
		if got := AllowlistFor(resourceType); !reflect.DeepEqual(got, wantAttrs) {
			t.Errorf("AllowlistFor(%q) = %v, want contracts table %v (order-sensitive)", resourceType, got, wantAttrs)
		}
	}
	if got := AllowlistResourceTypes(); !reflect.DeepEqual(got, []string{
		"aws_db_instance",
		"aws_iam_policy",
		"aws_iam_role",
		"aws_iam_role_policy_attachment",
		"aws_instance",
		"aws_lambda_function",
		"aws_s3_bucket",
	}) {
		t.Errorf("AllowlistResourceTypes() = %v, want exactly the contracts coverage", got)
	}
}
