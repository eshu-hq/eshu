// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// This file is the facts root's transitional compatibility surface for the
// cloud-provider inventory fact families, which moved to [cloud] in issue
// #6776 so the root directory drops back under the 40-file dirgate cap.
// Every entry is an alias or a thin forwarder with no behavior change: the
// value, the type identity, and the returned bytes are the same ones the
// root declared before the move.
//
// It carries only the names that still have a caller -- the facts root's own
// schemaVersionFamilies registry wiring and registry tests plus the
// collectors, reducers, projectors, and query surfaces that reach them as
// facts.X today. A later family move adds a stanza to this file and never
// creates a new compat_*.go, and each entry here is deleted once its last
// caller has moved to the cloud package directly (see the importer-migration
// follow-up, #6950).
//
// Omitted deliberately because nothing references it:
// AWSTagObservationSchemaVersion. Reach it as
// cloud.AWSTagObservationSchemaVersion; AWSSchemaVersion("aws_tag_observation")
// still returns its value.

import "github.com/eshu-hq/eshu/go/internal/facts/cloud"

// Stanza: aws.go (moved to cloud/aws.go).
const (
	// AWSDNSRecordFactKind identifies one Route53 DNS record observation. See
	// [cloud.AWSDNSRecordFactKind].
	AWSDNSRecordFactKind = cloud.AWSDNSRecordFactKind
	// AWSDNSRecordSchemaVersion is the first AWS DNS record schema. See
	// [cloud.AWSDNSRecordSchemaVersion].
	AWSDNSRecordSchemaVersion = cloud.AWSDNSRecordSchemaVersion
	// AWSIAMPermissionFactKind identifies one derived IAM permission statement.
	// It is the normalized, metadata-only projection of a single IAM policy
	// statement attached to a principal: effect, action set, resource pattern,
	// and a condition summary. It NEVER carries the raw policy JSON body or any
	// condition values. PR1 emits this fact; the reducer graph projection that
	// consumes it ships separately under principal review (issue #1134). See
	// [cloud.AWSIAMPermissionFactKind].
	AWSIAMPermissionFactKind = cloud.AWSIAMPermissionFactKind
	// AWSIAMPermissionSchemaVersion is the first derived IAM permission schema.
	// See [cloud.AWSIAMPermissionSchemaVersion].
	AWSIAMPermissionSchemaVersion = cloud.AWSIAMPermissionSchemaVersion
	// AWSImageReferenceFactKind identifies one ECR image reference observation.
	// See [cloud.AWSImageReferenceFactKind].
	AWSImageReferenceFactKind = cloud.AWSImageReferenceFactKind
	// AWSImageReferenceSchemaVersion is the first AWS image reference schema. See
	// [cloud.AWSImageReferenceSchemaVersion].
	AWSImageReferenceSchemaVersion = cloud.AWSImageReferenceSchemaVersion
	// AWSRelationshipFactKind identifies one relationship reported by AWS APIs.
	// See [cloud.AWSRelationshipFactKind].
	AWSRelationshipFactKind = cloud.AWSRelationshipFactKind
	// AWSRelationshipSchemaVersion is the first AWS relationship fact schema. See
	// [cloud.AWSRelationshipSchemaVersion].
	AWSRelationshipSchemaVersion = cloud.AWSRelationshipSchemaVersion
	// AWSResourceFactKind identifies one resource reported by an AWS API. See
	// [cloud.AWSResourceFactKind].
	AWSResourceFactKind = cloud.AWSResourceFactKind
	// AWSResourcePolicyPermissionFactKind identifies one derived resource-based-
	// policy permission statement. It is the resource-side analog of
	// aws_iam_permission: the normalized, metadata-only projection of a single
	// statement from a resource policy attached to an AWS resource (an S3 bucket
	// policy or a KMS key policy). It captures the attached resource identity,
	// the statement effect, the normalized action/resource patterns, a condition-
	// key summary, and the derived grantee principal facts (principal account
	// ids, principal types, public/anonymous, cross-account). It NEVER carries
	// the raw policy JSON body, statement Sid/bodies, or condition values. PR4b
	// of #1134 emits this fact; the resource-policy-aware CAN_PERFORM reducer
	// follow-up consumes it. See [cloud.AWSResourcePolicyPermissionFactKind].
	AWSResourcePolicyPermissionFactKind = cloud.AWSResourcePolicyPermissionFactKind
	// AWSResourcePolicyPermissionSchemaVersion is the first derived resource-
	// policy permission schema. See
	// [cloud.AWSResourcePolicyPermissionSchemaVersion].
	AWSResourcePolicyPermissionSchemaVersion = cloud.AWSResourcePolicyPermissionSchemaVersion
	// AWSResourceSchemaVersion is the first AWS resource fact schema. See
	// [cloud.AWSResourceSchemaVersion].
	AWSResourceSchemaVersion = cloud.AWSResourceSchemaVersion
	// AWSSecurityGroupRuleFactKind identifies one normalized EC2 security-group
	// ingress or egress rule. It is a derived posture fact distinct from the raw
	// aws_resource security-group-rule observation: each fact carries the single
	// normalized reachability tuple (group, direction, protocol, port range,
	// source) that the reducer projects into network-reachability edges. See
	// [cloud.AWSSecurityGroupRuleFactKind].
	AWSSecurityGroupRuleFactKind = cloud.AWSSecurityGroupRuleFactKind
	// AWSSecurityGroupRuleSchemaVersion is the first AWS security-group-rule
	// posture fact schema. See [cloud.AWSSecurityGroupRuleSchemaVersion].
	AWSSecurityGroupRuleSchemaVersion = cloud.AWSSecurityGroupRuleSchemaVersion
	// AWSTagObservationFactKind identifies one raw AWS tag observation. See
	// [cloud.AWSTagObservationFactKind].
	AWSTagObservationFactKind = cloud.AWSTagObservationFactKind
	// AWSWarningFactKind identifies one non-fatal AWS scanner warning. See
	// [cloud.AWSWarningFactKind].
	AWSWarningFactKind = cloud.AWSWarningFactKind
	// AWSWarningSchemaVersion is the first AWS warning fact schema. See
	// [cloud.AWSWarningSchemaVersion].
	AWSWarningSchemaVersion = cloud.AWSWarningSchemaVersion
)

