// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"testing"
	"time"
)

// TestLConstantsMatchGCPCloudValues pins every L-slice constant consolidated
// into this catalog leaf to the exact value it carried in package gcpcloud
// (root files, the extractor shared leaf, gcpruntime, and freshness) before
// the #6696 consolidation. The consolidation moves declarations only: any
// value drift here is a behavior change, not a move.
func TestLConstantsMatchGCPCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "ParentScopeOrganization", got: string(ParentScopeOrganization), want: "organization"},
		{name: "ParentScopeFolder", got: string(ParentScopeFolder), want: "folder"},
		{name: "ParentScopeProject", got: string(ParentScopeProject), want: "project"},
		{name: "RelationshipSupportSupported", got: RelationshipSupportSupported, want: "supported"},
		{name: "RelationshipSupportPartial", got: RelationshipSupportPartial, want: "partial"},
		{name: "RelationshipSupportUnsupported", got: RelationshipSupportUnsupported, want: "unsupported"},
		{name: "AssetTypeGKECluster", got: AssetTypeGKECluster, want: "container.googleapis.com/Cluster"},
		{name: "AssetTypeComputeNetwork", got: AssetTypeComputeNetwork, want: "compute.googleapis.com/Network"},
		{name: "AssetTypeComputeSubnetwork", got: AssetTypeComputeSubnetwork, want: "compute.googleapis.com/Subnetwork"},
		{name: "AssetTypePubSubTopic", got: AssetTypePubSubTopic, want: "pubsub.googleapis.com/Topic"},
		{name: "SecretManagerSecretAssetType", got: SecretManagerSecretAssetType, want: "secretmanager.googleapis.com/Secret"},
		{name: "ComputeResourceNamePrefix", got: ComputeResourceNamePrefix, want: "//compute.googleapis.com/"},
		{name: "PubSubResourceNamePrefix", got: PubSubResourceNamePrefix, want: "//pubsub.googleapis.com/"},
		{name: "DefaultServiceAccountSentinel", got: DefaultServiceAccountSentinel, want: "default"},
		{name: "PubSubDeletedTopicSentinel", got: PubSubDeletedTopicSentinel, want: "_deleted-topic_"},
		{name: "RunServiceResourceNamePrefix", got: RunServiceResourceNamePrefix, want: "//run.googleapis.com/"},
		{name: "ComputeSelfLinkMarker", got: ComputeSelfLinkMarker, want: "/compute/"},
		{name: "CloudKMSResourceNamePrefix", got: CloudKMSResourceNamePrefix, want: "//cloudkms.googleapis.com/"},
		{name: "AssetTypeKMSCryptoKey", got: AssetTypeKMSCryptoKey, want: "cloudkms.googleapis.com/CryptoKey"},
		{name: "AssetTypeStorageBucket", got: AssetTypeStorageBucket, want: "storage.googleapis.com/Bucket"},
		{name: "AssetTypeRunService", got: AssetTypeRunService, want: "run.googleapis.com/Service"},
		{name: "AssetTypeRunJob", got: AssetTypeRunJob, want: "run.googleapis.com/Job"},
		{name: "StorageBucketResourceNamePrefixFmt", got: StorageBucketResourceNamePrefixFmt, want: "//storage.googleapis.com/projects/_/buckets/"},
		{name: "WarningKindPartialPermission", got: WarningKindPartialPermission, want: "partial_permission"},
		{name: "WarningKindUnsupported", got: WarningKindUnsupported, want: "unsupported"},
		{name: "WarningKindQuota", got: WarningKindQuota, want: "quota"},
		{name: "WarningKindUnavailable", got: WarningKindUnavailable, want: "unavailable"},
		{name: "WarningKindStale", got: WarningKindStale, want: "stale_generation"},
		{name: "WarningKindRedaction", got: WarningKindRedaction, want: "redaction"},
		{name: "WarningKindPageTokenExpired", got: WarningKindPageTokenExpired, want: "page_token_expired"},
		{name: "OutcomePartial", got: OutcomePartial, want: "partial"},
		{name: "OutcomeUnsupported", got: OutcomeUnsupported, want: "unsupported"},
		{name: "OutcomeUnavailable", got: OutcomeUnavailable, want: "unavailable"},
		{name: "OutcomeStale", got: OutcomeStale, want: "stale"},
		{name: "ClaimStatusStarted", got: ClaimStatusStarted, want: "started"},
		{name: "ClaimStatusSucceeded", got: ClaimStatusSucceeded, want: "succeeded"},
		{name: "ClaimStatusPartial", got: ClaimStatusPartial, want: "partial"},
		{name: "ClaimStatusFailed", got: ClaimStatusFailed, want: "failed"},
		{name: "ClaimStatusRetried", got: ClaimStatusRetried, want: "retried"},
		{name: "ExtractionOutcomeExtracted", got: ExtractionOutcomeExtracted, want: "extracted"},
		{name: "ExtractionOutcomeEmpty", got: ExtractionOutcomeEmpty, want: "empty"},
		{name: "ExtensionSchemaVersionDefault", got: ExtensionSchemaVersionDefault, want: "1.0.0"},
		{name: "CollectorKind", got: CollectorKind, want: "gcp"},
		{name: "UnknownLabel", got: UnknownLabel, want: "unknown"},
		{name: "GlobalLocation", got: GlobalLocation, want: "global"},
		{name: "AssetTypeDNSResourceRecordSet", got: AssetTypeDNSResourceRecordSet, want: "dns.googleapis.com/ResourceRecordSet"},
		{name: "RedactionPolicyVersion", got: RedactionPolicyVersion, want: "gcp-cloud-2026-06-09"},
		{name: "RedactionReasonLabel", got: RedactionReasonLabel, want: "gcp_label_value"},
		{name: "RedactionReasonMember", got: RedactionReasonMember, want: "gcp_iam_member"},
		{name: "RedactionReasonDNSRecordName", got: RedactionReasonDNSRecordName, want: "gcp_dns_record_name"},
		{name: "RedactionReasonDNSTarget", got: RedactionReasonDNSTarget, want: "gcp_dns_target"},
		{name: "ServiceAccountAssetType", got: ServiceAccountAssetType, want: "iam.googleapis.com/ServiceAccount"},
		{name: "RedactionReasonIAMEtag", got: RedactionReasonIAMEtag, want: "gcp_iam_etag"},
		{name: "RedactionReasonIAMCondition", got: RedactionReasonIAMCondition, want: "gcp_iam_condition"},
		{name: "ImageConfidenceDigest", got: ImageConfidenceDigest, want: "digest"},
		{name: "ImageConfidenceTag", got: ImageConfidenceTag, want: "tag"},
		{name: "RedactionReasonTagValue", got: RedactionReasonTagValue, want: "gcp_tag_value"},
		{name: "TagSourceKindLabel", got: TagSourceKindLabel, want: "label"},
		{name: "CloudAssetInventoryEndpoint", got: CloudAssetInventoryEndpoint, want: "https://cloudasset.googleapis.com"},
		{name: "CloudAssetInventoryOAuthScope", got: CloudAssetInventoryOAuthScope, want: "https://www.googleapis.com/auth/cloud-platform"},
		{name: "CloudResourceManagerEndpoint", got: CloudResourceManagerEndpoint, want: "https://cloudresourcemanager.googleapis.com"},
		{name: "TagBindingsSourceURI", got: TagBindingsSourceURI, want: "resourcemanager://tagBindings.list"},
		{name: "EffectiveTagsSourceURI", got: EffectiveTagsSourceURI, want: "resourcemanager://effectiveTags.list"},
		{name: "TagSourceKindDirect", got: TagSourceKindDirect, want: "direct"},
		{name: "TagSourceKindEffective", got: TagSourceKindEffective, want: "effective"},
		{name: "TagInheritanceDirect", got: TagInheritanceDirect, want: "direct"},
		{name: "TagInheritanceInherited", got: TagInheritanceInherited, want: "inherited"},
		{name: "EventKindAssetChange", got: string(EventKindAssetChange), want: "asset_change"},
		{name: "EventKindAssetDeleted", got: string(EventKindAssetDeleted), want: "asset_deleted"},
		{name: "TriggerStatusQueued", got: string(TriggerStatusQueued), want: "queued"},
		{name: "TriggerStatusClaimed", got: string(TriggerStatusClaimed), want: "claimed"},
		{name: "TriggerStatusHandedOff", got: string(TriggerStatusHandedOff), want: "handed_off"},
		{name: "TriggerStatusFailed", got: string(TriggerStatusFailed), want: "failed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestLNumericConstantsMatchGCPCloudValues pins the L-slice numeric and
// duration constants (page sizes, bounds, timeouts, intervals) to their
// pre-consolidation values.
func TestLNumericConstantsMatchGCPCloudValues(t *testing.T) {
	t.Parallel()

	if got, want := DefaultLivePageSize, 100; got != want {
		t.Fatalf("DefaultLivePageSize = %d, want %d", got, want)
	}
	if got, want := MaxLivePageSize, 1000; got != want {
		t.Fatalf("MaxLivePageSize = %d, want %d", got, want)
	}
	if got, want := DefaultLiveResponseBytes, int64(16<<20); got != want {
		t.Fatalf("DefaultLiveResponseBytes = %d, want %d", got, want)
	}
	if got, want := DefaultLiveTagPageSize, 100; got != want {
		t.Fatalf("DefaultLiveTagPageSize = %d, want %d", got, want)
	}
	if got, want := MaxLiveTagPageSize, 300; got != want {
		t.Fatalf("MaxLiveTagPageSize = %d, want %d", got, want)
	}
	if got, want := MaxDNSRecordTargets, 100; got != want {
		t.Fatalf("MaxDNSRecordTargets = %d, want %d", got, want)
	}
	if got, want := MaxIAMPolicyMembers, 500; got != want {
		t.Fatalf("MaxIAMPolicyMembers = %d, want %d", got, want)
	}
	if got, want := DefaultLiveMaxAttempts, 3; got != want {
		t.Fatalf("DefaultLiveMaxAttempts = %d, want %d", got, want)
	}
	if got, want := DefaultLiveRequestTimeout, 30*time.Second; got != want {
		t.Fatalf("DefaultLiveRequestTimeout = %v, want %v", got, want)
	}
	if got, want := DefaultPollInterval, 30*time.Minute; got != want {
		t.Fatalf("DefaultPollInterval = %v, want %v", got, want)
	}
}
