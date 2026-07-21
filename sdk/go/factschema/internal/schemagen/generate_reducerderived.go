// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// ReducerSupplyChainImpactFindingSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "reducer_supply_chain_impact_finding" payload.
const ReducerSupplyChainImpactFindingSchemaID = schemaBaseID + "reducerderived/v1/supply_chain_impact_finding.schema.json"

// ReducerSupplyChainImpactFindingSchema returns the JSON Schema bytes for
// reducerderivedv1.SupplyChainImpactFinding.
func ReducerSupplyChainImpactFindingSchema() ([]byte, error) {
	return reflectSchema(ReducerSupplyChainImpactFindingSchemaID, "Eshu reducer_supply_chain_impact_finding Payload (schema version 1)", &reducerderivedv1.SupplyChainImpactFinding{})
}

// ReducerAWSCloudRuntimeDriftFindingSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "reducer_aws_cloud_runtime_drift_finding" payload.
const ReducerAWSCloudRuntimeDriftFindingSchemaID = schemaBaseID + "reducerderived/v1/aws_cloud_runtime_drift_finding.schema.json"

// ReducerAWSCloudRuntimeDriftFindingSchema returns the JSON Schema bytes for
// reducerderivedv1.AWSCloudRuntimeDriftFinding.
func ReducerAWSCloudRuntimeDriftFindingSchema() ([]byte, error) {
	return reflectSchema(ReducerAWSCloudRuntimeDriftFindingSchemaID, "Eshu reducer_aws_cloud_runtime_drift_finding Payload (schema version 1)", &reducerderivedv1.AWSCloudRuntimeDriftFinding{})
}

// ReducerTerraformConfigStateDriftFindingSchemaID is the checked-in JSON
// Schema $id for the schema-version-1
// "reducer_terraform_config_state_drift_finding" payload.
const ReducerTerraformConfigStateDriftFindingSchemaID = schemaBaseID + "reducerderived/v1/terraform_config_state_drift_finding.schema.json"

// ReducerTerraformConfigStateDriftFindingSchema returns the JSON Schema bytes
// for reducerderivedv1.TerraformConfigStateDriftFinding.
func ReducerTerraformConfigStateDriftFindingSchema() ([]byte, error) {
	return reflectSchema(ReducerTerraformConfigStateDriftFindingSchemaID, "Eshu reducer_terraform_config_state_drift_finding Payload (schema version 1)", &reducerderivedv1.TerraformConfigStateDriftFinding{})
}

// ReducerMultiCloudRuntimeDriftFindingSchemaID is the checked-in JSON Schema
// $id for the schema-version-1
// "reducer_multi_cloud_runtime_drift_finding" payload.
const ReducerMultiCloudRuntimeDriftFindingSchemaID = schemaBaseID + "reducerderived/v1/multi_cloud_runtime_drift_finding.schema.json"

// ReducerMultiCloudRuntimeDriftFindingSchema returns the JSON Schema bytes for
// reducerderivedv1.MultiCloudRuntimeDriftFinding.
func ReducerMultiCloudRuntimeDriftFindingSchema() ([]byte, error) {
	return reflectSchema(ReducerMultiCloudRuntimeDriftFindingSchemaID, "Eshu reducer_multi_cloud_runtime_drift_finding Payload (schema version 1)", &reducerderivedv1.MultiCloudRuntimeDriftFinding{})
}

// ReducerPackageOwnershipCorrelationSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "reducer_package_ownership_correlation" payload.
const ReducerPackageOwnershipCorrelationSchemaID = schemaBaseID + "reducerderived/v1/package_ownership_correlation.schema.json"

// ReducerPackageOwnershipCorrelationSchema returns the JSON Schema bytes for
// reducerderivedv1.PackageOwnershipCorrelation.
func ReducerPackageOwnershipCorrelationSchema() ([]byte, error) {
	return reflectSchema(ReducerPackageOwnershipCorrelationSchemaID, "Eshu reducer_package_ownership_correlation Payload (schema version 1)", &reducerderivedv1.PackageOwnershipCorrelation{})
}

// ReducerPackageConsumptionCorrelationSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "reducer_package_consumption_correlation"
// payload.
const ReducerPackageConsumptionCorrelationSchemaID = schemaBaseID + "reducerderived/v1/package_consumption_correlation.schema.json"

// ReducerPackageConsumptionCorrelationSchema returns the JSON Schema bytes for
// reducerderivedv1.PackageConsumptionCorrelation.
func ReducerPackageConsumptionCorrelationSchema() ([]byte, error) {
	return reflectSchema(ReducerPackageConsumptionCorrelationSchemaID, "Eshu reducer_package_consumption_correlation Payload (schema version 1)", &reducerderivedv1.PackageConsumptionCorrelation{})
}

// ReducerPackagePublicationCorrelationSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "reducer_package_publication_correlation"
// payload.
const ReducerPackagePublicationCorrelationSchemaID = schemaBaseID + "reducerderived/v1/package_publication_correlation.schema.json"

// ReducerPackagePublicationCorrelationSchema returns the JSON Schema bytes for
// reducerderivedv1.PackagePublicationCorrelation.
func ReducerPackagePublicationCorrelationSchema() ([]byte, error) {
	return reflectSchema(ReducerPackagePublicationCorrelationSchemaID, "Eshu reducer_package_publication_correlation Payload (schema version 1)", &reducerderivedv1.PackagePublicationCorrelation{})
}
