// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import "strings"

// FindingKind identifies one AWS runtime-vs-declaration finding.
type FindingKind string

const (
	// FindingKindOrphanedCloudResource means AWS reported a resource that has
	// no Terraform-state backing for the same ARN.
	FindingKindOrphanedCloudResource FindingKind = "orphaned_cloud_resource"
	// FindingKindUnmanagedCloudResource means AWS and Terraform state agree on
	// the ARN, but current Terraform config has no backing declaration.
	FindingKindUnmanagedCloudResource FindingKind = "unmanaged_cloud_resource"
	// FindingKindUnknownCloudResource means AWS and state evidence exist, but
	// collector coverage cannot prove whether Terraform config owns the state.
	FindingKindUnknownCloudResource FindingKind = "unknown_cloud_resource"
	// FindingKindAmbiguousCloudResource means multiple deterministic ownership
	// signals conflict for the same AWS stable identity.
	FindingKindAmbiguousCloudResource FindingKind = "ambiguous_cloud_resource"
	// FindingKindImageVersionDrift means cloud, state, and config all agree
	// the resource is Terraform-managed, but at least one allowlisted
	// comparable value (AMI, Lambda image/version, or ECS container image)
	// differs between the AWS-observed resource and the Terraform-declared
	// state (#5453). See ClassifyValueDrift for the comparison rules.
	FindingKindImageVersionDrift FindingKind = "image_version_drift"
)

const (
	// ManagementStatusTerraformStateOnly marks cloud+state resources missing
	// matching Terraform config.
	ManagementStatusTerraformStateOnly = "terraform_state_only"
	// ManagementStatusCloudOnly marks cloud resources without Terraform state
	// or config evidence for the same stable identity.
	ManagementStatusCloudOnly = "cloud_only"
	// ManagementStatusAmbiguous marks conflicting deterministic ownership
	// evidence that must not be promoted to a single owner.
	ManagementStatusAmbiguous = "ambiguous_management"
	// ManagementStatusUnknown marks missing coverage or permissions that keep
	// ownership unproven.
	ManagementStatusUnknown = "unknown_management"
)

// ResourceRow is the normalized view of one cloud, state, or config resource
// keyed by ARN. Tags are raw source tags used as evidence for DSL-side
// normalization; collectors must not pre-normalize them into environment truth.
type ResourceRow struct {
	ARN          string
	ResourceID   string
	ResourceType string
	Address      string
	ScopeID      string
	Tags         map[string]string
	// Attributes carries the bounded, comparable declared/observed scalar
	// values value-drift classification needs (for example "ami" or
	// "image_uri"), keyed by ValueAttributeAllowlistFor(state.ResourceType).
	// Both the cloud-side and state-side decoder normalize their own raw
	// field name onto this shared key (see value_attribute_allowlist.go).
	// Nil for resources with no comparable-attribute coverage.
	Attributes map[string]string
	// ContainerImages carries the bounded, deduplicated, source-ordered set
	// of container image references for ECS task-definition value drift
	// (see container_image_extract.go). Only ever populated for
	// aws_ecs_task_definition state rows and their matching ecs.task_definition
	// cloud rows; nil for every other resource type.
	ContainerImages []string
	// ContainerImagesTruncated reports whether the source container
	// definition carried more distinct images than
	// MaxContainerImagesPerResource. Callers should surface this as an
	// operator-facing warning rather than silently trusting a possibly
	// incomplete ContainerImages set.
	ContainerImagesTruncated bool
}

// DriftedAttribute is one declared/observed value pair ClassifyValueDrift
// found to differ between the Terraform-declared state and the AWS-observed
// cloud resource.
type DriftedAttribute struct {
	// Key is the allowlisted comparable attribute name (e.g. "ami",
	// "image_uri", "version") or containerImageAttributeKey for the ECS
	// container-image comparison.
	Key string
	// Declared is the Terraform-state value for Key.
	Declared string
	// Observed is the AWS cloud value for Key.
	Observed string
}

// containerImageAttributeKey is the synthetic DriftedAttribute.Key used for
// the ECS container-image comparison, which compares ResourceRow.ContainerImages
// sets rather than a single Attributes[key] scalar.
const containerImageAttributeKey = "image"

// containerImageJoinSeparator joins multiple declared or observed container
// images into one evidence-atom-safe string. It is chosen to never collide
// with a container image reference's own valid characters (registry/repo:tag
// or @sha256:digest), which never contain a pipe.
const containerImageJoinSeparator = "|"

// ecsTaskDefinitionResourceType is the Terraform resource type
// ClassifyValueDrift consults ContainerImages for, instead of the scalar
// Attributes allowlist.
const ecsTaskDefinitionResourceType = "aws_ecs_task_definition"

