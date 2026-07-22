// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

const (
	// FactKindReducerSupplyChainImpactFinding identifies one reducer-owned
	// supply-chain impact finding.
	FactKindReducerSupplyChainImpactFinding = "reducer_supply_chain_impact_finding"
	// FactKindReducerAWSCloudRuntimeDriftFinding identifies one AWS runtime
	// drift finding written by the reducer.
	FactKindReducerAWSCloudRuntimeDriftFinding = "reducer_aws_cloud_runtime_drift_finding"
	// FactKindReducerMultiCloudRuntimeDriftFinding identifies one provider-neutral
	// runtime drift finding written by the reducer.
	FactKindReducerMultiCloudRuntimeDriftFinding = "reducer_multi_cloud_runtime_drift_finding"
	// FactKindReducerTerraformConfigStateDriftFinding identifies one Terraform
	// config-vs-state drift finding written by the reducer (issue #5442).
	FactKindReducerTerraformConfigStateDriftFinding = "reducer_terraform_config_state_drift_finding"
	// FactKindReducerPackageOwnershipCorrelation identifies one reducer-owned
	// package source-hint ownership decision.
	FactKindReducerPackageOwnershipCorrelation = "reducer_package_ownership_correlation"
	// FactKindReducerPackageConsumptionCorrelation identifies one reducer-owned
	// package consumption decision.
	FactKindReducerPackageConsumptionCorrelation = "reducer_package_consumption_correlation"
	// FactKindReducerPackagePublicationCorrelation identifies one reducer-owned
	// package publication decision.
	FactKindReducerPackagePublicationCorrelation = "reducer_package_publication_correlation"
)

// DecodeReducerSupplyChainImpactFinding decodes env.Payload into the latest
// reducerderivedv1.SupplyChainImpactFinding struct.
func DecodeReducerSupplyChainImpactFinding(env Envelope) (reducerderivedv1.SupplyChainImpactFinding, error) {
	return decodeLatestMajor[reducerderivedv1.SupplyChainImpactFinding](FactKindReducerSupplyChainImpactFinding, env)
}

// EncodeReducerSupplyChainImpactFinding marshals a typed supply-chain impact
// finding into the map payload shape an Envelope carries.
func EncodeReducerSupplyChainImpactFinding(finding reducerderivedv1.SupplyChainImpactFinding) (map[string]any, error) {
	return encodeDirectPayload(finding)
}

// DecodeReducerAWSCloudRuntimeDriftFinding decodes env.Payload into the latest
// reducerderivedv1.AWSCloudRuntimeDriftFinding struct.
func DecodeReducerAWSCloudRuntimeDriftFinding(env Envelope) (reducerderivedv1.AWSCloudRuntimeDriftFinding, error) {
	return decodeLatestMajor[reducerderivedv1.AWSCloudRuntimeDriftFinding](FactKindReducerAWSCloudRuntimeDriftFinding, env)
}

// EncodeReducerAWSCloudRuntimeDriftFinding marshals a typed AWS runtime drift
// finding into the map payload shape an Envelope carries.
func EncodeReducerAWSCloudRuntimeDriftFinding(finding reducerderivedv1.AWSCloudRuntimeDriftFinding) (map[string]any, error) {
	return encodeDirectPayload(finding)
}

// DecodeReducerMultiCloudRuntimeDriftFinding decodes env.Payload into the
// latest reducerderivedv1.MultiCloudRuntimeDriftFinding struct.
func DecodeReducerMultiCloudRuntimeDriftFinding(env Envelope) (reducerderivedv1.MultiCloudRuntimeDriftFinding, error) {
	return decodeLatestMajor[reducerderivedv1.MultiCloudRuntimeDriftFinding](FactKindReducerMultiCloudRuntimeDriftFinding, env)
}

// EncodeReducerMultiCloudRuntimeDriftFinding marshals a typed provider-neutral
// runtime drift finding into the map payload shape an Envelope carries.
func EncodeReducerMultiCloudRuntimeDriftFinding(finding reducerderivedv1.MultiCloudRuntimeDriftFinding) (map[string]any, error) {
	return encodeDirectPayload(finding)
}

// DecodeReducerTerraformConfigStateDriftFinding decodes env.Payload into the
// latest reducerderivedv1.TerraformConfigStateDriftFinding struct.
func DecodeReducerTerraformConfigStateDriftFinding(env Envelope) (reducerderivedv1.TerraformConfigStateDriftFinding, error) {
	return decodeLatestMajor[reducerderivedv1.TerraformConfigStateDriftFinding](FactKindReducerTerraformConfigStateDriftFinding, env)
}

// EncodeReducerTerraformConfigStateDriftFinding marshals a typed Terraform
// config-vs-state drift finding into the map payload shape an Envelope carries.
func EncodeReducerTerraformConfigStateDriftFinding(finding reducerderivedv1.TerraformConfigStateDriftFinding) (map[string]any, error) {
	return encodeDirectPayload(finding)
}

// DecodeReducerPackageOwnershipCorrelation decodes env.Payload into the latest
// reducerderivedv1.PackageOwnershipCorrelation struct.
func DecodeReducerPackageOwnershipCorrelation(env Envelope) (reducerderivedv1.PackageOwnershipCorrelation, error) {
	return decodeLatestMajor[reducerderivedv1.PackageOwnershipCorrelation](FactKindReducerPackageOwnershipCorrelation, env)
}

// EncodeReducerPackageOwnershipCorrelation marshals a typed package ownership
// correlation into the map payload shape an Envelope carries.
func EncodeReducerPackageOwnershipCorrelation(correlation reducerderivedv1.PackageOwnershipCorrelation) (map[string]any, error) {
	return encodeDirectPayload(correlation)
}

// DecodeReducerPackageConsumptionCorrelation decodes env.Payload into the
// latest reducerderivedv1.PackageConsumptionCorrelation struct.
func DecodeReducerPackageConsumptionCorrelation(env Envelope) (reducerderivedv1.PackageConsumptionCorrelation, error) {
	return decodeLatestMajor[reducerderivedv1.PackageConsumptionCorrelation](FactKindReducerPackageConsumptionCorrelation, env)
}

// EncodeReducerPackageConsumptionCorrelation marshals a typed package
// consumption correlation into the map payload shape an Envelope carries.
func EncodeReducerPackageConsumptionCorrelation(correlation reducerderivedv1.PackageConsumptionCorrelation) (map[string]any, error) {
	return encodeDirectPayload(correlation)
}

// DecodeReducerPackagePublicationCorrelation decodes env.Payload into the
// latest reducerderivedv1.PackagePublicationCorrelation struct.
func DecodeReducerPackagePublicationCorrelation(env Envelope) (reducerderivedv1.PackagePublicationCorrelation, error) {
	return decodeLatestMajor[reducerderivedv1.PackagePublicationCorrelation](FactKindReducerPackagePublicationCorrelation, env)
}

// EncodeReducerPackagePublicationCorrelation marshals a typed package
// publication correlation into the map payload shape an Envelope carries.
func EncodeReducerPackagePublicationCorrelation(correlation reducerderivedv1.PackagePublicationCorrelation) (map[string]any, error) {
	return encodeDirectPayload(correlation)
}
