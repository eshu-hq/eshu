// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

// terraformConfigShapeHint is a read-only structural skeleton attached to a
// ready Terraform import-plan candidate. It names the arguments a human must
// author for the imported resource without emitting any value. Every field is
// derived only from the supported import mapping and the already-exposed import
// identity; the hint never carries secrets, tag values, ARNs beyond the import
// identity, state locators, policy JSON, or any other finding payload.
//
// Refused candidates never receive a hint: the safety gate runs first in
// terraformImportPlanCandidateForFinding, so a finding that fails the gate is
// returned before AttachConfigShapeHint is ever called.
type terraformConfigShapeHint struct {
	// Format identifies the hint as a structural skeleton, not generated config.
	Format string `json:"format"`
	// ResourceAddress mirrors the candidate's suggested_resource_address.
	ResourceAddress string `json:"resource_address"`
	// ProviderAlias mirrors the candidate provider hint alias when one exists.
	ProviderAlias string `json:"provider_alias,omitempty"`
	// RequiredArguments lists the argument names Terraform requires for the
	// resource type. Names only; the operator supplies every value.
	RequiredArguments []string `json:"required_arguments"`
	// NotableOptionalArguments lists commonly authored optional argument names
	// for the resource type. Names only; never populated from finding data.
	NotableOptionalArguments []string `json:"notable_optional_arguments,omitempty"`
	// OmittedSensitiveArguments names arguments deliberately left for manual,
	// out-of-band authoring because they are sensitive, data-plane, or policy
	// shaped. Names only; their values are never derived or emitted.
	OmittedSensitiveArguments []string `json:"omitted_sensitive_arguments,omitempty"`
	// HCLSkeleton is a commented resource block listing argument names with
	// placeholders. It contains no real values.
	HCLSkeleton string `json:"hcl_skeleton"`
	// ManualFillWarnings explains, in operator language, that the skeleton is
	// guidance the human must complete by hand.
	ManualFillWarnings []string `json:"manual_fill_warnings"`
}

// configShapeHintPlaceholder is the literal placeholder emitted for every
// argument value in a skeleton. It is intentionally not a valid value so a
// skeleton can never be mistaken for ready-to-apply configuration and so no
// finding-derived value can leak through this path.
const configShapeHintPlaceholder = "<FILL_IN>"

// terraformConfigShapeSchema names the structural arguments for one supported
// Terraform resource type. All slices hold argument names only and never any
// value. The catalog is intentionally small and well-known per the AWS provider
// resource schema; it is keyed off the same mapping table as import candidates.
type terraformConfigShapeSchema struct {
	Required            []string
	NotableOptional     []string
	OmittedFillManually []string
}

// terraformConfigShapeSchemas maps each supported Terraform resource type to its
// structural argument-name catalog. Only resource types that already have a safe
// deterministic import mapping appear here; an unmapped type yields no hint.
//
// The argument names follow the official Terraform AWS provider resource schema.
// Sensitive, data-plane, and policy-shaped arguments are listed by name under
// OmittedFillManually so the operator is told to author them out of band rather
// than have Eshu synthesize a value.
var terraformConfigShapeSchemas = map[string]terraformConfigShapeSchema{
	"aws_s3_bucket": {
		Required:            []string{"bucket"},
		NotableOptional:     []string{"force_destroy", "tags"},
		OmittedFillManually: []string{"policy"},
	},
	"aws_lambda_function": {
		Required:            []string{"function_name", "role"},
		NotableOptional:     []string{"handler", "runtime", "memory_size", "timeout", "tags"},
		OmittedFillManually: []string{"environment", "filename", "image_uri", "s3_bucket", "s3_key"},
	},
	"aws_sns_topic": {
		Required:            []string{"name"},
		NotableOptional:     []string{"display_name", "fifo_topic", "tags"},
		OmittedFillManually: []string{"policy", "delivery_policy", "kms_master_key_id"},
	},
	"aws_dynamodb_table": {
		Required:            []string{"name", "hash_key", "billing_mode"},
		NotableOptional:     []string{"range_key", "attribute", "tags"},
		OmittedFillManually: []string{"server_side_encryption"},
	},
	"aws_ecr_repository": {
		Required:            []string{"name"},
		NotableOptional:     []string{"image_tag_mutability", "tags"},
		OmittedFillManually: []string{"encryption_configuration"},
	},
	"aws_cloudwatch_log_group": {
		Required:            []string{"name"},
		NotableOptional:     []string{"retention_in_days", "tags"},
		OmittedFillManually: []string{"kms_key_id"},
	},
}

// configShapeHintForResourceType builds the structural hint for a ready
// candidate of the given Terraform resource type. It returns false when the
// resource type has no catalog entry so the caller attaches no hint. The hint
// carries argument names and placeholders only; resourceAddress and
// providerAlias are already-exposed identity fields, never finding payload.
func configShapeHintForResourceType(
	resourceType string,
	resourceAddress string,
	providerAlias string,
) (terraformConfigShapeHint, bool) {
	schema, ok := terraformConfigShapeSchemas[resourceType]
	if !ok {
		return terraformConfigShapeHint{}, false
	}
	hint := terraformConfigShapeHint{
		Format:                    "terraform_resource_skeleton",
		ResourceAddress:           resourceAddress,
		ProviderAlias:             providerAlias,
		RequiredArguments:         appendSorted(schema.Required),
		NotableOptionalArguments:  appendSorted(schema.NotableOptional),
		OmittedSensitiveArguments: appendSorted(schema.OmittedFillManually),
		ManualFillWarnings: []string{
			"structural skeleton only: every value is a placeholder the operator must author",
			"Eshu does not write, apply, or run Terraform and emits no resource values",
			"sensitive, policy, and data-plane arguments are named for manual authoring only",
		},
	}
	hint.HCLSkeleton = configShapeHintSkeleton(resourceType, resourceAddress, schema)
	return hint, true
}

// appendSorted returns a sorted copy of names, or nil when empty, so the hint is
// deterministic and never shares the caller's backing array.
func appendSorted(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	copy(out, names)
	sort.Strings(out)
	return out
}

// configShapeHintSkeleton renders the commented resource block. Required
// arguments get a placeholder assignment; notable optional and omitted-sensitive
// arguments are emitted only as comments. No value from the finding is ever
// interpolated; resourceType and the address label are structural identity.
func configShapeHintSkeleton(
	resourceType string,
	resourceAddress string,
	schema terraformConfigShapeSchema,
) string {
	label := resourceType
	if idx := strings.LastIndex(resourceAddress, "."); idx >= 0 && idx+1 < len(resourceAddress) {
		label = resourceAddress[idx+1:]
	}
	var b strings.Builder
	b.WriteString("# read-only config-shape hint: author every value by hand; Eshu emits no values\n")
	fmt.Fprintf(&b, "resource %q %q {\n", resourceType, label)
	for _, name := range appendSorted(schema.Required) {
		fmt.Fprintf(&b, "  %s = %q # required\n", name, configShapeHintPlaceholder)
	}
	for _, name := range appendSorted(schema.NotableOptional) {
		fmt.Fprintf(&b, "  # %s = %s # optional\n", name, configShapeHintPlaceholder)
	}
	for _, name := range appendSorted(schema.OmittedFillManually) {
		fmt.Fprintf(&b, "  # %s = %s # author manually; not generated by Eshu\n", name, configShapeHintPlaceholder)
	}
	b.WriteString("}\n")
	return b.String()
}
