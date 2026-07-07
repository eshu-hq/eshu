// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// DecodeAzureCloudResource decodes env.Payload into the latest
// azurev1.CloudResource struct for the "azure_cloud_resource" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2. Callers
// (reducer handlers) receive either the decoded struct or a classified
// *DecodeError; they must never substitute a zero-value struct on error.
func DecodeAzureCloudResource(env Envelope) (azurev1.CloudResource, error) {
	return decodeLatestMajor[azurev1.CloudResource](FactKindAzureCloudResource, env)
}

// EncodeAzureCloudResource marshals an azurev1.CloudResource into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAzureCloudResource for schema-version-1 payloads, used by collectors
// emitting this fact kind and by this module's round-trip tests.
func EncodeAzureCloudResource(resource azurev1.CloudResource) (map[string]any, error) {
	payload := map[string]any{
		"arm_resource_id": resource.ARMResourceID,
		"resource_type":   resource.ResourceType,
		"subscription_id": resource.SubscriptionID,
		"location":        resource.Location,
	}
	addStringPtr(payload, "normalized_resource_id", resource.NormalizedResourceID)
	addStringPtr(payload, "resource_name", resource.ResourceName)
	addStringPtr(payload, "provider_namespace", resource.ProviderNamespace)
	mergeUnknownPayloadKeys(payload, resource.Attributes, azureCloudResourcePayloadKeys)
	return payload, nil
}

// DecodeAzureCloudRelationship decodes env.Payload into the latest
// azurev1.CloudRelationship struct for the "azure_cloud_relationship" fact
// kind. See DecodeAzureCloudResource for the dispatch and error contract.
func DecodeAzureCloudRelationship(env Envelope) (azurev1.CloudRelationship, error) {
	return decodeLatestMajor[azurev1.CloudRelationship](FactKindAzureCloudRelationship, env)
}

// EncodeAzureCloudRelationship marshals an azurev1.CloudRelationship into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAzureCloudRelationship for schema-version-1 payloads.
func EncodeAzureCloudRelationship(relationship azurev1.CloudRelationship) (map[string]any, error) {
	payload := map[string]any{
		"relationship_type":      relationship.RelationshipType,
		"source_arm_resource_id": relationship.SourceARMResourceID,
		"target_arm_resource_id": relationship.TargetARMResourceID,
	}
	addStringPtr(payload, "source_normalized_resource_id", relationship.SourceNormalizedResourceID)
	addStringPtr(payload, "target_normalized_resource_id", relationship.TargetNormalizedResourceID)
	addStringPtr(payload, "target_resource_type", relationship.TargetResourceType)
	addStringPtr(payload, "support_state", relationship.SupportState)
	mergeUnknownPayloadKeys(payload, relationship.Attributes, azureCloudRelationshipPayloadKeys)
	return payload, nil
}

// DecodeAzureDNSRecord decodes env.Payload into the latest azurev1.DNSRecord
// struct for the "azure_dns_record" fact kind. See DecodeAzureCloudResource
// for the dispatch and error contract.
func DecodeAzureDNSRecord(env Envelope) (azurev1.DNSRecord, error) {
	return decodeLatestMajor[azurev1.DNSRecord](FactKindAzureDNSRecord, env)
}

// EncodeAzureDNSRecord marshals an azurev1.DNSRecord into the map[string]any
// payload shape an Envelope carries. It is the inverse of
// DecodeAzureDNSRecord for schema-version-1 payloads.
func EncodeAzureDNSRecord(record azurev1.DNSRecord) (map[string]any, error) {
	payload := map[string]any{
		"zone_arm_resource_id":    record.ZoneARMResourceID,
		"record_type":             record.RecordType,
		"record_name_fingerprint": record.RecordNameFingerprint,
	}
	addStringPtr(payload, "zone_normalized_id", record.ZoneNormalizedID)
	addStringSlice(payload, "target_fingerprints", record.TargetFingerprints)
	addInt32Ptr(payload, "target_count", record.TargetCount)
	addBoolPtr(payload, "target_truncated", record.TargetTruncated)
	addInt32Ptr(payload, "ttl_seconds", record.TTLSeconds)
	addStringPtr(payload, "provider_time", record.ProviderTime)
	addStringPtr(payload, "redaction_policy_version", record.RedactionPolicyVersion)
	return payload, nil
}

// DecodeAzureCollectionWarning decodes env.Payload into the latest
// azurev1.CollectionWarning struct for the "azure_collection_warning" fact
// kind. See DecodeAzureCloudResource for the dispatch and error contract.
func DecodeAzureCollectionWarning(env Envelope) (azurev1.CollectionWarning, error) {
	return decodeLatestMajor[azurev1.CollectionWarning](FactKindAzureCollectionWarning, env)
}

// EncodeAzureCollectionWarning marshals an azurev1.CollectionWarning into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAzureCollectionWarning for schema-version-1 payloads.
func EncodeAzureCollectionWarning(warning azurev1.CollectionWarning) (map[string]any, error) {
	payload := map[string]any{
		"warning_kind": warning.WarningKind,
		"outcome":      warning.Outcome,
	}
	addStringPtr(payload, "resource_family", warning.ResourceFamily)
	addBoolPtr(payload, "retryable", warning.Retryable)
	addInt32Ptr(payload, "hidden_resource_count", warning.HiddenResourceCount)
	addStringPtr(payload, "message", warning.Message)
	return payload, nil
}

// DecodeAzureTagObservation decodes env.Payload into azurev1.TagObservation.
func DecodeAzureTagObservation(env Envelope) (azurev1.TagObservation, error) {
	return decodeLatestMajor[azurev1.TagObservation](FactKindAzureTagObservation, env)
}

