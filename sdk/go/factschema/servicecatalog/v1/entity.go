// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Entity is the schema-version-1 typed payload for the "service_catalog.entity"
// fact kind (Contract System v1 §3.1, docs/internal/design/contract-system-v1.md).
//
// One Entity fact is a provider-native catalog record (Backstage Component,
// Cortex service, or an equivalent catalog entry) the service_catalog
// collector normalizes. The reducer's correlation index
// (go/internal/reducer/service_catalog_correlation_index.go,
// serviceCatalogEntityFromFact) keys every entity by (Provider, EntityRef);
// EntityRef is the ONLY payload field the join identity requires. Provider is
// schema-declared and read by the same join key, but a present-but-empty
// provider is a legitimate single-provider catalog observation, not a
// malformed fact, so it stays optional here — exactly like the pre-migration
// payloadString(...) read, which returned "" for an absent key without
// rejecting the fact.
type Entity struct {
	// EntityRef is the catalog entity's stable reference (for example
	// "component:default/checkout"). Required: it is the correlation index's
	// join key together with Provider, and the index drops any entity whose
	// entity_ref is blank (buildServiceCatalogCorrelationIndex,
	// serviceCatalogEntityFromFact) — a fact missing it carries no usable
	// catalog identity at all.
	EntityRef string `json:"entity_ref"`

	// Provider names the source catalog system (for example "backstage",
	// "cortex"). Optional: the join key includes it, but an empty provider is
	// a valid single-provider deployment's observation, matching the
	// pre-migration payloadString read.
	Provider *string `json:"provider,omitempty"`

	// EntityType classifies the catalog entity (for example "component",
	// "service", "resource"). Optional: read to gate the repo-local
	// admitted-service-id derivation (serviceCatalogAdmittedServiceID), but a
	// blank value simply fails that gate rather than invalidating the fact.
	EntityType *string `json:"entity_type,omitempty"`

	// DisplayName is the catalog entity's human-readable name. Optional:
	// carried through to the correlation decision for display only.
	DisplayName *string `json:"display_name,omitempty"`

	// Lifecycle is the catalog-declared lifecycle stage (for example
	// "production", "experimental"). Optional: carried through for display and
	// downstream evidence, never used as join or admission identity.
	Lifecycle *string `json:"lifecycle,omitempty"`

	// Tier is the catalog-declared criticality tier. Optional, same rationale
	// as Lifecycle.
	Tier *string `json:"tier,omitempty"`

	// ServiceID is a provider-asserted Eshu service id, when the catalog
	// source already knows Eshu's own service identity. Optional: read as a
	// fallback service id source when the repo-local repository-scope match
	// path does not otherwise admit one.
	ServiceID *string `json:"service_id,omitempty"`

	// WorkloadID is a provider-asserted Eshu workload id, mirroring ServiceID.
	// Optional for the same reason.
	WorkloadID *string `json:"workload_id,omitempty"`
}
