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
	// FindingKindValueComparisonInconclusive means cloud, state, and config all
	// agree the resource is Terraform-managed and the resource type IS covered
	// by value drift, but NOT ONE comparable value could actually be compared:
	// every allowlisted attribute was missing on at least one side, or the ECS
	// container images were unreadable or absent on a side (#5837).
	//
	// It fires only for a gap that can CHANGE between passes. A permanently
	// unpairable shape -- an ECS task definition whose observed image set has
	// more than one member -- is not coverage this outcome speaks for: no pass
	// produces a finding there, so no pass can lose one, and a finding on every
	// healthy multi-container task definition would be pure noise.
	//
	// This is deliberately a finding rather than silence. Classify is
	// degradation-safe on the existence axis — a missing state or config row
	// still writes a durable row — but returning "" here made it UNSAFE on the
	// value axis: the ARN dropped out of the candidate set, the
	// generation-authoritative retire's keep-set emptied, and a still-true
	// image_version_drift finding was DELETED rather than superseded. Emitting
	// explicit uncertainty keeps a row in the keep-set for a degraded ARN, and
	// the finding self-heals into image_version_drift (or into nothing, on a
	// genuine convergence) once the upstream evidence returns. See the
	// cloudruntime package README for the bounded residual this does not close.
	FindingKindValueComparisonInconclusive FindingKind = "value_comparison_inconclusive"
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
	// ContainerImagesDegraded reports that this side carried a
	// container-definitions value that could not be READ -- a redaction-marker
	// object where a JSON string was expected, or a string that failed to
	// parse -- as opposed to carrying none at all. Both leave ContainerImages
	// empty, so only this flag separates "the task definition declares no
	// containers" from "the evidence exists and we could not use it" (#5837).
	// See container_image_extract.go.
	ContainerImagesDegraded bool
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
//
// An empty return means CONVERGENCE, and that is a load-bearing distinction
// downstream: the caller drops the ARN from the candidate set, so the
// generation-authoritative retire deletes whatever finding the ARN previously
// held. A resource type value drift covers whose comparisons could not be made
// at all therefore does NOT converge -- it returns
// FindingKindValueComparisonInconclusive, so a degraded ARN keeps a durable row
// and the retire supersedes rather than destroys (#5837).
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
	comparison := ClassifyValueComparison(cloud, state)
	if len(comparison.Drifted) > 0 {
		return FindingKindImageVersionDrift
	}
	if comparison.Inconclusive() {
		return FindingKindValueComparisonInconclusive
	}
	return ""
}

// ValueComparison is the full outcome of comparing one ARN's allowlisted
// values, not only the mismatches. Classify needs the difference between "every
// comparison ran and agreed" (convergence) and "no comparison could run at all"
// (inconclusive), and the drifted list alone is empty in both cases.
// The two totals are counts rather than key lists on purpose. Classify runs
// once per ARN on the reducer's hot path and only needs the arithmetic, so
// materializing two more slices there cost 2 allocations per ARN for nothing
// (measured: 50 -> 92 ns/op, 1 -> 3 allocs/op on BenchmarkClassifyWithValueDrift
// before this shape). Uncomparable IS a key list because the candidate builder
// names those attributes in the finding's evidence -- and it only ever allocates
// on the degraded path, which is the path that produces a finding anyway.
type ValueComparison struct {
	// Drifted holds the comparisons that ran and disagreed, in allowlist order.
	Drifted []DriftedAttribute
	// Comparable counts every key this resource type is covered for on THIS
	// pair: its ValueAttributeAllowlistFor entries, plus the ECS
	// container-image comparison unless that comparison is permanently
	// unpairable for the shape (both sides carry images and more than one is
	// observed), which is not coverage at all. Zero means value drift reached
	// no comparable key here.
	Comparable int
	// Compared counts the keys where BOTH sides carried a concrete value (or,
	// for ECS, where the container-image shape was unambiguous) and a
	// comparison was actually performed.
	Compared int
	// Uncomparable names the keys whose comparison could not be made because a
	// side was missing, blank, or ambiguous, in allowlist order. Its length is
	// always Comparable minus Compared.
	Uncomparable []string
}

