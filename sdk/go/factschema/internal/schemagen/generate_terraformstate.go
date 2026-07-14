// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
)

// TerraformStateSnapshotSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_snapshot" payload.
const TerraformStateSnapshotSchemaID = schemaBaseID + "terraformstate/v1/snapshot.schema.json"

// TerraformStateSnapshotSchema returns the JSON Schema bytes for
// tfstatev1.Snapshot.
func TerraformStateSnapshotSchema() ([]byte, error) {
	return reflectSchema(TerraformStateSnapshotSchemaID, "Eshu terraform_state_snapshot Payload (schema version 1)", &tfstatev1.Snapshot{})
}

// TerraformStateResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_resource" payload.
const TerraformStateResourceSchemaID = schemaBaseID + "terraformstate/v1/resource.schema.json"

// TerraformStateResourceSchema returns the JSON Schema bytes for
// tfstatev1.Resource.
func TerraformStateResourceSchema() ([]byte, error) {
	return reflectSchema(TerraformStateResourceSchemaID, "Eshu terraform_state_resource Payload (schema version 1)", &tfstatev1.Resource{})
}

// TerraformStateModuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_module" payload.
const TerraformStateModuleSchemaID = schemaBaseID + "terraformstate/v1/module.schema.json"

// TerraformStateModuleSchema returns the JSON Schema bytes for
// tfstatev1.Module.
func TerraformStateModuleSchema() ([]byte, error) {
	return reflectSchema(TerraformStateModuleSchemaID, "Eshu terraform_state_module Payload (schema version 1)", &tfstatev1.Module{})
}

// TerraformStateOutputSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_output" payload.
const TerraformStateOutputSchemaID = schemaBaseID + "terraformstate/v1/output.schema.json"

// TerraformStateOutputSchema returns the JSON Schema bytes for
// tfstatev1.Output.
func TerraformStateOutputSchema() ([]byte, error) {
	return reflectSchema(TerraformStateOutputSchemaID, "Eshu terraform_state_output Payload (schema version 1)", &tfstatev1.Output{})
}

// TerraformStateTagObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_tag_observation" payload.
const TerraformStateTagObservationSchemaID = schemaBaseID + "terraformstate/v1/tag_observation.schema.json"

// TerraformStateTagObservationSchema returns the JSON Schema bytes for
// tfstatev1.TagObservation.
func TerraformStateTagObservationSchema() ([]byte, error) {
	return reflectSchema(TerraformStateTagObservationSchemaID, "Eshu terraform_state_tag_observation Payload (schema version 1)", &tfstatev1.TagObservation{})
}

// TerraformStateCandidateSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_candidate" payload.
const TerraformStateCandidateSchemaID = schemaBaseID + "terraformstate/v1/candidate.schema.json"

// TerraformStateCandidateSchema returns the JSON Schema bytes for
// tfstatev1.Candidate.
func TerraformStateCandidateSchema() ([]byte, error) {
	return reflectSchema(TerraformStateCandidateSchemaID, "Eshu terraform_state_candidate Payload (schema version 1)", &tfstatev1.Candidate{})
}

// TerraformStateProviderBindingSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "terraform_state_provider_binding" payload.
const TerraformStateProviderBindingSchemaID = schemaBaseID + "terraformstate/v1/provider_binding.schema.json"

// TerraformStateProviderBindingSchema returns the JSON Schema bytes for
// tfstatev1.ProviderBinding.
func TerraformStateProviderBindingSchema() ([]byte, error) {
	return reflectSchema(TerraformStateProviderBindingSchemaID, "Eshu terraform_state_provider_binding Payload (schema version 1)", &tfstatev1.ProviderBinding{})
}

// TerraformStateWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_warning" payload.
const TerraformStateWarningSchemaID = schemaBaseID + "terraformstate/v1/warning.schema.json"

// TerraformStateWarningSchema returns the JSON Schema bytes for
// tfstatev1.Warning.
func TerraformStateWarningSchema() ([]byte, error) {
	return reflectSchema(TerraformStateWarningSchemaID, "Eshu terraform_state_warning Payload (schema version 1)", &tfstatev1.Warning{})
}
