package tfconfigstate

import "sort"

// attributeAllowlist maps Terraform resource type to the deterministic,
// ordered list of attribute paths the attribute_drift classifier compares.
// Keep this list small and high-signal — every entry expands the false-positive
// surface. The Phase 1 seed is operator-meaningful drift only:
//
//   - bucket-level configuration (versioning, ACL, encryption)
//   - compute identity (instance_type, runtime, handler, memory_size)
//   - database engine identity (engine, engine_version)
//   - IAM policy identity (assume_role_policy, policy_arn)
//
// Promotion to a versioned data file lives in a follow-up ADR (design doc §9
// open question Q5).
var attributeAllowlist = map[string][]string{
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

// AllowlistFor returns the ordered allowlist for one resource type. Returns
// nil when the type has no allowlist entry — attribute_drift cannot fire for
// such resources in v1. Use AllowlistResourceTypes to enumerate covered types.
func AllowlistFor(resourceType string) []string {
	attrs, ok := attributeAllowlist[resourceType]
	if !ok {
		return nil
	}
	out := make([]string, len(attrs))
	copy(out, attrs)
	return out
}

// AllowlistResourceTypes returns the sorted list of resource types covered by
// the attribute allowlist. Useful for telemetry cardinality assertions and
// documentation pages that enumerate supported drift surfaces.
func AllowlistResourceTypes() []string {
	out := make([]string, 0, len(attributeAllowlist))
	for k := range attributeAllowlist {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
