// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// OperationalLink is the schema-version-1 typed payload for the
// "service_catalog.operational_link" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Unlike Entity, Ownership, and RepositoryLink, no reducer decode call reads
// this kind: it is decoded by the query-layer incident-context read model
// (go/internal/query/incident_context_runtime_sql.go
// listIncidentServiceCatalogOperationalLinksQuery fetches the fact,
// go/internal/query/incident_context_runtime_store.go
// decodeIncidentServiceCatalogOperationalLink shapes it, through the
// go/internal/query/factschema_decode_incident.go
// decodeServiceCatalogOperationalLink seam, #4794 W2a). That query-layer seam
// is covered by the merged reducer+query payload-usage manifest gate
// (go/internal/payloadusage resolveQueryDecodeFiles), so this schema stays
// honest about every field the read model reads. No struct field is required:
// the read path derefs every optional pointer field to ""/nil on absence, so
// an absent key is a valid empty value and nothing here gates admission — the
// decode only dead-letters this kind on an unsupported schema major.
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
