// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Shared, root, and collector-kind constants for the GCP cloud collector
// (#6696, slice L). Consolidated verbatim out of the gcpcloud root files
// (warnings.go, metrics.go, envelope.go, types.go, normalize.go, parse.go,
// redaction.go, dns_record.go, gcp_secrets_iam.go, iam_policy_observation.go,
// image_reference.go, tag_observation.go, generation.go) and the extractor
// shared leaf (extractor/bounds.go, extractor/relationships.go,
// extractor/names.go, extractor/shared.go).
//
// A move must never change a value: gcp_l_test.go pins every constant
// byte-identical to its pre-consolidation gcpcloud value.

// Claim boundary and scope (from extractor/bounds.go).

// ParentScopeKind enumerates the Cloud Asset Inventory parent scope kinds a GCP
// collector claim can shard by. Values are bounded enums safe for telemetry
// labels and scope identity; they never carry provider numbers or names.
type ParentScopeKind string

const (
	// ParentScopeOrganization is an organization-level CAI parent scope.
	ParentScopeOrganization ParentScopeKind = "organization"
	// ParentScopeFolder is a folder-level CAI parent scope.
	ParentScopeFolder ParentScopeKind = "folder"
	// ParentScopeProject is a project-level CAI parent scope.
	ParentScopeProject ParentScopeKind = "project"
)

// Valid reports whether the parent scope kind is one of the bounded enum values.
func (p ParentScopeKind) Valid() bool {
	switch p {
	case ParentScopeOrganization, ParentScopeFolder, ParentScopeProject:
		return true
	default:
		return false
	}
}

// Relationship support states (from extractor/relationships.go). They are
// provenance only: the collector reports what Cloud Asset Inventory returned
// and resolves no endpoints and writes no graph edge (reducer-owned).
const (
	// RelationshipSupportSupported means the provider returned a complete
	// relationship between two resources. It is the default for an observed
	// relationship.
	RelationshipSupportSupported = "supported"
	// RelationshipSupportPartial means the relationship was observed but the
	// target is opaque or outside the readable boundary (cross-project), so a
	// reducer must treat the target as unresolved rather than a clean match.
	RelationshipSupportPartial = "partial"
	// RelationshipSupportUnsupported means the relationship type or tier is not
	// fully supported (for example a Security Command Center
	// premium/enterprise-gated edge), carried as provenance only.
	RelationshipSupportUnsupported = "unsupported"
)

// Cross-group CAI asset types, resource-name prefixes, sentinels, and
// markers (from extractor/names.go and extractor/shared.go). Each names the
// CAI asset type or full-resource-name shape of a typed edge endpoint so
// reducers can resolve both endpoints exactly.
//
// Helpers here decode only resource identities (bucket names, topic names,
// hosts, key names). They never return data-plane locators such as object
// paths inside source URIs or request bodies.
const (
	// AssetTypeGKECluster is the CAI asset type for a GKE cluster, the target
	// of environment-to-cluster edges.
	AssetTypeGKECluster = "container.googleapis.com/Cluster"
	// AssetTypeComputeNetwork is the CAI asset type for a Compute Engine VPC
	// network.
	AssetTypeComputeNetwork = "compute.googleapis.com/Network"
	// AssetTypeComputeSubnetwork is the CAI asset type for a Compute Engine
	// subnetwork.
	AssetTypeComputeSubnetwork = "compute.googleapis.com/Subnetwork"
	// AssetTypePubSubTopic is the CAI asset type for a Pub/Sub topic.
	AssetTypePubSubTopic = "pubsub.googleapis.com/Topic"
	// SecretManagerSecretAssetType is the CAI asset type for a Secret Manager
	// secret.
	SecretManagerSecretAssetType = "secretmanager.googleapis.com/Secret"
	// ComputeResourceNamePrefix is the CAI full-resource-name prefix for
	// Compute Engine resources.
	ComputeResourceNamePrefix = "//compute.googleapis.com/"
	// PubSubResourceNamePrefix is the CAI full-resource-name prefix for
	// Pub/Sub resources.
	PubSubResourceNamePrefix = "//pubsub.googleapis.com/"
	// DefaultServiceAccountSentinel is the GKE API's sentinel value for "use
	// the default service account".
	DefaultServiceAccountSentinel = "default"
	// PubSubDeletedTopicSentinel is the value Pub/Sub reports for a
	// subscription's topic when the topic was deleted.
	PubSubDeletedTopicSentinel = "_deleted-topic_"
)

// RunServiceResourceNamePrefix is the CAI full-resource-name prefix for Cloud
// Run services, used to build revision-parent and trigger-target endpoints.
const RunServiceResourceNamePrefix = "//run.googleapis.com/"

