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
//
// #5446 extends this allowlist with deployment-topology-shaped scalars for
// aws_ecs_task_definition, aws_ecs_service, and three more attributes on the
// two families already present (aws_lambda_function, aws_db_instance), plus
// three new resource types (aws_rds_cluster, aws_lb,
// aws_elasticache_replication_group). Three attributes considered for this
// extension were deliberately excluded, each for a different reason:
//
//   - aws_ecs_task_definition.container_definitions is a JSON document that
//     can embed container environment variables and secret ARNs/ValueFrom
//     references — the exact policy-document risk class this file's own
//     top-of-block comment already excludes aws_iam_role.assume_role_policy
//     and aws_iam_policy.policy for (multi-KB free-form JSON, not a bounded
//     scalar).
//   - aws_lambda_function.qualified_arn is redundant with the function's
//     unqualified arn/name identity already resolvable from the node's other
//     properties and is a version-specific derived value, not additional
//     deployment-topology signal worth a dedicated promoted property.
//   - aws_elasticache_replication_group.cache_nodes is a list of per-node
//     blocks (arbitrary cardinality, not a Terraform MaxItems=1 block this
//     file's dot-path walker can unwrap), so no single scalar path exists to
//     promote; primary_endpoint_address is the single-valued cluster-mode-
//     disabled connection endpoint this allowlist promotes instead.
var terraformAttributePromotionAllowlist = map[string][]string{
	"aws_instance": {
		"instance_type",
		"ami",
	},
	"aws_db_instance": {
		"engine",
		"engine_version",
		"instance_class",
		"endpoint",
	},
	"aws_lambda_function": {
		"runtime",
		"handler",
		"memory_size",
		"timeout",
		"version",
		"image_uri",
	},
	"aws_s3_bucket": {
		"versioning.enabled",
		"acl",
		"server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm",
	},
	"aws_iam_role_policy_attachment": {
		"policy_arn",
	},
	"aws_ecs_task_definition": {
		"family",
		"revision",
	},
	"aws_ecs_service": {
		"task_definition",
	},
	"aws_rds_cluster": {
		"endpoint",
		"reader_endpoint",
	},
	"aws_lb": {
		"dns_name",
	},
	"aws_elasticache_replication_group": {
		"primary_endpoint_address",
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
// float64) run through terraformAttributeScalarValue, which gates
// canonicalGraphPropertyValue (the same normalization the generic
// entity-metadata writer uses, reused here instead of adding a map case to
// canonicalGraphPropertyValue itself — that would let arbitrary nested
// config reach a node through the generic path) to reject the []string/
// []any list shapes that function would otherwise also accept. Every value
// that reaches the caller has been proven scalar (not just proven present)
// and size-capped by this function.
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
		normalized, ok := terraformAttributeScalarValue(raw)
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

// terraformAttributePromotionKeysForType returns every tf_attr_* node
// property key promoteTerraformResourceAttributes can possibly write for
// resourceType, in allowlist declaration order. Returns nil for a
// resourceType with no allowlist entry.
//
// This is the REMOVE-clause source of truth for the TerraformResource
// writer (#5441 review round 8, P1-a): `r += row.attrs` is an additive
// map-merge, so a key absent from the current row's promoted attrs (removed
// from state, now malformed, or newly oversize under the 512-byte cap)
// leaves whatever value a PRIOR write left on the node -- stale graph truth
// that no longer matches the source state. Deriving the REMOVE key set from
// this same allowlist (rather than hand-maintaining a parallel list) is
// deliberate: the two can never drift out of sync.
func terraformAttributePromotionKeysForType(resourceType string) []string {
	allowlist, ok := terraformAttributePromotionAllowlist[resourceType]
	if !ok || len(allowlist) == 0 {
		return nil
	}
	keys := make([]string, 0, len(allowlist))
	for _, path := range allowlist {
		keys = append(keys, terraformAttributePromotionKeyPrefix+strings.ReplaceAll(path, ".", "_"))
	}
	return keys
}

// terraformAttributeScalarValue gates canonicalGraphPropertyValue to true
// scalars only (string/bool/int-family/float), rejecting the []string/[]any
// list shapes canonicalGraphPropertyValue accepts for the generic
// entity-metadata path (P2 finding F4). A promoted Terraform attribute must
// always be a plain scalar node property, never a list: an allowlisted
// attribute whose raw walked value is unexpectedly a list (a malformed
// state payload, or a future Terraform provider schema change that turns a
// scalar attribute into a list) is dropped rather than promoted, keeping
// this function's "proven scalar" contract true regardless of what
// canonicalGraphPropertyValue itself would otherwise accept.
func terraformAttributeScalarValue(raw any) (any, bool) {
	switch raw.(type) {
	case []string, []any:
		return nil, false
	default:
		return canonicalGraphPropertyValue(raw)
	}
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
