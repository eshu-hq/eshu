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
