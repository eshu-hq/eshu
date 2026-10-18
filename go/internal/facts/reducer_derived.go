// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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
	// ReducerCodeDriftedFindingFactKind identifies one reducer-owned drifted
	// parallel-implementation pair persisted for the code-divergence drifted
	// read surface (epic #6833, child #6837).
	ReducerCodeDriftedFindingFactKind = "reducer_code_drifted_finding"
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
	ReducerCodeDriftedFindingFactKind,
}

var reducerDerivedSchemaVersions = map[string]string{
	ReducerSupplyChainImpactFindingFactKind:         ReducerDerivedSchemaVersionV1,
	ReducerAWSCloudRuntimeDriftFindingFactKind:      ReducerDerivedSchemaVersionV1,
	ReducerMultiCloudRuntimeDriftFindingFactKind:    ReducerDerivedSchemaVersionV1,
	ReducerPackageOwnershipCorrelationFactKind:      ReducerDerivedSchemaVersionV1,
	ReducerPackageConsumptionCorrelationFactKind:    ReducerDerivedSchemaVersionV1,
	ReducerPackagePublicationCorrelationFactKind:    ReducerDerivedSchemaVersionV1,
	ReducerTerraformConfigStateDriftFindingFactKind: ReducerDerivedSchemaVersionV1,
	ReducerCodeDriftedFindingFactKind:               ReducerDerivedSchemaVersionV1,
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

// ReducerDerivedFamilyDeclaration is the typed fact-family contract declaration for the
// reducer_derived family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var ReducerDerivedFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "reducer_derived",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "reducer_derived_findings",
	ProjectionHook:         "reducer_derived_read_model",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/supply-chain/impact/findings",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "reducer_aws_cloud_runtime_drift_finding", ReadSurface: "POST /api/v0/aws/runtime-drift/findings", PayloadSchema: "sdk/go/factschema/schema/reducer_aws_cloud_runtime_drift_finding.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in query read model"},
		{Kind: "reducer_code_drifted_finding", ReadSurface: "POST /api/v0/code/divergence/findings", PayloadSchema: "sdk/go/factschema/schema/reducer_code_drifted_finding.v1.schema.json"},
		{Kind: "reducer_multi_cloud_runtime_drift_finding", ReadSurface: "POST /api/v0/cloud/runtime-drift/findings", PayloadSchema: "sdk/go/factschema/schema/reducer_multi_cloud_runtime_drift_finding.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in query read model"},
		{Kind: "reducer_package_consumption_correlation", ReadSurface: "GET /api/v0/package-registry/correlations", PayloadSchema: "sdk/go/factschema/schema/reducer_package_consumption_correlation.v1.schema.json"},
		{Kind: "reducer_package_ownership_correlation", ReadSurface: "GET /api/v0/package-registry/correlations", PayloadSchema: "sdk/go/factschema/schema/reducer_package_ownership_correlation.v1.schema.json"},
		{Kind: "reducer_package_publication_correlation", ReadSurface: "GET /api/v0/package-registry/correlations", PayloadSchema: "sdk/go/factschema/schema/reducer_package_publication_correlation.v1.schema.json"},
		{Kind: "reducer_supply_chain_impact_finding", PayloadSchema: "sdk/go/factschema/schema/reducer_supply_chain_impact_finding.v1.schema.json"},
		{Kind: "reducer_terraform_config_state_drift_finding", ReadSurface: "POST /api/v0/terraform/config-state-drift/findings", PayloadSchema: "sdk/go/factschema/schema/reducer_terraform_config_state_drift_finding.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in query read model"},
	},
	Producers: []contract.Producer{
		{ID: "reducer", Repository: "eshu-hq/eshu", Path: "go/internal/reducer", AuthorityMode: "core_owned", Kinds: []string{"reducer_aws_cloud_runtime_drift_finding", "reducer_code_drifted_finding", "reducer_multi_cloud_runtime_drift_finding", "reducer_package_consumption_correlation", "reducer_package_ownership_correlation", "reducer_package_publication_correlation", "reducer_supply_chain_impact_finding", "reducer_terraform_config_state_drift_finding"}},
	},
})

// ReducerInternalFamilyDeclaration is the typed fact-family contract declaration for the
// reducer_internal family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var ReducerInternalFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "reducer_internal",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	AdmissionExempt:        true,
	ReducerDomain:          "cloud_asset_resolution",
	ProjectionHook:         "cloud_asset_resolution",
	AdmissionHook:          "none",
	ReadSurface:            "none",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{Kind: "reducer_cloud_asset_resolution", Disposition: "unconsumed", NoReducerReason: "Reducer-internal durable resolution fact with no production query, MCP, or storage consumer today (documented exemption).", NoReducerEvidence: "go/internal/reducer/cloudasset/doc.go; docs/internal/design/4784-reducer-derived-fact-governance.md"},
	},
	Producers: []contract.Producer{
		{ID: "reducer", Repository: "eshu-hq/eshu", Path: "go/internal/reducer", AuthorityMode: "core_owned", Kinds: []string{"reducer_cloud_asset_resolution"}},
	},
})