// EncodeAzureTagObservation maps an azurev1.TagObservation directly to a payload.
func EncodeAzureTagObservation(observation azurev1.TagObservation) (map[string]any, error) {
	payload := map[string]any{
		"arm_resource_id":        observation.ARMResourceID,
		"normalized_resource_id": observation.NormalizedResourceID,
		"resource_type":          observation.ResourceType,
		"tag_value_fingerprints": observation.TagValueFingerprints,
	}
	addStringPtr(payload, "subscription_id", observation.SubscriptionID)
	addStringPtr(payload, "resource_group", observation.ResourceGroup)
	addStringPtr(payload, "provider_namespace", observation.ProviderNamespace)
	addStringPtr(payload, "resource_name", observation.ResourceName)
	addStringSlice(payload, "tag_keys", observation.TagKeys)
	addIntPtr(payload, "tag_count", observation.TagCount)
	addBoolPtr(payload, "tag_truncated", observation.TagTruncated)
	addStringPtr(payload, "provider_time", observation.ProviderTime)
	addStringPtr(payload, "redaction_policy_version", observation.RedactionPolicyVersion)
	return payload, nil
}

// DecodeAzureIdentityObservation decodes env.Payload into azurev1.IdentityObservation.
func DecodeAzureIdentityObservation(env Envelope) (azurev1.IdentityObservation, error) {
	return decodeLatestMajor[azurev1.IdentityObservation](FactKindAzureIdentityObservation, env)
}

// EncodeAzureIdentityObservation maps an azurev1.IdentityObservation directly to a payload.
func EncodeAzureIdentityObservation(observation azurev1.IdentityObservation) (map[string]any, error) {
	payload := map[string]any{
		"arm_resource_id":        observation.ARMResourceID,
		"normalized_resource_id": observation.NormalizedResourceID,
		"resource_type":          observation.ResourceType,
		"identity_type":          observation.IdentityType,
	}
	addStringPtr(payload, "role_class", observation.RoleClass)
	addStringPtr(payload, "assignment_scope", observation.AssignmentScope)
	addStringPtr(payload, "principal_fingerprint", observation.PrincipalFingerprint)
	addStringPtr(payload, "client_fingerprint", observation.ClientFingerprint)
	addStringPtr(payload, "object_fingerprint", observation.ObjectFingerprint)
	addStringPtr(payload, "tenant_fingerprint", observation.TenantFingerprint)
	addStringPtr(payload, "provider_time", observation.ProviderTime)
	addStringPtr(payload, "redaction_policy_version", observation.RedactionPolicyVersion)
	return payload, nil
}

// DecodeAzureResourceChange decodes env.Payload into azurev1.ResourceChange.
func DecodeAzureResourceChange(env Envelope) (azurev1.ResourceChange, error) {
	return decodeLatestMajor[azurev1.ResourceChange](FactKindAzureResourceChange, env)
}

// EncodeAzureResourceChange maps an azurev1.ResourceChange directly to a payload.
func EncodeAzureResourceChange(change azurev1.ResourceChange) (map[string]any, error) {
	payload := map[string]any{
		"target_arm_resource_id": change.TargetARMResourceID,
		"target_normalized_id":   change.TargetNormalizedID,
		"target_resource_type":   change.TargetResourceType,
		"change_type":            change.ChangeType,
		"change_time":            change.ChangeTime,
	}
	addStringPtr(payload, "operation", change.Operation)
	addStringPtr(payload, "client_type", change.ClientType)
	addStringPtr(payload, "actor_class", change.ActorClass)
	addStringPtr(payload, "actor_fingerprint", change.ActorFingerprint)
	addStringSlice(payload, "changed_property_paths", change.ChangedPropertyPaths)
	addIntPtr(payload, "changed_property_count", change.ChangedPropertyCount)
	addBoolPtr(payload, "changed_property_truncated", change.ChangedPropertyTruncated)
	addBoolPtr(payload, "is_tombstone_candidate", change.IsTombstoneCandidate)
	addStringPtr(payload, "redaction_policy_version", change.RedactionPolicyVersion)
	return payload, nil
}

// DecodeAzureImageReference decodes env.Payload into azurev1.ImageReference.
func DecodeAzureImageReference(env Envelope) (azurev1.ImageReference, error) {
	return decodeLatestMajor[azurev1.ImageReference](FactKindAzureImageReference, env)
}

// EncodeAzureImageReference maps an azurev1.ImageReference directly to a payload.
func EncodeAzureImageReference(reference azurev1.ImageReference) (map[string]any, error) {
	payload := map[string]any{
		"owning_arm_resource_id": reference.OwningARMResourceID,
		"owning_normalized_id":   reference.OwningNormalizedID,
		"owning_resource_type":   reference.OwningResourceType,
		"tag_digest_confidence":  reference.TagDigestConfidence,
	}
	addStringPtr(payload, "image_reference", reference.ImageReference)
	addStringPtr(payload, "image_digest", reference.ImageDigest)
	addStringPtr(payload, "container_name_fingerprint", reference.ContainerNameFingerprint)
	addStringPtr(payload, "provider_time", reference.ProviderTime)
	addStringPtr(payload, "redaction_policy_version", reference.RedactionPolicyVersion)
	return payload, nil
}

var azureCloudResourcePayloadKeys = map[string]struct{}{
	"arm_resource_id": {}, "resource_type": {}, "subscription_id": {}, "location": {},
	"normalized_resource_id": {}, "resource_name": {}, "provider_namespace": {},
}

var azureCloudRelationshipPayloadKeys = map[string]struct{}{
	"relationship_type": {}, "source_arm_resource_id": {}, "target_arm_resource_id": {},
	"source_normalized_resource_id": {}, "target_normalized_resource_id": {},
	"target_resource_type": {}, "support_state": {},
}
