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
