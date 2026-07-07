// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// DecodeGCPCloudResource decodes env.Payload into the latest gcpv1.Resource
// struct for the "gcp_cloud_resource" fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. Callers (reducer
// handlers) receive either the decoded struct or a classified *DecodeError;
// they must never substitute a zero-value struct on error.
func DecodeGCPCloudResource(env Envelope) (gcpv1.Resource, error) {
	return decodeLatestMajor[gcpv1.Resource](FactKindGCPCloudResource, env)
}

// EncodeGCPCloudResource marshals a gcpv1.Resource into the map[string]any
// payload shape an Envelope carries. It is the inverse of
// DecodeGCPCloudResource for schema-version-1 payloads, used by collectors
// emitting this fact kind and by this module's round-trip tests.
func EncodeGCPCloudResource(resource gcpv1.Resource) (map[string]any, error) {
	payload := map[string]any{
		"full_resource_name": resource.FullResourceName,
		"asset_type":         resource.AssetType,
	}
	addStringPtr(payload, "project_id", resource.ProjectID)
	addStringPtr(payload, "location", resource.Location)
	addStringPtr(payload, "display_name", resource.DisplayName)
	addStringPtr(payload, "state", resource.State)
	addStringPtr(payload, "asset_type_family", resource.AssetTypeFamily)
	addStringSlice(payload, "correlation_anchors", resource.CorrelationAnchors)
	mergeUnknownPayloadKeys(payload, resource.Attributes, gcpResourcePayloadKeys)
	return payload, nil
}

// DecodeGCPCloudRelationship decodes env.Payload into the latest
// gcpv1.Relationship struct for the "gcp_cloud_relationship" fact kind. See
// DecodeGCPCloudResource for the dispatch and error contract.
func DecodeGCPCloudRelationship(env Envelope) (gcpv1.Relationship, error) {
	return decodeLatestMajor[gcpv1.Relationship](FactKindGCPCloudRelationship, env)
}

// EncodeGCPCloudRelationship marshals a gcpv1.Relationship into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeGCPCloudRelationship for schema-version-1 payloads.
func EncodeGCPCloudRelationship(relationship gcpv1.Relationship) (map[string]any, error) {
	payload := map[string]any{
		"source_full_resource_name": relationship.SourceFullResourceName,
		"target_full_resource_name": relationship.TargetFullResourceName,
		"relationship_type":         relationship.RelationshipType,
	}
	addStringPtr(payload, "source_asset_type", relationship.SourceAssetType)
	addStringPtr(payload, "target_asset_type", relationship.TargetAssetType)
	addStringPtr(payload, "support_state", relationship.SupportState)
	mergeUnknownPayloadKeys(payload, relationship.Attributes, gcpRelationshipPayloadKeys)
	return payload, nil
}

// DecodeGCPCollectionWarning decodes env.Payload into the latest
// gcpv1.CollectionWarning struct for the "gcp_collection_warning" fact kind.
// See DecodeGCPCloudResource for the dispatch and error contract.
func DecodeGCPCollectionWarning(env Envelope) (gcpv1.CollectionWarning, error) {
	return decodeLatestMajor[gcpv1.CollectionWarning](FactKindGCPCollectionWarning, env)
}

// EncodeGCPCollectionWarning marshals a gcpv1.CollectionWarning into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeGCPCollectionWarning for schema-version-1 payloads.
func EncodeGCPCollectionWarning(warning gcpv1.CollectionWarning) (map[string]any, error) {
	payload := map[string]any{
		"warning_kind": warning.WarningKind,
		"outcome":      warning.Outcome,
	}
	addStringPtr(payload, "reason", warning.Reason)
	addBoolPtr(payload, "retryable", warning.Retryable)
	addInt64Ptr(payload, "hidden_count", warning.HiddenCount)
	return payload, nil
}

// DecodeGCPDNSRecord decodes env.Payload into the latest gcpv1.DNSRecord
// struct for the "gcp_dns_record" fact kind. See DecodeGCPCloudResource for
// the dispatch and error contract.
func DecodeGCPDNSRecord(env Envelope) (gcpv1.DNSRecord, error) {
	return decodeLatestMajor[gcpv1.DNSRecord](FactKindGCPDNSRecord, env)
}

