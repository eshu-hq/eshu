package facts

const (
	// TerraformStateSnapshotFactKind identifies a Terraform state snapshot fact.
	TerraformStateSnapshotFactKind = "terraform_state_snapshot"
	// TerraformStateResourceFactKind identifies one resource instance observed
	// from Terraform state.
	TerraformStateResourceFactKind = "terraform_state_resource"

	// TerraformStateSnapshotSchemaVersion is the first snapshot fact schema.
	TerraformStateSnapshotSchemaVersion = "terraform_state_snapshot.v1"
	// TerraformStateResourceSchemaVersion is the first resource fact schema.
	TerraformStateResourceSchemaVersion = "terraform_state_resource.v1"
)

const (
	// SourceConfidenceUnknown marks facts whose source confidence is not set.
	SourceConfidenceUnknown = "unknown"
	// SourceConfidenceExact marks facts copied from source-authoritative data.
	SourceConfidenceExact = "exact"
	// SourceConfidenceDerived marks facts derived from deterministic evidence.
	SourceConfidenceDerived = "derived"
)
