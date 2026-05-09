package facts

const (
	// TerraformStateSnapshotFactKind identifies a Terraform state snapshot fact.
	TerraformStateSnapshotFactKind = "terraform_state_snapshot"
	// TerraformStateResourceFactKind identifies one resource instance observed
	// from Terraform state.
	TerraformStateResourceFactKind = "terraform_state_resource"

	// TerraformStateSnapshotSchemaVersion is the first snapshot fact schema.
	TerraformStateSnapshotSchemaVersion = "1.0.0"
	// TerraformStateResourceSchemaVersion is the first resource fact schema.
	TerraformStateResourceSchemaVersion = "1.0.0"
)

const (
	// SourceConfidenceUnknown marks facts whose source confidence is not set.
	SourceConfidenceUnknown = "unknown"
	// SourceConfidenceExact marks facts copied from source-authoritative data.
	SourceConfidenceExact = "exact"
	// SourceConfidenceDerived marks facts derived from deterministic evidence.
	SourceConfidenceDerived = "derived"
)
