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
	return encodeToPayload(resource)
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
	return encodeToPayload(relationship)
}

// DecodeAzureTagObservation decodes env.Payload into the latest
// azurev1.TagObservation struct for the "azure_tag_observation" fact kind.
// See DecodeAzureCloudResource for the dispatch and error contract.
func DecodeAzureTagObservation(env Envelope) (azurev1.TagObservation, error) {
	return decodeLatestMajor[azurev1.TagObservation](FactKindAzureTagObservation, env)
}

// EncodeAzureTagObservation marshals an azurev1.TagObservation into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAzureTagObservation for schema-version-1 payloads.
func EncodeAzureTagObservation(observation azurev1.TagObservation) (map[string]any, error) {
	return encodeToPayload(observation)
}

// DecodeAzureIdentityObservation decodes env.Payload into the latest
// azurev1.IdentityObservation struct for the "azure_identity_observation"
// fact kind. See DecodeAzureCloudResource for the dispatch and error
// contract.
func DecodeAzureIdentityObservation(env Envelope) (azurev1.IdentityObservation, error) {
	return decodeLatestMajor[azurev1.IdentityObservation](FactKindAzureIdentityObservation, env)
}

// EncodeAzureIdentityObservation marshals an azurev1.IdentityObservation into
// the map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAzureIdentityObservation for schema-version-1 payloads.
func EncodeAzureIdentityObservation(observation azurev1.IdentityObservation) (map[string]any, error) {
	return encodeToPayload(observation)
}

// DecodeAzureResourceChange decodes env.Payload into the latest
// azurev1.ResourceChange struct for the "azure_resource_change" fact kind.
// See DecodeAzureCloudResource for the dispatch and error contract.
func DecodeAzureResourceChange(env Envelope) (azurev1.ResourceChange, error) {
	return decodeLatestMajor[azurev1.ResourceChange](FactKindAzureResourceChange, env)
}

// EncodeAzureResourceChange marshals an azurev1.ResourceChange into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAzureResourceChange for schema-version-1 payloads.
func EncodeAzureResourceChange(change azurev1.ResourceChange) (map[string]any, error) {
	return encodeToPayload(change)
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
	return encodeToPayload(record)
}

// DecodeAzureImageReference decodes env.Payload into the latest
// azurev1.ImageReference struct for the "azure_image_reference" fact kind.
// See DecodeAzureCloudResource for the dispatch and error contract.
func DecodeAzureImageReference(env Envelope) (azurev1.ImageReference, error) {
	return decodeLatestMajor[azurev1.ImageReference](FactKindAzureImageReference, env)
}

// EncodeAzureImageReference marshals an azurev1.ImageReference into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAzureImageReference for schema-version-1 payloads.
func EncodeAzureImageReference(reference azurev1.ImageReference) (map[string]any, error) {
	return encodeToPayload(reference)
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
	return encodeToPayload(warning)
}
