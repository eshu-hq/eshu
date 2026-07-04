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
	return encodeToPayload(resource)
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
	return encodeToPayload(relationship)
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
	return encodeToPayload(warning)
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
	return encodeToPayload(record)
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
	return encodeToPayload(observation)
}