// ComputeSelfLinkMarker delimits the API version segment in a Compute Engine
// selfLink; the path after the version segment is the CAI resource path.
const ComputeSelfLinkMarker = "/compute/"

// CloudKMSResourceNamePrefix is the CAI full-resource-name prefix for Cloud
// KMS keys. A bare relative CMEK reference (projects/.../cryptoKeys/...) is
// normalized by prefixing it with this value.
const CloudKMSResourceNamePrefix = "//cloudkms.googleapis.com/"

// Asset types shared by extractors across service groups (from
// extractor/shared.go). Each names the CAI asset type of a typed edge
// endpoint so reducers can resolve both endpoints exactly.
const (
	// AssetTypeKMSCryptoKey is the CAI asset type for a Cloud KMS CryptoKey,
	// the target of CMEK encryption edges from many groups.
	AssetTypeKMSCryptoKey = "cloudkms.googleapis.com/CryptoKey"
	// AssetTypeStorageBucket is the CAI asset type for a Cloud Storage bucket,
	// the target of source-bucket and DAG-bucket edges.
	AssetTypeStorageBucket = "storage.googleapis.com/Bucket"
	// AssetTypeRunService is the CAI asset type for a Cloud Run service, the
	// target of trigger and revision-parent edges.
	AssetTypeRunService = "run.googleapis.com/Service"
	// AssetTypeRunJob is the CAI asset type for a Cloud Run job, handled
	// alongside services for container image references.
	AssetTypeRunJob = "run.googleapis.com/Job"
)

// StorageBucketResourceNamePrefixFmt is a plain string prefix, not a
// fmt.Sprintf format string, despite the "Fmt" suffix: it carries no `%` verb.
// Every caller builds a bucket full resource name by concatenating the bucket
// name directly onto this prefix, never via fmt.Sprintf.
const StorageBucketResourceNamePrefixFmt = "//storage.googleapis.com/projects/_/buckets/"

// Warning kinds and outcomes (from warnings.go). Each value is a bounded enum
// safe for telemetry labels and durable status keys.
const (
	// WarningKindPartialPermission marks a scope or content family the collector
	// could not fully read because a project, folder, or content-type permission
	// was missing.
	WarningKindPartialPermission = "partial_permission"
	// WarningKindUnsupported marks a content family or relationship tier the
	// provider does not expose, for example missing Security Command Center tier
	// for relationship content.
	WarningKindUnsupported = "unsupported"
	// WarningKindQuota marks a scan that hit Cloud Asset Inventory quota or
	// throttle limits before completing.
	WarningKindQuota = "quota"
	// WarningKindUnavailable marks a scan that could not reach Cloud Asset
	// Inventory or received a retry-exhausted provider availability failure.
	WarningKindUnavailable = "unavailable"
	// WarningKindStale marks a generation that was rejected because a newer
	// generation already owns the shard.
	WarningKindStale = "stale_generation"
	// WarningKindRedaction marks a resource whose payload required redaction
	// beyond the normal label/member fingerprinting policy.
	WarningKindRedaction = "redaction"
	// WarningKindPageTokenExpired marks a paginated scan whose continuation token
	// expired before the shard finished.
	WarningKindPageTokenExpired = "page_token_expired" // #nosec G101 -- warning-kind label for a pagination token expiry, not a credential
)

// Outcome enumerates the bounded per-shard source states a warning reports.
// These mirror the multi-cloud contract per-path evidence states.
const (
	// OutcomePartial means the collector read only part of the configured scope or
	// content family.
	OutcomePartial = "partial"
	// OutcomeUnsupported means the provider, tier, API, or content family does not
	// expose this evidence.
	OutcomeUnsupported = "unsupported"
	// OutcomeUnavailable means the source was configured but unreachable,
	// unauthorized, or rate-limited without current evidence.
	OutcomeUnavailable = "unavailable"
	// OutcomeStale means the accepted generation is older than the configured
	// freshness window.
	OutcomeStale = "stale"
)

// Claim status and extraction outcomes (from metrics.go). Claim status values
// are bounded enums for the claim lifecycle counter; extraction outcomes are
// bounded enums for the typed-depth extractor counter. They are safe as
// telemetry labels.
const (
	// ClaimStatusStarted marks a claim the collector began.
	ClaimStatusStarted = "started"
	// ClaimStatusSucceeded marks a claim that completed with full coverage.
	ClaimStatusSucceeded = "succeeded"
	// ClaimStatusPartial marks a claim that completed with durable warning
	// evidence for incomplete coverage.
	ClaimStatusPartial = "partial"
	// ClaimStatusFailed marks a claim that failed before producing complete
	// evidence.
	ClaimStatusFailed = "failed"
	// ClaimStatusRetried marks a claim retry attempt.
	ClaimStatusRetried = "retried"
)

