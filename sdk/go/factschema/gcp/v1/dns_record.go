// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// DNSRecord is the schema-version-1 typed payload for the "gcp_dns_record"
// fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// The required set matches the collector emitter (gcpcloud.NewDNSRecordEnvelope),
// which fails closed on a missing managed-zone full resource name, a missing
// record type, or a missing record name (fingerprinted into
// RecordNameFingerprint before this struct ever sees it — the raw name never
// reaches a durable fact).
type DNSRecord struct {
	// ManagedZoneFullResourceName is the CAI full resource name of the owning
	// Cloud DNS managed zone. Required.
	ManagedZoneFullResourceName string `json:"managed_zone_full_resource_name"`

	// RecordType is the bounded DNS record type (A, AAAA, CNAME, ...).
	// Required.
	RecordType string `json:"record_type"`

	// RecordNameFingerprint is a keyed fingerprint of the raw record name; the
	// raw name is never carried. Required — the emitter always produces one
	// once the record name validation passes.
	RecordNameFingerprint string `json:"record_name_fingerprint"`

	// ManagedZoneProjectID is the GCP project derived from
	// ManagedZoneFullResourceName. Optional: always emitted but may be empty.
	ManagedZoneProjectID *string `json:"managed_zone_project_id,omitempty"`

	// TargetFingerprints are keyed fingerprints of the record's raw target
	// values; raw targets are never carried. Optional: a record with zero
	// usable targets omits this.
	TargetFingerprints []string `json:"target_fingerprints,omitempty"`

	// TargetCount is the number of fingerprinted targets carried (post
	// truncation). Optional pointer so nil stays distinct from an observed
	// zero.
	TargetCount *int64 `json:"target_count,omitempty"`

	// TargetTruncated reports whether the target set exceeded the collector's
	// bound and was truncated. Optional pointer so nil (unreported) stays
	// distinct from an observed false.
	TargetTruncated *bool `json:"target_truncated,omitempty"`

	// TTLSeconds is the record TTL in seconds. Optional pointer so nil stays
	// distinct from an observed zero TTL.
	TTLSeconds *int64 `json:"ttl_seconds,omitempty"`
}
