// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// ReducerSupplyChainImpactFindingFactKind identifies one reducer-owned
	// vulnerability impact finding persisted for the supply-chain impact read
	// surface.
	ReducerSupplyChainImpactFindingFactKind = "reducer_supply_chain_impact_finding"
	// ReducerAWSCloudRuntimeDriftFindingFactKind identifies one reducer-owned
	// AWS runtime drift finding persisted for the AWS drift read surface.
	ReducerAWSCloudRuntimeDriftFindingFactKind = "reducer_aws_cloud_runtime_drift_finding"
	// ReducerMultiCloudRuntimeDriftFindingFactKind identifies one reducer-owned
	// provider-neutral runtime drift finding persisted for the cloud drift read
	// surface.
	ReducerMultiCloudRuntimeDriftFindingFactKind = "reducer_multi_cloud_runtime_drift_finding"
	// ReducerPackageOwnershipCorrelationFactKind identifies one reducer-owned
	// package ownership correlation persisted for package ownership read surfaces.
	ReducerPackageOwnershipCorrelationFactKind = "reducer_package_ownership_correlation"
	// ReducerPackageConsumptionCorrelationFactKind identifies one reducer-owned
	// package consumption correlation persisted for supply-chain read surfaces.
	ReducerPackageConsumptionCorrelationFactKind = "reducer_package_consumption_correlation"
	// ReducerPackagePublicationCorrelationFactKind identifies one reducer-owned
	// package publication correlation persisted for package provenance read surfaces.
	ReducerPackagePublicationCorrelationFactKind = "reducer_package_publication_correlation"
	// ReducerTerraformConfigStateDriftFindingFactKind identifies one
	// reducer-owned Terraform config-vs-state drift finding persisted for the
	// provider-neutral Terraform drift read surface (issue #5442). Unlike the
	// AWS/multi-cloud runtime drift kinds, this kind carries two distinct row
	// shapes distinguished by its outcome field: a per-address row (outcome
	// "exact") for each of the five DriftKind classifications, and a
	// per-state-scope row (outcome "ambiguous") recorded when backend-owner
	// resolution finds more than one candidate config repo. See
	// go/internal/correlation/drift/tfconfigstate/doc.go for the outcome-model
	// reasoning.
	ReducerTerraformConfigStateDriftFindingFactKind = "reducer_terraform_config_state_drift_finding"
	// ReducerCloudAssetResolutionFactKind identifies the reducer-internal cloud
	// asset resolution canonicalization row. It is registered as
	// admission-exempt, not versioned.
	ReducerCloudAssetResolutionFactKind = "reducer_cloud_asset_resolution"

	// ReducerDerivedSchemaVersionV1 is the first governed reducer-derived fact
	// schema version.
	ReducerDerivedSchemaVersionV1 = "1.0.0"
)

var reducerDerivedFactKinds = []string{
	ReducerSupplyChainImpactFindingFactKind,
	ReducerAWSCloudRuntimeDriftFindingFactKind,
	ReducerMultiCloudRuntimeDriftFindingFactKind,
	ReducerPackageOwnershipCorrelationFactKind,
	ReducerPackageConsumptionCorrelationFactKind,
	ReducerPackagePublicationCorrelationFactKind,
	ReducerTerraformConfigStateDriftFindingFactKind,
}

var reducerDerivedSchemaVersions = map[string]string{
	ReducerSupplyChainImpactFindingFactKind:         ReducerDerivedSchemaVersionV1,
	ReducerAWSCloudRuntimeDriftFindingFactKind:      ReducerDerivedSchemaVersionV1,
	ReducerMultiCloudRuntimeDriftFindingFactKind:    ReducerDerivedSchemaVersionV1,
	ReducerPackageOwnershipCorrelationFactKind:      ReducerDerivedSchemaVersionV1,
	ReducerPackageConsumptionCorrelationFactKind:    ReducerDerivedSchemaVersionV1,
	ReducerPackagePublicationCorrelationFactKind:    ReducerDerivedSchemaVersionV1,
	ReducerTerraformConfigStateDriftFindingFactKind: ReducerDerivedSchemaVersionV1,
}

// ReducerDerivedFactKinds returns governed reducer-derived fact kinds.
func ReducerDerivedFactKinds() []string {
	return slices.Clone(reducerDerivedFactKinds)
}

// ReducerDerivedSchemaVersion returns the schema version for a governed
// reducer-derived fact kind.
func ReducerDerivedSchemaVersion(factKind string) (string, bool) {
	version, ok := reducerDerivedSchemaVersions[factKind]
	return version, ok
}
