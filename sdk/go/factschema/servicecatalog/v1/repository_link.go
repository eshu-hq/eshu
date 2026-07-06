// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// RepositoryLink is the schema-version-1 typed payload for the
// "service_catalog.repository_link" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// One RepositoryLink fact is a catalog-declared source-repository claim for a
// catalog entity. The reducer's correlation index
// (go/internal/reducer/service_catalog_correlation_index.go,
// serviceCatalogRepositoryLinkFromFact) only admits a link into its
// (Provider, EntityRef)-keyed map when EntityRef is non-blank; EntityRef is
// therefore the only required field here. Every repository-identifying field
// (RepositoryID, four URL spellings, RepositoryName) is deliberately OPTIONAL:
// a link that carries none of them is a legitimate, fully-decodable
// "name-only" catalog claim that the reducer's own business logic classifies
// as ServiceCatalogCorrelationRejected (matchServiceCatalogRepositories,
// "name-only links cannot prove ownership") — a correlation OUTCOME, not a
// decode failure. Typing any of these as required would incorrectly turn that
// intentional rejected-outcome path into an input_invalid dead-letter.
type RepositoryLink struct {
	// EntityRef is the catalog entity this repository link targets. Required:
	// the correlation index's join key, mirroring Entity.EntityRef.
	EntityRef string `json:"entity_ref"`

	// Provider names the source catalog system. Optional, same rationale as
	// Entity.Provider.
	Provider *string `json:"provider,omitempty"`

	// RepositoryID is a provider-asserted canonical Eshu repository id.
	// Optional: matchServiceCatalogRepositoryID prefers this identity when
	// present but the link is still valid without it (matched instead by URL,
	// or rejected as name-only).
	RepositoryID *string `json:"repository_id,omitempty"`

	// RepoID is a legacy repository-id wire key, read as a fallback via
	// firstNonBlank(repository_id, repo_id). Optional for the same reason as
	// RepositoryID.
	RepoID *string `json:"repo_id,omitempty"`

	// NormalizedURL is the preferred repository URL spelling among four
	// (normalized_url, repository_url, raw_url, url), matched by
	// firstNonBlank in that preference order. Optional: any of the four, or
	// none, may be present.
	NormalizedURL *string `json:"normalized_url,omitempty"`

	// RepositoryURL is the second-preference repository URL wire key.
	// Optional, same reason as NormalizedURL.
	RepositoryURL *string `json:"repository_url,omitempty"`

	// RawURL is the third-preference repository URL wire key. Optional, same
	// reason as NormalizedURL.
	RawURL *string `json:"raw_url,omitempty"`

	// URL is the least-preferred repository URL wire key. Optional, same
	// reason as NormalizedURL.
	URL *string `json:"url,omitempty"`

	// RepositoryName is a bare repository name with no URL or canonical id —
	// the "name-only" shape the reducer rejects as unable to prove ownership.
	// Optional: present alone, it still decodes; the rejection is a
	// correlation-outcome decision, not a decode failure.
	RepositoryName *string `json:"repository_name,omitempty"`

	// ServiceID is a provider-asserted Eshu service id for this link.
	// Optional: read as a decision-level fallback when the exact/derived
	// match itself does not otherwise supply one.
	ServiceID *string `json:"service_id,omitempty"`

	// WorkloadID is a provider-asserted Eshu workload id, mirroring ServiceID.
	// Optional for the same reason.
	WorkloadID *string `json:"workload_id,omitempty"`
}