const (
	// ExtractionOutcomeExtracted marks a resource whose registered extractor
	// produced at least one bounded attribute.
	ExtractionOutcomeExtracted = "extracted"
	// ExtractionOutcomeEmpty marks a resource whose extractor ran but produced no
	// usable attributes.
	ExtractionOutcomeEmpty = "empty"
)

// ExtensionSchemaVersionDefault is the default provider-specific extension
// schema version stamped on a gcp_cloud_resource fact when the observation does
// not supply its own version (from envelope.go). The extension object is
// versioned independently from the fact schema so safe control-plane metadata
// can evolve without a fact schema bump.
const ExtensionSchemaVersionDefault = "1.0.0"

// CollectorKind is the durable collector_kind value for GCP cloud facts (from
// types.go).
const CollectorKind = "gcp"

// UnknownLabel is the bounded fallback used when a normalized telemetry-safe
// value cannot be derived (from normalize.go). It keeps metric labels low
// cardinality and avoids leaking raw provider input.
const UnknownLabel = "unknown"

// GlobalLocation is the normalized location bucket for resources with no
// provider-reported region or zone (from normalize.go).
const GlobalLocation = "global"

// AssetTypeDNSResourceRecordSet is the CAI asset type for a Cloud DNS resource
// record set (from parse.go).
const AssetTypeDNSResourceRecordSet = "dns.googleapis.com/ResourceRecordSet"

// Redaction policy version and reasons (from redaction.go,
// dns_record.go, iam_policy_observation.go, and tag_observation.go).
//
// Rotation: rotating the underlying redact.Key material MUST be accompanied by
// bumping the policy version constant. The version stamp is what distinguishes
// the pre- and post-rotation keyspaces, so fingerprints produced under
// different keys are never treated as the same identity. Bumping it also
// signals that facts stamped with an older version were fingerprinted under
// retired key material and must be re-collected (re-fingerprinted) to join
// the current keyspace. A key rotation without a version bump is unsupported:
// it would silently produce mismatched fingerprints for the same underlying
// identity across the rotation boundary. See
// docs/internal/design/multicloud-live-collector-security-review.md (section
// 2, Redaction-key handling) for the full rotation procedure.
const RedactionPolicyVersion = "gcp-cloud-2026-06-09"

const (
	// RedactionReasonLabel marks a fingerprinted resource label value.
	RedactionReasonLabel = "gcp_label_value"
	// RedactionReasonMember marks a fingerprinted IAM member identity.
	RedactionReasonMember = "gcp_iam_member"
)

// MaxDNSRecordTargets bounds the number of fingerprinted targets carried on
// one gcp_dns_record fact so a record set cannot emit an unbounded payload
// (from dns_record.go).
const MaxDNSRecordTargets = 100

const (
	// RedactionReasonDNSRecordName marks a fingerprinted DNS record name.
	RedactionReasonDNSRecordName = "gcp_dns_record_name"
	// RedactionReasonDNSTarget marks a fingerprinted DNS record target.
	RedactionReasonDNSTarget = "gcp_dns_target"
)

// ServiceAccountAssetType is the Cloud Asset Inventory asset type for a GCP
// IAM service account (from gcp_secrets_iam.go). Trust bindings on this
// resource become gcp_iam_trust_policy facts.
const ServiceAccountAssetType = "iam.googleapis.com/ServiceAccount"

// MaxIAMPolicyMembers bounds the number of member bindings carried on one
// gcp_iam_policy_observation fact so a large binding cannot emit an unbounded
// payload (from iam_policy_observation.go).
const MaxIAMPolicyMembers = 500

const (
	// RedactionReasonIAMEtag marks a fingerprinted IAM policy etag.
	RedactionReasonIAMEtag = "gcp_iam_etag"
	// RedactionReasonIAMCondition marks a fingerprinted IAM condition body.
	RedactionReasonIAMCondition = "gcp_iam_condition"
)

// Image reference confidence (from image_reference.go). It classifies how
// strongly an image reference is anchored. Digest-anchored evidence is
// required before the reducer admits image evidence into deployment or
// vulnerability paths.
const (
	// ImageConfidenceDigest means the reference is anchored to a content digest.
	ImageConfidenceDigest = "digest"
	// ImageConfidenceTag means the reference is tag-only (lower confidence).
	ImageConfidenceTag = "tag"
)

// RedactionReasonTagValue marks a fingerprinted GCP tag/label value (from
// tag_observation.go).
const RedactionReasonTagValue = "gcp_tag_value"

// TagSourceKindLabel is the tag source kind stamped on tag observations
// derived from CAI resource labels (from generation.go).
const TagSourceKindLabel = "label"