// AWSFactKinds returns the accepted AWS fact kinds in their emission order.
// See [cloud.AWSFactKinds].
func AWSFactKinds() []string {
	return cloud.AWSFactKinds()
}

// AWSSchemaVersion returns the schema version for an AWS fact kind. See
// [cloud.AWSSchemaVersion].
func AWSSchemaVersion(factKind string) (string, bool) {
	return cloud.AWSSchemaVersion(factKind)
}

// Stanza: azure.go (moved to cloud/azure.go).
const (
	// AzureCloudRelationshipFactKind identifies one Azure relationship
	// observation from Resource Graph joins or ARM fallback. It stays provenance
	// until a reducer resolves both endpoints in scope. See
	// [cloud.AzureCloudRelationshipFactKind].
	AzureCloudRelationshipFactKind = cloud.AzureCloudRelationshipFactKind
	// AzureCloudRelationshipSchemaVersion is the first Azure relationship schema.
	// See [cloud.AzureCloudRelationshipSchemaVersion].
	AzureCloudRelationshipSchemaVersion = cloud.AzureCloudRelationshipSchemaVersion
	// AzureCloudResourceFactKind identifies one Azure Resource Graph resource
	// observation. The reducer owns canonical CloudResource identity; this fact
	// is provider source evidence only. See [cloud.AzureCloudResourceFactKind].
	AzureCloudResourceFactKind = cloud.AzureCloudResourceFactKind
	// AzureCloudResourceSchemaVersion is the first Azure cloud resource schema.
	// See [cloud.AzureCloudResourceSchemaVersion].
	AzureCloudResourceSchemaVersion = cloud.AzureCloudResourceSchemaVersion
	// AzureCollectionWarningFactKind identifies one explicit partial,
	// unsupported, stale, permission-hidden, quota, fallback, truncation, or
	// redaction outcome for an Azure collection scope. See
	// [cloud.AzureCollectionWarningFactKind].
	AzureCollectionWarningFactKind = cloud.AzureCollectionWarningFactKind
	// AzureCollectionWarningSchemaVersion is the first Azure collection warning
	// schema. See [cloud.AzureCollectionWarningSchemaVersion].
	AzureCollectionWarningSchemaVersion = cloud.AzureCollectionWarningSchemaVersion
	// AzureDNSRecordFactKind identifies one Azure DNS record observation. See
	// [cloud.AzureDNSRecordFactKind].
	AzureDNSRecordFactKind = cloud.AzureDNSRecordFactKind
	// AzureDNSRecordSchemaVersion is the first Azure DNS record schema. See
	// [cloud.AzureDNSRecordSchemaVersion].
	AzureDNSRecordSchemaVersion = cloud.AzureDNSRecordSchemaVersion
	// AzureIdentityObservationFactKind identifies one Azure managed-identity or
	// role/authorization metadata observation. Principal identifiers are
	// fingerprinted, never raw, until an identity design admits them. See
	// [cloud.AzureIdentityObservationFactKind].
	AzureIdentityObservationFactKind = cloud.AzureIdentityObservationFactKind
	// AzureIdentityObservationSchemaVersion is the first Azure identity schema.
	// See [cloud.AzureIdentityObservationSchemaVersion].
	AzureIdentityObservationSchemaVersion = cloud.AzureIdentityObservationSchemaVersion
	// AzureImageReferenceFactKind identifies one Azure runtime image-reference
	// observation from AKS, Container Apps, App Service, or VM scale sets. See
	// [cloud.AzureImageReferenceFactKind].
	AzureImageReferenceFactKind = cloud.AzureImageReferenceFactKind
	// AzureImageReferenceSchemaVersion is the first Azure image reference schema.
	// See [cloud.AzureImageReferenceSchemaVersion].
	AzureImageReferenceSchemaVersion = cloud.AzureImageReferenceSchemaVersion
	// AzureResourceChangeFactKind identifies one Azure Resource Graph change
	// record. Change records are freshness evidence and cannot, by themselves,
	// prove final resource state. See [cloud.AzureResourceChangeFactKind].
	AzureResourceChangeFactKind = cloud.AzureResourceChangeFactKind
	// AzureResourceChangeSchemaVersion is the first Azure resource change schema.
	// See [cloud.AzureResourceChangeSchemaVersion].
	AzureResourceChangeSchemaVersion = cloud.AzureResourceChangeSchemaVersion
	// AzureTagObservationFactKind identifies one Azure tag evidence observation
	// from Resource Graph or ARM fallback. See
	// [cloud.AzureTagObservationFactKind].
	AzureTagObservationFactKind = cloud.AzureTagObservationFactKind
	// AzureTagObservationSchemaVersion is the first Azure tag observation schema.
	// See [cloud.AzureTagObservationSchemaVersion].
	AzureTagObservationSchemaVersion = cloud.AzureTagObservationSchemaVersion
)

