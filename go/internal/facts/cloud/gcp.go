// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// GCPCloudResourceFactKind identifies one Cloud Asset Inventory resource
	// observation reported by the GCP cloud collector. It is the provider-specific
	// source fact for GCP resource inventory; reducers admit it into the shared
	// CloudResource keyspace. It is deliberately not a generic cloud_resource fact.
	GCPCloudResourceFactKind = "gcp_cloud_resource"
	// GCPCollectionWarningFactKind identifies one explicit partial, unsupported,
	// stale, permission-hidden, quota, or redaction outcome from the GCP cloud
	// collector. It is the GCP analog of aws_warning and records coverage gaps as
	// durable evidence rather than letting them degrade silently into success.
	GCPCollectionWarningFactKind = "gcp_collection_warning"
	// GCPCloudRelationshipFactKind identifies one observed relationship between
	// two GCP resources (provider relationship evidence; reducers resolve both
	// endpoints before any graph write).
	GCPCloudRelationshipFactKind = "gcp_cloud_relationship"
	// GCPTagObservationFactKind identifies one GCP tag/label evidence observation
	// where tag values are fingerprinted.
	GCPTagObservationFactKind = "gcp_tag_observation"
	// GCPIAMPolicyObservationFactKind identifies one GCP IAM policy binding
	// observation; principals are fingerprinted by class and raw policy JSON is
	// never carried.
	GCPIAMPolicyObservationFactKind = "gcp_iam_policy_observation"
	// GCPDNSRecordFactKind identifies one Cloud DNS record observation where the
	// record name and targets are fingerprinted.
	GCPDNSRecordFactKind = "gcp_dns_record"
	// GCPImageReferenceFactKind identifies one GCP runtime image-reference
	// observation, digest-first with a fingerprinted container name.
	GCPImageReferenceFactKind = "gcp_image_reference"

	// GCPCloudResourceSchemaVersion is the GCP cloud resource fact schema. 1.1.0
	// adds the bounded typed-depth `attributes` map and `correlation_anchors`
	// list (mirroring the AWS resource attribute contract). These are generic
	// additive fields: per-asset-type extractors populate them without further
	// schema bumps, so adding a new typed resource type does not move this
	// version.
	GCPCloudResourceSchemaVersion = "1.1.0"
	// GCPCollectionWarningSchemaVersion is the first GCP collection warning fact
	// schema.
	GCPCollectionWarningSchemaVersion = "1.0.0"
	// GCPCloudRelationshipSchemaVersion is the first GCP relationship fact schema.
	GCPCloudRelationshipSchemaVersion = "1.0.0"
	// GCPTagObservationSchemaVersion is the first GCP tag observation fact schema.
	GCPTagObservationSchemaVersion = "1.0.0"
	// GCPIAMPolicyObservationSchemaVersion is the first GCP IAM policy fact schema.
	GCPIAMPolicyObservationSchemaVersion = "1.0.0"
	// GCPDNSRecordSchemaVersion is the first GCP DNS record fact schema.
	GCPDNSRecordSchemaVersion = "1.0.0"
	// GCPImageReferenceSchemaVersion is the first GCP image reference fact schema.
	GCPImageReferenceSchemaVersion = "1.0.0"
)

var gcpFactKinds = []string{
	GCPCloudResourceFactKind,
	GCPCollectionWarningFactKind,
	GCPCloudRelationshipFactKind,
	GCPTagObservationFactKind,
	GCPIAMPolicyObservationFactKind,
	GCPDNSRecordFactKind,
	GCPImageReferenceFactKind,
}

var gcpSchemaVersions = map[string]string{
	GCPCloudResourceFactKind:        GCPCloudResourceSchemaVersion,
	GCPCollectionWarningFactKind:    GCPCollectionWarningSchemaVersion,
	GCPCloudRelationshipFactKind:    GCPCloudRelationshipSchemaVersion,
	GCPTagObservationFactKind:       GCPTagObservationSchemaVersion,
	GCPIAMPolicyObservationFactKind: GCPIAMPolicyObservationSchemaVersion,
	GCPDNSRecordFactKind:            GCPDNSRecordSchemaVersion,
	GCPImageReferenceFactKind:       GCPImageReferenceSchemaVersion,
}

// GCPFactKinds returns the accepted GCP fact kinds in their emission order.
func GCPFactKinds() []string {
	return slices.Clone(gcpFactKinds)
}

// GCPSchemaVersion returns the schema version for a GCP fact kind.
func GCPSchemaVersion(factKind string) (string, bool) {
	version, ok := gcpSchemaVersions[factKind]
	return version, ok
}

// GCPFamilyDeclaration is the typed fact-family contract declaration for the
// gcp family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var GCPFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "gcp",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "gcp_resource_materialization",
	ProjectionHook:         "multi_cloud_runtime_drift",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/cloud/inventory",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "gcp_cloud_relationship", PayloadSchema: "sdk/go/factschema/schema/gcp_cloud_relationship.v1.schema.json"},
		{Kind: "gcp_cloud_resource", SchemaVersion: "1.1.0", PayloadSchema: "sdk/go/factschema/schema/gcp_cloud_resource.v1.schema.json"},
		{Kind: "gcp_collection_warning", PayloadSchema: "sdk/go/factschema/schema/gcp_collection_warning.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "gcp_dns_record", PayloadSchema: "sdk/go/factschema/schema/gcp_dns_record.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "gcp_iam_policy_observation", PayloadSchema: "sdk/go/factschema/schema/gcp_iam_policy_observation.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "gcp_image_reference", PayloadSchema: "sdk/go/factschema/schema/gcp_image_reference.v1.schema.json"},
		{Kind: "gcp_tag_observation", PayloadSchema: "sdk/go/factschema/schema/gcp_tag_observation.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/storage/postgres"},
	},
	Producers: []contract.Producer{
		{ID: "collector-gcp-cloud", Repository: "eshu-hq/eshu", Path: "go/internal/collector/gcpcloud", PlannedRepository: "eshu-hq/eshu-collector-gcp-cloud", AuthorityMode: "core_issued_grant", Kinds: []string{"gcp_cloud_relationship", "gcp_cloud_resource", "gcp_collection_warning", "gcp_dns_record", "gcp_iam_policy_observation", "gcp_image_reference", "gcp_tag_observation"}},
	},
})
