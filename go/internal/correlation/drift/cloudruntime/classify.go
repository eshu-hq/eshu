package cloudruntime

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
}

// Classify returns the cloud-runtime finding for one ARN join or an empty
// string when cloud, state, and config converge. The dispatch is exclusive:
// cloud-only resources are orphaned; cloud+state resources without config are
// unmanaged.
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
	return ""
}