// EncodeGCPDNSRecord marshals a gcpv1.DNSRecord into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeGCPDNSRecord
// for schema-version-1 payloads.
func EncodeGCPDNSRecord(record gcpv1.DNSRecord) (map[string]any, error) {
	payload := map[string]any{
		"managed_zone_full_resource_name": record.ManagedZoneFullResourceName,
		"record_type":                     record.RecordType,
		"record_name_fingerprint":         record.RecordNameFingerprint,
	}
	addStringPtr(payload, "managed_zone_project_id", record.ManagedZoneProjectID)
	addStringSlice(payload, "target_fingerprints", record.TargetFingerprints)
	addInt64Ptr(payload, "target_count", record.TargetCount)
	addBoolPtr(payload, "target_truncated", record.TargetTruncated)
	addInt64Ptr(payload, "ttl_seconds", record.TTLSeconds)
	return payload, nil
}

// DecodeGCPIAMPolicyObservation decodes env.Payload into the latest
// gcpv1.IAMPolicyObservation struct for the "gcp_iam_policy_observation"
// fact kind. See DecodeGCPCloudResource for the dispatch and error contract.
func DecodeGCPIAMPolicyObservation(env Envelope) (gcpv1.IAMPolicyObservation, error) {
	return decodeLatestMajor[gcpv1.IAMPolicyObservation](FactKindGCPIAMPolicyObservation, env)
}

// EncodeGCPIAMPolicyObservation marshals a gcpv1.IAMPolicyObservation into
// the map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeGCPIAMPolicyObservation for schema-version-1 payloads.
func EncodeGCPIAMPolicyObservation(observation gcpv1.IAMPolicyObservation) (map[string]any, error) {
	payload := map[string]any{
		"full_resource_name": observation.FullResourceName,
		"asset_type":         observation.AssetType,
		"role":               observation.Role,
		"members":            observation.Members,
	}
	addStringPtr(payload, "project_id", observation.ProjectID)
	addBoolPtr(payload, "condition_present", observation.ConditionPresent)
	addStringPtr(payload, "condition_fingerprint", observation.ConditionFingerprint)
	addStringPtr(payload, "etag_fingerprint", observation.EtagFingerprint)
	return payload, nil
}

// DecodeGCPTagObservation decodes env.Payload into gcpv1.TagObservation.
func DecodeGCPTagObservation(env Envelope) (gcpv1.TagObservation, error) {
	return decodeLatestMajor[gcpv1.TagObservation](FactKindGCPTagObservation, env)
}

// EncodeGCPTagObservation maps a gcpv1.TagObservation directly to a payload.
func EncodeGCPTagObservation(observation gcpv1.TagObservation) (map[string]any, error) {
	payload := map[string]any{
		"full_resource_name":     observation.FullResourceName,
		"asset_type":             observation.AssetType,
		"tag_value_fingerprints": observation.TagValueFingerprints,
	}
	addStringPtr(payload, "project_id", observation.ProjectID)
	addStringSlice(payload, "tag_keys", observation.TagKeys)
	addStringPtr(payload, "source_kind", observation.SourceKind)
	addStringMap(payload, "tag_inheritance_state", observation.TagInheritanceState)
	addStringPtr(payload, "redaction_policy_version", observation.RedactionPolicyVersion)
	return payload, nil
}

// DecodeGCPImageReference decodes env.Payload into gcpv1.ImageReference.
func DecodeGCPImageReference(env Envelope) (gcpv1.ImageReference, error) {
	return decodeLatestMajor[gcpv1.ImageReference](FactKindGCPImageReference, env)
}

// EncodeGCPImageReference maps a gcpv1.ImageReference directly to a payload.
func EncodeGCPImageReference(reference gcpv1.ImageReference) (map[string]any, error) {
	payload := map[string]any{
		"owning_full_resource_name": reference.OwningFullResourceName,
		"tag_digest_confidence":     reference.TagDigestConfidence,
	}
	addStringPtr(payload, "owning_project_id", reference.OwningProjectID)
	addStringPtr(payload, "image_reference", reference.ImageReference)
	addStringPtr(payload, "image_digest", reference.ImageDigest)
	addStringPtr(payload, "container_name_fingerprint", reference.ContainerNameFingerprint)
	addStringPtr(payload, "redaction_policy_version", reference.RedactionPolicyVersion)
	return payload, nil
}

var gcpResourcePayloadKeys = map[string]struct{}{
	"full_resource_name": {}, "asset_type": {}, "project_id": {}, "location": {},
	"display_name": {}, "state": {}, "asset_type_family": {}, "correlation_anchors": {},
}

var gcpRelationshipPayloadKeys = map[string]struct{}{
	"source_full_resource_name": {}, "target_full_resource_name": {},
	"relationship_type": {}, "source_asset_type": {}, "target_asset_type": {},
	"support_state": {},
}
