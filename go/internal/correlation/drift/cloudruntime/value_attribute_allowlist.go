// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import "sort"

// valueAttributeAllowlist maps a Terraform resource type -- the STATE-side
// ResourceRow.ResourceType, matching the Terraform provider's own type name
// (e.g. "aws_instance"), never the AWS collector's resource_type string --
// to the deterministic, ordered list of comparable attribute keys the
// value-drift classifier (ClassifyValueDrift) compares between the
// observed AWS cloud resource and the declared Terraform-state resource.
//
// The two sides normalize onto the SAME map key deliberately: the observed
// AWS field name and the declared Terraform attribute name often differ
// (AWS returns "ami_id", Terraform declares "ami"), so the decoders in
// go/internal/storage/postgres normalize both onto this allowlist's key
// before ClassifyValueDrift ever runs a comparison. See
// go/internal/correlation/drift/tfconfigstate.attributeAllowlist for the
// sibling config-vs-state allowlist this mirrors; that allowlist compares
// Terraform config against Terraform state, this one compares the observed
// AWS runtime resource against Terraform state.
//
// Keep this list small and high-signal: every entry is a candidate false
// positive if a provider adds a legitimate reason for the value to differ
// (e.g. AWS resolving a floating tag to a digest). aws_ecs_task_definition
// container images are NOT listed here -- container_definitions requires
// its own bounded, security-reviewed extraction (see
// container_image_extract.go) rather than a scalar attribute-path compare,
// so ClassifyValueDrift consults ContainerImages directly instead of this
// allowlist for that resource type.
//
// RDS/generic engine_version drift is a documented bounded gap: the AWS
// collector does not yet emit an observed engine_version on the aws_resource
// cloud fact for aws_db_instance, so there is no observed-side value to
// compare against Terraform's declared engine_version. See the cloudruntime
// package README for the follow-up issue.
var valueAttributeAllowlist = map[string][]string{
	"aws_instance": {
		"ami",
	},
	"aws_lambda_function": {
		"image_uri",
		"version",
	},
}

// ValueAttributeAllowlistFor returns the ordered allowlist for one Terraform
// resource type. Returns nil when the type has no allowlist entry --
// ClassifyValueDrift cannot fire attribute-level drift for such resources
// today (aws_ecs_task_definition drift is handled separately through
// ContainerImages, not this allowlist).
func ValueAttributeAllowlistFor(resourceType string) []string {
	attrs, ok := valueAttributeAllowlist[resourceType]
	if !ok {
		return nil
	}
	out := make([]string, len(attrs))
	copy(out, attrs)
	return out
}

// ValueAttributeAllowlistResourceTypes returns the sorted list of Terraform
// resource types covered by the value-attribute allowlist. Useful for
// telemetry cardinality assertions and documentation pages that enumerate
// supported value-drift surfaces.
func ValueAttributeAllowlistResourceTypes() []string {
	out := make([]string, 0, len(valueAttributeAllowlist))
	for k := range valueAttributeAllowlist {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
