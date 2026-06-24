// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// maxDNSRecordTargets bounds the number of fingerprinted targets carried on one
// azure_dns_record fact so a record set cannot emit an unbounded payload.
const maxDNSRecordTargets = 100

// DNSRecordObservation is one observed Azure DNS record. DNS names are sensitive,
// so the collector fingerprints the record name and every target value with the
// redaction key; it keeps the bounded record type and TTL as evidence.
type DNSRecordObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// ZoneARMResourceID is the raw ARM identity of the owning DNS zone.
	ZoneARMResourceID string
	// RecordType is the bounded DNS record type (A, AAAA, CNAME, ...).
	RecordType string
	// RecordName is the raw record name; it is fingerprinted, never stored raw.
	RecordName string
	// Targets are the raw record target values; each is fingerprinted.
	Targets []string
	// TTLSeconds is the record TTL in seconds.
	TTLSeconds int64
	// ProviderTime is the read time, or nil when absent.
	ProviderTime *time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewDNSRecordEnvelope builds the durable azure_dns_record fact for one DNS
// record. The record name and every target are fingerprinted with the redaction
// key so no DNS name text reaches durable facts; the record type and TTL stay as
// bounded evidence. The stable fact key is derived from the zone identity, record
// type, and raw record name (hashed by facts.StableID, never exposed), so it is
// independent of the redaction key.
//
// It fails closed on a missing zone, a missing record type, a missing record
// name, or a zero redaction key.
func NewDNSRecordEnvelope(observation DNSRecordObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("azure dns record observation requires a redaction key")
	}
	zoneID := strings.TrimSpace(observation.ZoneARMResourceID)
	if zoneID == "" {
		return facts.Envelope{}, fmt.Errorf("azure dns record observation requires zone_arm_resource_id")
	}
	recordType := strings.ToUpper(strings.TrimSpace(observation.RecordType))
	if recordType == "" {
		return facts.Envelope{}, fmt.Errorf("azure dns record observation requires record_type")
	}
	recordName := strings.TrimSpace(observation.RecordName)
	if recordName == "" {
		return facts.Envelope{}, fmt.Errorf("azure dns record observation requires record_name")
	}

	zone, err := ParseARMIdentity(zoneID)
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("normalize zone arm identity: %w", err)
	}
	targets, targetsTruncated := fingerprintDNSTargets(observation.Targets, recordType, key)

	stableKey := facts.StableID(facts.AzureDNSRecordFactKind, map[string]any{
		"zone_normalized_id": zone.Normalized,
		"record_type":        recordType,
		"record_name":        recordName,
		"source_lane":        observation.Boundary.SourceLane,
		"tenant_id":          observation.Boundary.TenantID,
	})

	payload := map[string]any{
		"collector_kind":           CollectorKind,
		"collector_instance_id":    observation.Boundary.CollectorInstanceID,
		"tenant_id":                observation.Boundary.TenantID,
		"scope_kind":               observation.Boundary.ScopeKind,
		"provider_scope_id":        observation.Boundary.ProviderScopeID,
		"source_lane":              observation.Boundary.SourceLane,
		"zone_arm_resource_id":     zoneID,
		"zone_normalized_id":       zone.Normalized,
		"record_type":              recordType,
		"record_name_fingerprint":  redact.String(recordName, "azure_dns_record_name", "azure_dns_record_name:"+recordType, key).Marker,
		"target_fingerprints":      targets,
		"target_count":             len(targets),
		"target_truncated":         targetsTruncated,
		"ttl_seconds":              observation.TTLSeconds,
		"provider_time":            timeOrNil(observation.ProviderTime),
		"redaction_policy_version": RedactionPolicyVersion,
	}

	return newEnvelope(
		observation.Boundary,
		facts.AzureDNSRecordFactKind,
		facts.AzureDNSRecordSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, zone.Normalized+"|"+recordType+"|"+recordName),
		observation.SourceURI,
		payload,
	), nil
}

// fingerprintDNSTargets fingerprints each non-blank DNS target, de-duplicating
// and bounding the set, returning the fingerprints and whether truncated.
func fingerprintDNSTargets(targets []string, recordType string, key redact.Key) ([]string, bool) {
	seen := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	raw := make([]string, 0, len(seen))
	for t := range seen {
		raw = append(raw, t)
	}
	sort.Strings(raw)
	truncated := false
	if len(raw) > maxDNSRecordTargets {
		raw = raw[:maxDNSRecordTargets]
		truncated = true
	}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		out = append(out, redact.String(t, "azure_dns_target", "azure_dns_target:"+recordType, key).Marker)
	}
	return out, truncated
}
