// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// OperationalLink is the schema-version-1 typed payload for the
// "service_catalog.operational_link" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Unlike Entity, Ownership, and RepositoryLink, no reducer decode call reads
// this kind: it is read only by a raw-SQL JSONB loader
// (go/internal/query/incident_context_runtime_sql.go
// listIncidentServiceCatalogOperationalLinksQuery, decoded by
// go/internal/query/incident_context_runtime_store.go
// decodeIncidentServiceCatalogOperationalLink), which the #4573
// payload-usage-manifest gate cannot see (it scans reducer decode calls only).
// It is typed here anyway, mirroring the incident family's SQL-loader-only
// field precedent (sdk/go/factschema/AGENTS.md), so this schema stays honest
// about every field a real consumer reads. No struct field is required: the
// SQL loader reads every key with StringVal, which already tolerates an
// absent key as an empty string, so nothing here gates admission — this
// struct exists purely to keep the checked-in schema truthful, not to add a
// new decode-time rejection path.
type OperationalLink struct {
	// Provider names the source catalog system. Optional: read by the SQL
	// loader as StringVal(payload, "provider"), which tolerates an absent key.
	Provider *string `json:"provider,omitempty"`

	// EntityRef is the catalog entity this operational link belongs to.
	// Optional for the same StringVal-tolerance reason as Provider; the SQL
	// loader uses it only to look up matching correlations, not as a decode
	// gate.
	EntityRef *string `json:"entity_ref,omitempty"`

	// LinkType classifies the operational link (for example "runbook",
	// "dashboard", "on-call"). Optional, same reason as Provider.
	LinkType *string `json:"link_type,omitempty"`

	// Title is the link's human-readable label. Optional, same reason as
	// Provider.
	Title *string `json:"title,omitempty"`

	// URL is the operational link's target URL. Optional: the SQL query
	// filters ON this field (fact.payload->>'url' = $1), but the loader still
	// tolerates a missing key returning no rows rather than a decode failure,
	// so it is schema-declared, not required.
	URL *string `json:"url,omitempty"`
}