// Inconclusive reports whether value drift covers this resource type but not a
// single comparison could be made. It is the condition that must never be
// mistaken for convergence: silence here would drop the ARN from the candidate
// set and let the retire delete a still-true finding (#5837).
func (c ValueComparison) Inconclusive() bool {
	return c.Comparable > 0 && c.Compared == 0
}

// ClassifyValueComparison is the sole authority on which comparable values were
// compared, which drifted, and which could not be read. Classify (finding-kind
// decision), ClassifyValueDrift (the drifted subset), and the candidate builder
// (declared_/observed_ and coverage-gap evidence atoms) all derive from this one
// function, so they can never disagree.
//
// A value is compared only when BOTH sides carry a concrete, non-empty value
// for the same allowlisted key; a value missing on either side is "no signal",
// exactly like tfconfigstate.classifyAttributeDrift -- but it is now RECORDED as
// no signal (Uncomparable) rather than merely skipped. Returns a zero value when
// cloud or state is nil.
func ClassifyValueComparison(cloud, state *ResourceRow) ValueComparison {
	var out ValueComparison
	if cloud == nil || state == nil {
		return out
	}
	for _, attr := range ValueAttributeAllowlistFor(state.ResourceType) {
		out.Comparable++
		cloudValue, cloudHas := attrValue(cloud, attr)
		stateValue, stateHas := attrValue(state, attr)
		if !cloudHas || !stateHas {
			out.Uncomparable = append(out.Uncomparable, attr)
			continue
		}
		out.Compared++
		if cloudValue != stateValue {
			out.Drifted = append(out.Drifted, DriftedAttribute{Key: attr, Declared: stateValue, Observed: cloudValue})
		}
	}
	if state.ResourceType == ecsTaskDefinitionResourceType {
		drift, ambiguous := ClassifyContainerImageDrift(state.ContainerImages, cloud.ContainerImages)
		switch {
		case !ambiguous:
			out.Comparable++
			out.Compared++
			if drift {
				out.Drifted = append(out.Drifted, DriftedAttribute{
					Key:      containerImageAttributeKey,
					Declared: joinContainerImages(state.ContainerImages),
					Observed: joinContainerImages(cloud.ContainerImages),
				})
			}
		case len(state.ContainerImages) == 0 || len(cloud.ContainerImages) == 0:
			// A side carried no usable images: either the evidence was
			// unreadable (ContainerImagesDegraded) or collector coverage never
			// produced it. Both are TRANSIENT -- a healthier pass on the same
			// ARN can carry images and reach a real verdict -- so this must
			// count as covered-but-uncomparable, or the retire deletes the
			// verdict that pass wrote.
			out.Comparable++
			out.Uncomparable = append(out.Uncomparable, containerImageAttributeKey)
		default:
			// Both sides carry images and the only obstacle is that more than
			// one observed image cannot be paired against the declared set.
			// That is a PERMANENT property of the task definition's shape, not
			// of this pass's evidence: it is identical on every pass, so no
			// pass ever produced a finding here and none can lose one. Leaving
			// Comparable at zero says value drift does not cover this shape --
			// which is the pre-existing, documented under-reporting gap -- and
			// keeps the inconclusive outcome off every healthy multi-container
			// task definition (#5837).
		}
	}
	return out
}

// ClassifyValueDrift returns the ordered, deterministic list of comparable
// attribute mismatches between the AWS-observed cloud resource and the
// Terraform-declared state resource. It is the drifted projection of
// ClassifyValueComparison, kept as its own function because the candidate
// builder's declared_/observed_ evidence atoms need exactly that subset.
//
// Returns nil when cloud or state is nil, when state.ResourceType has no
// ValueAttributeAllowlistFor entry and is not the ECS task-definition type, or
// when every comparable attribute is missing or matches. Callers that must tell
// "matched" apart from "could not be compared" MUST use
// ClassifyValueComparison: this function is empty for both.
func ClassifyValueDrift(cloud, state *ResourceRow) []DriftedAttribute {
	return ClassifyValueComparison(cloud, state).Drifted
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