// AzureFactKinds returns the accepted Azure fact kinds in their emission
// order. See [cloud.AzureFactKinds].
func AzureFactKinds() []string {
	return cloud.AzureFactKinds()
}

// AzureSchemaVersion returns the schema version for an Azure fact kind. See
// [cloud.AzureSchemaVersion].
func AzureSchemaVersion(factKind string) (string, bool) {
	return cloud.AzureSchemaVersion(factKind)
}

// Stanza: gcp.go (moved to cloud/gcp.go).
const (
	// GCPCloudRelationshipFactKind identifies one observed relationship between
	// two GCP resources (provider relationship evidence; reducers resolve both
	// endpoints before any graph write). See
	// [cloud.GCPCloudRelationshipFactKind].
	GCPCloudRelationshipFactKind = cloud.GCPCloudRelationshipFactKind
	// GCPCloudRelationshipSchemaVersion is the first GCP relationship fact
	// schema. See [cloud.GCPCloudRelationshipSchemaVersion].
	GCPCloudRelationshipSchemaVersion = cloud.GCPCloudRelationshipSchemaVersion
	// GCPCloudResourceFactKind identifies one Cloud Asset Inventory resource
	// observation reported by the GCP cloud collector. It is the provider-
	// specific source fact for GCP resource inventory; reducers admit it into the
	// shared CloudResource keyspace. It is deliberately not a generic
	// cloud_resource fact. See [cloud.GCPCloudResourceFactKind].
	GCPCloudResourceFactKind = cloud.GCPCloudResourceFactKind
	// GCPCloudResourceSchemaVersion is the GCP cloud resource fact schema. 1.1.0
	// adds the bounded typed-depth `attributes` map and `correlation_anchors`
	// list (mirroring the AWS resource attribute contract). These are generic
	// additive fields: per-asset-type extractors populate them without further
	// schema bumps, so adding a new typed resource type does not move this
	// version. See [cloud.GCPCloudResourceSchemaVersion].
	GCPCloudResourceSchemaVersion = cloud.GCPCloudResourceSchemaVersion
	// GCPCollectionWarningFactKind identifies one explicit partial, unsupported,
	// stale, permission-hidden, quota, or redaction outcome from the GCP cloud
	// collector. It is the GCP analog of aws_warning and records coverage gaps as
	// durable evidence rather than letting them degrade silently into success.
	// See [cloud.GCPCollectionWarningFactKind].
	GCPCollectionWarningFactKind = cloud.GCPCollectionWarningFactKind
	// GCPCollectionWarningSchemaVersion is the first GCP collection warning fact
	// schema. See [cloud.GCPCollectionWarningSchemaVersion].
	GCPCollectionWarningSchemaVersion = cloud.GCPCollectionWarningSchemaVersion
	// GCPDNSRecordFactKind identifies one Cloud DNS record observation where the
	// record name and targets are fingerprinted. See
	// [cloud.GCPDNSRecordFactKind].
	GCPDNSRecordFactKind = cloud.GCPDNSRecordFactKind
	// GCPDNSRecordSchemaVersion is the first GCP DNS record fact schema. See
	// [cloud.GCPDNSRecordSchemaVersion].
	GCPDNSRecordSchemaVersion = cloud.GCPDNSRecordSchemaVersion
	// GCPIAMPolicyObservationFactKind identifies one GCP IAM policy binding
	// observation; principals are fingerprinted by class and raw policy JSON is
	// never carried. See [cloud.GCPIAMPolicyObservationFactKind].
	GCPIAMPolicyObservationFactKind = cloud.GCPIAMPolicyObservationFactKind
	// GCPIAMPolicyObservationSchemaVersion is the first GCP IAM policy fact
	// schema. See [cloud.GCPIAMPolicyObservationSchemaVersion].
	GCPIAMPolicyObservationSchemaVersion = cloud.GCPIAMPolicyObservationSchemaVersion
	// GCPImageReferenceFactKind identifies one GCP runtime image-reference
	// observation, digest-first with a fingerprinted container name. See
	// [cloud.GCPImageReferenceFactKind].
	GCPImageReferenceFactKind = cloud.GCPImageReferenceFactKind
	// GCPImageReferenceSchemaVersion is the first GCP image reference fact
	// schema. See [cloud.GCPImageReferenceSchemaVersion].
	GCPImageReferenceSchemaVersion = cloud.GCPImageReferenceSchemaVersion
	// GCPTagObservationFactKind identifies one GCP tag/label evidence observation
	// where tag values are fingerprinted. See [cloud.GCPTagObservationFactKind].
	GCPTagObservationFactKind = cloud.GCPTagObservationFactKind
	// GCPTagObservationSchemaVersion is the first GCP tag observation fact
	// schema. See [cloud.GCPTagObservationSchemaVersion].
	GCPTagObservationSchemaVersion = cloud.GCPTagObservationSchemaVersion
)

// GCPFactKinds returns the accepted GCP fact kinds in their emission order.
// See [cloud.GCPFactKinds].
func GCPFactKinds() []string {
	return cloud.GCPFactKinds()
}

// GCPSchemaVersion returns the schema version for a GCP fact kind. See
// [cloud.GCPSchemaVersion].
func GCPSchemaVersion(factKind string) (string, bool) {
	return cloud.GCPSchemaVersion(factKind)
}
