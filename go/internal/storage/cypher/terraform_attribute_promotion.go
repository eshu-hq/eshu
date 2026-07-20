// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "strings"

// terraformAttributePromotionKeyPrefix namespaces every promoted Terraform
// resource attribute on a TerraformResource node (e.g. "instance_type"
// becomes the "tf_attr_instance_type" node property) so promoted attributes
// never collide with the writer's own fixed properties (address, mode,
// resource_type, ...) or a future generic entity-metadata key.
const terraformAttributePromotionKeyPrefix = "tf_attr_"

// terraformAttributePromotionValueSizeCapBytes bounds any single promoted
// attribute value. This is defense in depth on top of the allowlist below:
// even an allowlisted attribute is dropped, not truncated, once it exceeds
// this size, so a future allowlist addition cannot silently bloat every
// TerraformResource node with an unexpectedly large value. Truncating
// instead of dropping was rejected — a truncated policy-shaped value could
// still leak a damaging prefix.
const terraformAttributePromotionValueSizeCapBytes = 512

// terraformAttributePromotionAllowlist maps a Terraform resource type to the
// bounded, ordered set of attribute paths that may be promoted from the raw
// collector-classified terraform_state_resource Attributes object
// (sdk/go/factschema/terraformstate/v1.Resource.Attributes) onto the
// resource's TerraformResource graph node.
//
// This is deliberately NOT the same list as
// internal/correlation/drift/tfconfigstate.attributeAllowlist, even though
// both start from Phase 1's operator-meaningful attribute set. The drift
// allowlist only ever COMPARES two ResourceRow.Attributes values in memory
// to emit a boolean drift signal; the compared value is never written
// anywhere durable. This allowlist instead selects attributes that get
// PERSISTED as queryable node properties on every ingest, forever, in a
// shared graph store — a materially different risk profile. Concretely,
// this list excludes the drift allowlist's aws_iam_role.assume_role_policy
// and aws_iam_policy.policy entries: both are full IAM policy JSON
// documents that can run to several KB and embed AWS account IDs and
// principal ARNs. Comparing them in memory for one drift check is safe;
// writing them onto every IAM role/policy node in the graph is not (#5441).
//
// aws_iam_role_policy_attachment.policy_arn is a narrower judgment call:
// unlike the two policy documents it is a single bounded string, and
// "which policy is this role attached to" is core deployment-topology
// truth (the graph's whole reason to exist), so it is included even though
// a customer-managed policy ARN embeds the AWS account ID — the same
// account ID already appears throughout the graph on every other ARN-keyed
// CloudResource node.
//
// Only add an entry here after confirming the attribute is a bounded
// scalar (or a scalar nested behind a Terraform MaxItems=1 block) and after
// running it past the redaction guard in
// terraform_attribute_promotion_test.go.
var terraformAttributePromotionAllowlist = map[string][]string{
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
	"aws_s3_bucket": {
		"versioning.enabled",
		"acl",
		"server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm",
	},
	"aws_iam_role_policy_attachment": {
		"policy_arn",
	},
}

// promoteTerraformResourceAttributes flattens the allowlisted, redaction-safe
// subset of one Terraform state resource's classified Attributes object into
// prefixed scalar node properties (e.g. "instance_type" ->
// "tf_attr_instance_type"). Returns nil when the resource type has no
// allowlist entry, the resource carries no attributes, or every allowlisted
// path is absent, unresolvable, non-scalar, or oversize.
//
// The result is a plain map[string]any of scalar values (string/bool/int64/
// float64) run through canonicalGraphPropertyValue, the same normalization
// the generic entity-metadata writer uses — deliberately reused here instead
// of adding a map case to canonicalGraphPropertyValue itself, which would
// let arbitrary nested config reach a node through the generic path. Every
// value that reaches the caller has already been proven scalar and
// size-capped by this function.
func promoteTerraformResourceAttributes(resourceType string, attributes map[string]any) map[string]any {
	if resourceType == "" || len(attributes) == 0 {
		return nil
	}
	allowlist, ok := terraformAttributePromotionAllowlist[resourceType]
	if !ok || len(allowlist) == 0 {
		return nil
	}

	var result map[string]any
	for _, path := range allowlist {
		raw, found := terraformAttributePathValue(attributes, path)
		if !found {
			continue
		}
		normalized, ok := canonicalGraphPropertyValue(raw)
		if !ok {
			continue
		}
		if terraformAttributePromotionValueTooLarge(normalized) {
			continue
		}
		if result == nil {
			result = make(map[string]any, len(allowlist))
		}
		result[terraformAttributePromotionKeyPrefix+strings.ReplaceAll(path, ".", "_")] = normalized
	}
	return result
}

// terraformAttributePathValue walks a dot-separated attribute path (e.g.
// "server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm")
// through the raw classified Attributes object. Terraform state JSON
// represents a MaxItems=1 nested block as a one-element list of maps rather
// than a bare map, so each segment transparently unwraps a single-element
// []any before descending — the drift package's flat
// ResourceRow.Attributes (map[string]string) performs the equivalent
// unwrap upstream of tfconfigstate.ResourceRow; this walker does the same
// job directly against the raw collector shape since TerraformResource
// promotion reads Attributes before any such flattening happens. Returns
// false when any segment is missing, the value is nil, or a list segment
// does not hold exactly one element (an ambiguous/empty block is treated as
// "no signal", matching the drift classifier's UnknownAttributes handling).
func terraformAttributePathValue(attributes map[string]any, path string) (any, bool) {
	var current any = attributes
	for _, segment := range strings.Split(path, ".") {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := asMap[segment]
		if !ok || value == nil {
			return nil, false
		}
		if list, ok := value.([]any); ok {
			if len(list) != 1 {
				return nil, false
			}
			value = list[0]
		}
		current = value
	}
	if current == nil {
		return nil, false
	}
	return current, true
}

// terraformAttributePromotionValueTooLarge measures a normalized scalar's
// string representation against terraformAttributePromotionValueSizeCapBytes.
// Only strings can plausibly exceed the cap (bool/int64/float64 are always
// small), but the check applies uniformly so a future scalar kind cannot
// silently bypass it.
func terraformAttributePromotionValueTooLarge(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return len(s) > terraformAttributePromotionValueSizeCapBytes
}
