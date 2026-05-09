package facts

import "slices"

const (
	// TerraformStateSnapshotFactKind identifies a Terraform state snapshot fact.
	TerraformStateSnapshotFactKind = "terraform_state_snapshot"
	// TerraformStateResourceFactKind identifies one resource instance observed
	// from Terraform state.
	TerraformStateResourceFactKind = "terraform_state_resource"
	// TerraformStateOutputFactKind identifies one output observed from Terraform state.
	TerraformStateOutputFactKind = "terraform_state_output"
	// TerraformStateModuleFactKind identifies one module observed from Terraform state.
	TerraformStateModuleFactKind = "terraform_state_module"
	// TerraformStateProviderBindingFactKind identifies a provider binding observed
	// from Terraform state.
	TerraformStateProviderBindingFactKind = "terraform_state_provider_binding"
	// TerraformStateTagObservationFactKind identifies one resource tag observed
	// from Terraform state.
	TerraformStateTagObservationFactKind = "terraform_state_tag_observation"
	// TerraformStateWarningFactKind identifies a non-fatal Terraform state warning.
	TerraformStateWarningFactKind = "terraform_state_warning"

	// TerraformStateSnapshotSchemaVersion is the first snapshot fact schema.
	TerraformStateSnapshotSchemaVersion = "1.0.0"
	// TerraformStateResourceSchemaVersion is the first resource fact schema.
	TerraformStateResourceSchemaVersion = "1.0.0"
	// TerraformStateOutputSchemaVersion is the first output fact schema.
	TerraformStateOutputSchemaVersion = "1.0.0"
	// TerraformStateModuleSchemaVersion is the first module fact schema.
	TerraformStateModuleSchemaVersion = "1.0.0"
	// TerraformStateProviderBindingSchemaVersion is the first provider binding
	// fact schema.
	TerraformStateProviderBindingSchemaVersion = "1.0.0"
	// TerraformStateTagObservationSchemaVersion is the first tag observation
	// fact schema.
	TerraformStateTagObservationSchemaVersion = "1.0.0"
	// TerraformStateWarningSchemaVersion is the first warning fact schema.
	TerraformStateWarningSchemaVersion = "1.0.0"
)

var terraformStateFactKinds = []string{
	TerraformStateSnapshotFactKind,
	TerraformStateResourceFactKind,
	TerraformStateOutputFactKind,
	TerraformStateModuleFactKind,
	TerraformStateProviderBindingFactKind,
	TerraformStateTagObservationFactKind,
	TerraformStateWarningFactKind,
}

var terraformStateSchemaVersions = map[string]string{
	TerraformStateSnapshotFactKind:        TerraformStateSnapshotSchemaVersion,
	TerraformStateResourceFactKind:        TerraformStateResourceSchemaVersion,
	TerraformStateOutputFactKind:          TerraformStateOutputSchemaVersion,
	TerraformStateModuleFactKind:          TerraformStateModuleSchemaVersion,
	TerraformStateProviderBindingFactKind: TerraformStateProviderBindingSchemaVersion,
	TerraformStateTagObservationFactKind:  TerraformStateTagObservationSchemaVersion,
	TerraformStateWarningFactKind:         TerraformStateWarningSchemaVersion,
}

// TerraformStateFactKinds returns the accepted Terraform state fact kinds in
// their emission order.
func TerraformStateFactKinds() []string {
	return slices.Clone(terraformStateFactKinds)
}

// TerraformStateSchemaVersion returns the schema version for a Terraform state
// fact kind.
func TerraformStateSchemaVersion(factKind string) (string, bool) {
	version, ok := terraformStateSchemaVersions[factKind]
	return version, ok
}
