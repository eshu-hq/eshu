// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Ownership is the schema-version-1 typed payload for the
// "service_catalog.ownership" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// One Ownership fact is a catalog-declared ownership claim linking a catalog
// entity to an owning team or group. The reducer's correlation index
// (go/internal/reducer/service_catalog_correlation_index.go,
// serviceCatalogOwnershipFromFact) only admits an ownership row into its
// (Provider, EntityRef)-keyed map when BOTH EntityRef and an owner reference
// are non-blank; the owner reference itself may arrive under either of two
// wire keys (owner_ref preferred, owner as a legacy fallback), matched via
// firstNonBlank. EntityRef is the required join identity here; OwnerRef and
// OwnerLegacy stay optional so a payload carrying only one of the two owner
// spellings still decodes (the reducer's own firstNonBlank already tolerates
// either being absent).
type Ownership struct {
	// EntityRef is the catalog entity this ownership claim targets. Required:
	// the correlation index's join key, mirroring Entity.EntityRef.
	EntityRef string `json:"entity_ref"`

	// Provider names the source catalog system. Optional, same rationale as
	// Entity.Provider: it participates in the join key but a blank value is a
	// legitimate single-provider observation.
	Provider *string `json:"provider,omitempty"`

	// OwnerRef is the preferred owner reference (for example
	// "group:default/payments"). Optional: the reducer prefers this key but
	// falls back to OwnerLegacy when absent (firstNonBlank), so requiring it
	// here would dead-letter a fact using only the legacy key.
	OwnerRef *string `json:"owner_ref,omitempty"`

	// OwnerLegacy is the legacy owner reference wire key ("owner"), read only
	// when OwnerRef is blank. Optional for the same reason as OwnerRef.
	OwnerLegacy *string `json:"owner,omitempty"`
}
