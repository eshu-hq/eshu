// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/extractor"
)

// Compat aliases for the extractor shared foundation (issue #6696, wave 1).
//
// The registry, claim boundary, relationship observation, and shared extractor
// helpers moved to the extractor leaf package so extractor group subpackages
// (starting with extractor/serverless) can depend on them without importing
// this root package. These aliases keep every unmoved caller in this package
// compiling unchanged; each entry names the leaf owner. Delete an entry once
// its last caller moves out of the root (see extractor/AGENTS.md).
//
// Type aliases preserve identity, so exported signatures and facts payloads
// are unchanged; const and func aliases preserve values and behavior.

// Claim boundary and scope (see extractor/bounds.go).
type (
	// Boundary is the durable scope-generation and claim identity shared by
	// all facts emitted for one GCP collector claim.
	Boundary = extractor.Boundary
	// ParentScopeKind enumerates the CAI parent scope kinds a GCP collector
	// claim can shard by.
	ParentScopeKind = extractor.ParentScopeKind
)

const (
	// ParentScopeOrganization is an organization-level CAI parent scope.
	ParentScopeOrganization = extractor.ParentScopeOrganization
	// ParentScopeFolder is a folder-level CAI parent scope.
	ParentScopeFolder = extractor.ParentScopeFolder
	// ParentScopeProject is a project-level CAI parent scope.
	ParentScopeProject = extractor.ParentScopeProject
)

// Extractor registry and dispatch (see extractor/registry.go).
type (
	// AttributeExtraction is the bounded, redaction-safe typed-depth output
	// of a per-asset-type extractor.
	AttributeExtraction = extractor.AttributeExtraction
	// ExtractContext is the bounded input handed to a per-asset-type
	// extractor.
	ExtractContext = extractor.ExtractContext
	// AssetAttributeExtractor extracts bounded typed depth from one CAI
	// resource data blob for a single asset type.
	AssetAttributeExtractor = extractor.AssetAttributeExtractor
)

var (
	// RegisterAssetExtractor installs the extractor for one CAI asset type.
	RegisterAssetExtractor = extractor.RegisterAssetExtractor
	// LookupAssetExtractor returns the registered extractor for an asset
	// type.
	lookupAssetExtractor = extractor.LookupAssetExtractor
	// HasAssetExtractor reports whether a typed-depth extractor is registered
	// for an asset type.
	HasAssetExtractor = extractor.HasAssetExtractor
	// extractAssetAttributes dispatches to the registered extractor for the
	// context asset type.
	extractAssetAttributes = extractor.ExtractAssetAttributes
	// unregisterAssetExtractorForTest removes a registration; test-only.
	unregisterAssetExtractorForTest = extractor.UnregisterAssetExtractorForTest
)

// Relationship observation (see extractor/relationships.go).
type (
	// RelationshipObservation is one provider-observed relationship between
	// two GCP resources.
	RelationshipObservation = extractor.RelationshipObservation
)

const (
	// RelationshipSupportSupported means the provider returned a complete
	// relationship between two resources.
	RelationshipSupportSupported = extractor.RelationshipSupportSupported
	// RelationshipSupportPartial means the relationship was observed but the
	// target is opaque or outside the readable boundary.
	RelationshipSupportPartial = extractor.RelationshipSupportPartial
	// RelationshipSupportUnsupported means the relationship type or tier is
	// not fully supported.
	RelationshipSupportUnsupported = extractor.RelationshipSupportUnsupported
)

// Shared extractor helpers (see extractor/shared.go).
var (
	// cmekKeyFullResourceName normalizes a CMEK reference into a Cloud KMS
	// CAI full resource name.
	cmekKeyFullResourceName = extractor.CMEKKeyFullResourceName
	// dedupeNonEmpty trims, drops blanks, and deduplicates a string slice.
	dedupeNonEmpty = extractor.DedupeNonEmpty
	// dedupeSortedNonEmpty is dedupeNonEmpty composed with a final sort.
	dedupeSortedNonEmpty = extractor.DedupeSortedNonEmpty
	// firstNonEmpty returns the first trimmed non-blank value.
	firstNonEmpty = extractor.FirstNonEmpty
	// imageDigestFromReference extracts the digest from a container image
	// reference.
	imageDigestFromReference = extractor.ImageDigestFromReference
	// normalizeRFC3339 parses a compute API RFC3339 timestamp into whole-second
	// RFC3339 UTC.
	normalizeRFC3339 = extractor.NormalizeRFC3339
)