// Classify returns the cloud-runtime finding for one ARN join or an empty
// string when cloud, state, and config converge. The dispatch is exclusive:
// cloud-only resources are orphaned; cloud+state resources without config are
// unmanaged; cloud+state+config resources whose allowlisted comparable
// values differ are image_version_drift. Existence findings (orphaned,
// unmanaged) always take precedence over value drift -- value drift can only
// fire once all three layers are already known to agree the resource is
// Terraform-managed.
func Classify(cloud, state, config *ResourceRow) FindingKind {
	if cloud == nil {
		return ""
	}
	if state == nil {
		return FindingKindOrphanedCloudResource
	}
	if config == nil {
		return FindingKindUnmanagedCloudResource
	}
	if len(ClassifyValueDrift(cloud, state)) > 0 {
		return FindingKindImageVersionDrift
	}
	return ""
}

// ClassifyValueDrift returns the ordered, deterministic list of comparable
// attribute mismatches between the AWS-observed cloud resource and the
// Terraform-declared state resource. It is the sole authority both Classify
// (finding-kind decision) and the candidate builder (declared_/observed_
// evidence atoms) consult, so the two can never disagree about which
// attributes drifted.
//
// A value is compared only when BOTH sides carry a concrete, non-empty
// value for the same allowlisted key; a value missing on either side is
// treated as "no signal" (ambiguous, not drift) and skipped, exactly like
// tfconfigstate.classifyAttributeDrift. Returns nil when cloud or state is
// nil, when state.ResourceType has no ValueAttributeAllowlistFor entry and
// is not the ECS task-definition type, or when every comparable attribute
// is missing or matches.
func ClassifyValueDrift(cloud, state *ResourceRow) []DriftedAttribute {
	if cloud == nil || state == nil {
		return nil
	}
	var drifted []DriftedAttribute
	for _, attr := range ValueAttributeAllowlistFor(state.ResourceType) {
		cloudValue, cloudHas := attrValue(cloud, attr)
		stateValue, stateHas := attrValue(state, attr)
		if !cloudHas || !stateHas {
			continue
		}
		if cloudValue != stateValue {
			drifted = append(drifted, DriftedAttribute{Key: attr, Declared: stateValue, Observed: cloudValue})
		}
	}
	if state.ResourceType == ecsTaskDefinitionResourceType {
		if drift, ambiguous := ClassifyContainerImageDrift(state.ContainerImages, cloud.ContainerImages); drift && !ambiguous {
			drifted = append(drifted, DriftedAttribute{
				Key:      containerImageAttributeKey,
				Declared: joinContainerImages(state.ContainerImages),
				Observed: joinContainerImages(cloud.ContainerImages),
			})
		}
	}
	return drifted
}

// ClassifyContainerImageDrift compares the declared set of ECS
// task-definition container images against the observed set and returns
// whether a deterministic drift signal fires, plus whether the shape is too
// ambiguous to classify at all.
//
// The only deterministic, non-ambiguous cases are:
//   - exactly one observed image that IS a member of the declared set -> no
//     drift (covers both the single-container case and the essential-
//     container membership case for a multi-container task definition).
//   - exactly one observed image that is NOT a member of the declared set
//     -> drift.
//
// Every other shape -- either side empty (missing evidence), or more than
// one observed image (pairing between declared and observed containers is
// not determinable by position or name alone) -- returns ambiguous=true and
// drift=false. This is a deliberate, documented bounded gap: Eshu never
// guesses which declared container an ambiguous observed set corresponds
// to, so a genuinely multi-container-drifted task definition is under-
// reported rather than risking a false positive. See the cloudruntime
// package README for detail.
func ClassifyContainerImageDrift(declared, observed []string) (drift bool, ambiguous bool) {
	if len(declared) == 0 || len(observed) == 0 {
		return false, true
	}
	if len(observed) != 1 {
		return false, true
	}
	for _, image := range declared {
		if image == observed[0] {
			return false, false
		}
	}
	return true, false
}

// attrValue reads one allowlisted attribute off a ResourceRow, reporting
// whether it is present and non-empty.
func attrValue(row *ResourceRow, key string) (string, bool) {
	if row == nil || len(row.Attributes) == 0 {
		return "", false
	}
	value, ok := row.Attributes[key]
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

// joinContainerImages renders a bounded image set as one evidence-atom-safe
// string for the DriftedAttribute.Declared/Observed fields.
func joinContainerImages(images []string) string {
	return strings.Join(images, containerImageJoinSeparator)
}
