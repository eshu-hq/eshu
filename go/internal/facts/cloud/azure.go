// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// AzureCloudResourceFactKind identifies one Azure Resource Graph resource
	// observation. The reducer owns canonical CloudResource identity; this fact
	// is provider source evidence only.
	AzureCloudResourceFactKind = "azure_cloud_resource"
	// AzureCloudRelationshipFactKind identifies one Azure relationship
	// observation from Resource Graph joins or ARM fallback. It stays
	// provenance until a reducer resolves both endpoints in scope.
	AzureCloudRelationshipFactKind = "azure_cloud_relationship"
	// AzureTagObservationFactKind identifies one Azure tag evidence
	// observation from Resource Graph or ARM fallback.
	AzureTagObservationFactKind = "azure_tag_observation"
	// AzureIdentityObservationFactKind identifies one Azure managed-identity or
	// role/authorization metadata observation. Principal identifiers are
	// fingerprinted, never raw, until an identity design admits them.
	AzureIdentityObservationFactKind = "azure_identity_observation"
	// AzureResourceChangeFactKind identifies one Azure Resource Graph change
	// record. Change records are freshness evidence and cannot, by themselves,
	// prove final resource state.
	AzureResourceChangeFactKind = "azure_resource_change"
	// AzureDNSRecordFactKind identifies one Azure DNS record observation.
	AzureDNSRecordFactKind = "azure_dns_record"
	// AzureImageReferenceFactKind identifies one Azure runtime image-reference
	// observation from AKS, Container Apps, App Service, or VM scale sets.
	AzureImageReferenceFactKind = "azure_image_reference"
	// AzureCollectionWarningFactKind identifies one explicit partial,
	// unsupported, stale, permission-hidden, quota, fallback, truncation, or
	// redaction outcome for an Azure collection scope.
	AzureCollectionWarningFactKind = "azure_collection_warning"

	// AzureCloudResourceSchemaVersion is the first Azure cloud resource schema.
	AzureCloudResourceSchemaVersion = "1.0.0"
	// AzureCloudRelationshipSchemaVersion is the first Azure relationship schema.
	AzureCloudRelationshipSchemaVersion = "1.0.0"
	// AzureTagObservationSchemaVersion is the first Azure tag observation schema.
	AzureTagObservationSchemaVersion = "1.0.0"
	// AzureIdentityObservationSchemaVersion is the first Azure identity schema.
	AzureIdentityObservationSchemaVersion = "1.0.0"
	// AzureResourceChangeSchemaVersion is the first Azure resource change schema.
	AzureResourceChangeSchemaVersion = "1.0.0"
	// AzureDNSRecordSchemaVersion is the first Azure DNS record schema.
	AzureDNSRecordSchemaVersion = "1.0.0"
	// AzureImageReferenceSchemaVersion is the first Azure image reference schema.
	AzureImageReferenceSchemaVersion = "1.0.0"
	// AzureCollectionWarningSchemaVersion is the first Azure collection warning
	// schema.
	AzureCollectionWarningSchemaVersion = "1.0.0"
)

var azureFactKinds = []string{
	AzureCloudResourceFactKind,
	AzureCloudRelationshipFactKind,
	AzureTagObservationFactKind,
	AzureIdentityObservationFactKind,
	AzureResourceChangeFactKind,
	AzureDNSRecordFactKind,
	AzureImageReferenceFactKind,
	AzureCollectionWarningFactKind,
}

var azureSchemaVersions = map[string]string{
	AzureCloudResourceFactKind:       AzureCloudResourceSchemaVersion,
	AzureCloudRelationshipFactKind:   AzureCloudRelationshipSchemaVersion,
	AzureTagObservationFactKind:      AzureTagObservationSchemaVersion,
	AzureIdentityObservationFactKind: AzureIdentityObservationSchemaVersion,
	AzureResourceChangeFactKind:      AzureResourceChangeSchemaVersion,
	AzureDNSRecordFactKind:           AzureDNSRecordSchemaVersion,
	AzureImageReferenceFactKind:      AzureImageReferenceSchemaVersion,
	AzureCollectionWarningFactKind:   AzureCollectionWarningSchemaVersion,
}

// AzureFactKinds returns the accepted Azure fact kinds in their emission order.
func AzureFactKinds() []string {
	return slices.Clone(azureFactKinds)
}

// AzureSchemaVersion returns the schema version for an Azure fact kind.
func AzureSchemaVersion(factKind string) (string, bool) {
	version, ok := azureSchemaVersions[factKind]
	return version, ok
}

// AzureFamilyDeclaration is the typed fact-family contract declaration for the
// azure family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var AzureFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "azure",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "azure_resource_materialization",
	ProjectionHook:         "multi_cloud_runtime_drift",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/cloud/inventory",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "azure_cloud_relationship", PayloadSchema: "sdk/go/factschema/schema/azure_cloud_relationship.v1.schema.json"},
		{Kind: "azure_cloud_resource", PayloadSchema: "sdk/go/factschema/schema/azure_cloud_resource.v1.schema.json"},
		{Kind: "azure_collection_warning", PayloadSchema: "sdk/go/factschema/schema/azure_collection_warning.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "azure_dns_record", PayloadSchema: "sdk/go/factschema/schema/azure_dns_record.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "azure_identity_observation", PayloadSchema: "sdk/go/factschema/schema/azure_identity_observation.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/storage/postgres"},
		{Kind: "azure_image_reference", PayloadSchema: "sdk/go/factschema/schema/azure_image_reference.v1.schema.json"},
		{Kind: "azure_resource_change", PayloadSchema: "sdk/go/factschema/schema/azure_resource_change.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/storage/postgres"},
		{Kind: "azure_tag_observation", PayloadSchema: "sdk/go/factschema/schema/azure_tag_observation.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/storage/postgres"},
	},
	Producers: []contract.Producer{
		{ID: "collector-azure-cloud", Repository: "eshu-hq/eshu", Path: "go/internal/collector/cloud/azure", PlannedRepository: "eshu-hq/eshu-collector-azure-cloud", AuthorityMode: "core_issued_grant", Kinds: []string{"azure_cloud_relationship", "azure_cloud_resource", "azure_collection_warning", "azure_dns_record", "azure_identity_observation", "azure_image_reference", "azure_resource_change", "azure_tag_observation"}},
	},
})