// Cross-group asset types and prefixes (see extractor/shared.go and
// extractor/names.go).
const (
	// assetTypeKMSCryptoKey is the CAI asset type for a Cloud KMS CryptoKey.
	assetTypeKMSCryptoKey = extractor.AssetTypeKMSCryptoKey
	// assetTypeStorageBucket is the CAI asset type for a Cloud Storage bucket.
	assetTypeStorageBucket = extractor.AssetTypeStorageBucket
	// assetTypeRunService is the CAI asset type for a Cloud Run service.
	assetTypeRunService = extractor.AssetTypeRunService
	// assetTypeRunJob is the CAI asset type for a Cloud Run job.
	assetTypeRunJob = extractor.AssetTypeRunJob
	// assetTypeGKECluster is the CAI asset type for a GKE cluster.
	assetTypeGKECluster = extractor.AssetTypeGKECluster
	// assetTypeComputeNetwork is the CAI asset type for a Compute Engine VPC
	// network.
	assetTypeComputeNetwork = extractor.AssetTypeComputeNetwork
	// assetTypeComputeSubnetwork is the CAI asset type for a Compute Engine
	// subnetwork.
	assetTypeComputeSubnetwork = extractor.AssetTypeComputeSubnetwork
	// assetTypePubSubTopic is the CAI asset type for a Pub/Sub topic.
	assetTypePubSubTopic = extractor.AssetTypePubSubTopic
	// secretManagerSecretAssetType is the CAI asset type for a Secret Manager
	// secret.
	secretManagerSecretAssetType = extractor.SecretManagerSecretAssetType
	// cloudKMSResourceNamePrefix is the CAI full-resource-name prefix for
	// Cloud KMS keys.
	cloudKMSResourceNamePrefix = extractor.CloudKMSResourceNamePrefix
	// storageBucketResourceNamePrefixFmt builds a bucket full resource name
	// by concatenation.
	storageBucketResourceNamePrefixFmt = extractor.StorageBucketResourceNamePrefixFmt
	// computeResourceNamePrefix is the CAI full-resource-name prefix for
	// Compute Engine resources.
	computeResourceNamePrefix = extractor.ComputeResourceNamePrefix
	// pubSubResourceNamePrefix is the CAI full-resource-name prefix for
	// Pub/Sub resources.
	pubSubResourceNamePrefix = extractor.PubSubResourceNamePrefix
	// runServiceResourceNamePrefix is the CAI full-resource-name prefix for
	// Cloud Run services.
	runServiceResourceNamePrefix = extractor.RunServiceResourceNamePrefix
	// defaultServiceAccountSentinel is the GKE API's sentinel value for "use
	// the default service account".
	defaultServiceAccountSentinel = extractor.DefaultServiceAccountSentinel
)

// Cross-group resource-name builders (see extractor/names.go).
var (
	// secretManagerTopicFullName builds a CAI Pub/Sub Topic full resource
	// name from a Secret Manager topic reference.
	secretManagerTopicFullName = extractor.SecretManagerTopicFullName
	// pubSubTopicRefFullName builds a CAI Pub/Sub Topic full resource name
	// from a topic reference.
	pubSubTopicRefFullName = extractor.PubSubTopicRefFullName
	// pubSubPushEndpointHostFingerprint digests a push endpoint host.
	pubSubPushEndpointHostFingerprint = extractor.PubSubPushEndpointHostFingerprint
	// gcsBucketFromURI extracts the bucket name from a gs:// URI.
	gcsBucketFromURI = extractor.GCSBucketFromURI
	// gitRemoteHost extracts the lowercase host from a git remote reference.
	gitRemoteHost = extractor.GitRemoteHost
	// computeFullResourceNameFromSelfLink builds a CAI full resource name
	// from a Compute Engine reference.
	computeFullResourceNameFromSelfLink = extractor.ComputeFullResourceNameFromSelfLink
	// cloudFunctionProjectLocation extracts project and location from a Cloud
	// Functions full resource name.
	cloudFunctionProjectLocation = extractor.CloudFunctionProjectLocation
	// eventarcProjectLocation extracts project and location from a CAI full
	// resource name.
	eventarcProjectLocation = extractor.EventarcProjectLocation
)
