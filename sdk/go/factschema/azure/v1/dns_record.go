// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// DNSRecord is the schema-version-1 typed payload for the
// "azure_dns_record" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A fully typed, CLOSED schema: the collector emitter
// (azurecloud.NewDNSRecordEnvelope) fingerprints the record name and every
// target value before emission, so the payload's full shape is already
// known. DNS names are sensitive; the fingerprinted fields never carry raw
// text.
//
// Required fields mirror what the emitter validates non-empty: ZoneARM-
// ResourceID, RecordType, and RecordNameFingerprint (the emitter rejects a
// missing zone, record type, or record name before fingerprinting).
type DNSRecord struct {
	// ZoneARMResourceID is the raw ARM identity of the owning DNS zone.
	// Required.
	ZoneARMResourceID string `json:"zone_arm_resource_id"`

	// RecordType is the bounded DNS record type (A, AAAA, CNAME, MX, NS,
	// PTR, TXT, SRV, or CAA). Required.
	RecordType string `json:"record_type"`

	// RecordNameFingerprint is the keyed fingerprint of the raw record name.
	// Required: the emitter rejects a missing record name before
	// fingerprinting, so this field is always present once decode succeeds.
	RecordNameFingerprint string `json:"record_name_fingerprint"`

	// ZoneNormalizedID is the normalized form of ZoneARMResourceID. Optional
	// metadata.
	ZoneNormalizedID *string `json:"zone_normalized_id,omitempty"`

	// TargetFingerprints lists the keyed fingerprints of each observed
	// record target. Optional: a record with zero targets omits this.
	TargetFingerprints []string `json:"target_fingerprints,omitempty"`

	// TargetCount is the number of fingerprinted targets before truncation.
	// Optional pointer so nil (unreported) stays distinct from an observed
	// zero.
	TargetCount *int32 `json:"target_count,omitempty"`

	// TargetTruncated reports whether TargetFingerprints was truncated.
	// Optional.
	TargetTruncated *bool `json:"target_truncated,omitempty"`

	// TTLSeconds is the record TTL in seconds. Optional pointer so nil
	// (unreported) stays distinct from an observed zero TTL.
	TTLSeconds *int32 `json:"ttl_seconds,omitempty"`

	// ProviderTime is the read time. Optional: absent when the provider did
	// not report one.
	ProviderTime *string `json:"provider_time,omitempty"`

	// RedactionPolicyVersion is the redaction policy version the collector
	// fingerprinted the record name and targets under. Optional metadata.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`
}
