// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

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

	// GCPCloudResourceSchemaVersion is the first GCP cloud resource fact schema.
	GCPCloudResourceSchemaVersion = "1.0.0"
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
