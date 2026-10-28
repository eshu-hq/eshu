// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"reflect"
	"testing"
)

// TestDriftAllowlistContent locks the allowlist surface the host classifier
// and the extracted satellites share. Any addition or removal changes what
// drift can fire and what satellites may emit, so it must be a deliberate,
// reviewed edit to this table — not a silent drift of the map.
func TestDriftAllowlistContent(t *testing.T) {
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
	if !reflect.DeepEqual(driftAttributeAllowlist, want) {
		t.Fatalf("driftAttributeAllowlist = %#v, want %#v", driftAttributeAllowlist, want)
	}
	if got := DriftAllowlistResourceTypes(); !reflect.DeepEqual(got, []string{
		"aws_db_instance",
		"aws_iam_policy",
		"aws_iam_role",
		"aws_iam_role_policy_attachment",
		"aws_instance",
		"aws_lambda_function",
		"aws_s3_bucket",
	}) {
		t.Fatalf("DriftAllowlistResourceTypes() = %v, want sorted coverage", got)
	}
	if got := DriftAllowlistFor("aws_made_up_thing"); got != nil {
		t.Fatalf("DriftAllowlistFor(unknown) = %v, want nil", got)
	}
	got := DriftAllowlistFor("aws_s3_bucket")
	got[0] = "mutated"
	if driftAttributeAllowlist["aws_s3_bucket"][0] == "mutated" {
		t.Fatal("DriftAllowlistFor must return a copy: caller mutated the shared table")
	}
}
