// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
)

// DecodeServiceCatalogEntity decodes env.Payload into the latest
// servicecatalogv1.Entity struct for the "service_catalog.entity" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2. Callers
// (reducer handlers) receive either the decoded struct or a classified
// *DecodeError; they must never substitute a zero-value struct on error.
func DecodeServiceCatalogEntity(env Envelope) (servicecatalogv1.Entity, error) {
	return decodeLatestMajor[servicecatalogv1.Entity](FactKindServiceCatalogEntity, env)
}

// EncodeServiceCatalogEntity marshals a servicecatalogv1.Entity into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeServiceCatalogEntity for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeServiceCatalogEntity(entity servicecatalogv1.Entity) (map[string]any, error) {
	return encodeToPayload(entity)
}

// DecodeServiceCatalogOwnership decodes env.Payload into the latest
// servicecatalogv1.Ownership struct for the "service_catalog.ownership" fact
// kind. See DecodeServiceCatalogEntity for the dispatch and error contract.
func DecodeServiceCatalogOwnership(env Envelope) (servicecatalogv1.Ownership, error) {
	return decodeLatestMajor[servicecatalogv1.Ownership](FactKindServiceCatalogOwnership, env)
}

// EncodeServiceCatalogOwnership marshals a servicecatalogv1.Ownership into
// the map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeServiceCatalogOwnership for schema-version-1 payloads.
func EncodeServiceCatalogOwnership(ownership servicecatalogv1.Ownership) (map[string]any, error) {
	return encodeToPayload(ownership)
}

// DecodeServiceCatalogRepositoryLink decodes env.Payload into the latest
// servicecatalogv1.RepositoryLink struct for the
// "service_catalog.repository_link" fact kind. See DecodeServiceCatalogEntity
// for the dispatch and error contract.
func DecodeServiceCatalogRepositoryLink(env Envelope) (servicecatalogv1.RepositoryLink, error) {
	return decodeLatestMajor[servicecatalogv1.RepositoryLink](FactKindServiceCatalogRepositoryLink, env)
}

// EncodeServiceCatalogRepositoryLink marshals a servicecatalogv1.RepositoryLink
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeServiceCatalogRepositoryLink for schema-version-1
// payloads.
func EncodeServiceCatalogRepositoryLink(link servicecatalogv1.RepositoryLink) (map[string]any, error) {
	return encodeToPayload(link)
}

// DecodeServiceCatalogOperationalLink decodes env.Payload into the latest
// servicecatalogv1.OperationalLink struct for the
// "service_catalog.operational_link" fact kind. See DecodeServiceCatalogEntity
// for the dispatch and error contract. Unlike the other three kinds in this
// file, no reducer decode call uses this function today; it exists so a
// future SQL-to-decode-seam conversion of
// go/internal/query/incident_context_runtime_store.go has a validated seam
// ready, and so the checked-in schema stays honest against that loader's
// field reads (servicecatalogv1's package doc).
func DecodeServiceCatalogOperationalLink(env Envelope) (servicecatalogv1.OperationalLink, error) {
	return decodeLatestMajor[servicecatalogv1.OperationalLink](FactKindServiceCatalogOperationalLink, env)
}

// EncodeServiceCatalogOperationalLink marshals a
// servicecatalogv1.OperationalLink into the map[string]any payload shape an
// Envelope carries. It is the inverse of DecodeServiceCatalogOperationalLink
// for schema-version-1 payloads.
func EncodeServiceCatalogOperationalLink(link servicecatalogv1.OperationalLink) (map[string]any, error) {
	return encodeToPayload(link)
}
