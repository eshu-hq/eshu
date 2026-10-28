// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "sort"

// DriftAttributeAllowlist is the single source of truth for the Terraform
// state attribute paths that config-vs-state drift detection compares. It
// maps a Terraform resource type to the deterministic, ordered list of
// dot-path attribute keys the attribute_drift classifier evaluates.
//
// Keep this list small and high-signal — every entry expands both the
// false-positive surface and the satellite emission surface. The Phase 1
// seed is operator-meaningful drift only:
//
//   - bucket-level configuration (versioning, ACL, encryption)
//   - compute identity (instance_type, runtime, handler, memory_size)
//   - database engine identity (engine, engine_version)
//   - IAM policy identity (assume_role_policy, policy_arn)
//
// Consumers:
//   - Host classifier: go/internal/correlation/drift/tfconfigstate AllowlistFor
//     delegates to DriftAllowlistFor so the compared set cannot drift from
//     this contract.
//   - Extracted satellites (eshu-collector-terraform-state): emission is
//     allowlist-only and fail-closed — a resource type with no entry emits no
//     attributes, and only the listed leaf paths are emitted, re-nested in
//     the shape the drift evidence loader flattens. The satellite additionally
//     intersects this list with its hard-sensitive deny list (aws_iam_policy
//     policy is allowlisted here but never emitted there); the classifier
//     skips absent state values, so that exclusion is a documented coverage
//     gap, not false drift.
//
// Promotion to a versioned data file lives in a follow-up ADR.
var DriftAttributeAllowlist = map[string][]string{
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

// DriftAllowlistFor returns the ordered drift allowlist for one resource
// type. Returns nil when the type has no entry — attribute_drift cannot fire
// for such resources, and satellites must emit no attributes for them.
func DriftAllowlistFor(resourceType string) []string {
	attrs, ok := DriftAttributeAllowlist[resourceType]
	if !ok {
		return nil
	}
	out := make([]string, len(attrs))
	copy(out, attrs)
	return out
}

// DriftAllowlistResourceTypes returns the sorted list of resource types
// covered by the drift allowlist.
func DriftAllowlistResourceTypes() []string {
	out := make([]string, 0, len(DriftAttributeAllowlist))
	for k := range DriftAttributeAllowlist {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
