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
