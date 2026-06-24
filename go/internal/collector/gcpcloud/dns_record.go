// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// maxDNSRecordTargets bounds the number of fingerprinted targets carried on one
// gcp_dns_record fact so a record set cannot emit an unbounded payload.
const maxDNSRecordTargets = 100

const (
	redactionReasonDNSRecordName = "gcp_dns_record_name"
	redactionReasonDNSTarget     = "gcp_dns_target"
)

// DNSRecordObservation is one observed Cloud DNS record. DNS names are sensitive,
// so the collector fingerprints the record name and every target with the
// redaction key; it keeps the bounded record type and TTL as evidence.
type DNSRecordObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// ManagedZoneFullResourceName is the CAI full resource name of the zone.
	ManagedZoneFullResourceName string
	// RecordType is the bounded DNS record type (A, AAAA, CNAME, ...).
	RecordType string
	// RecordName is the raw record name; it is fingerprinted, never stored raw.
	RecordName string
	// Targets are the raw record target values; each is fingerprinted.
	Targets []string
	// TTLSeconds is the record TTL in seconds.
	TTLSeconds int64
	// UpdateTime is the read/update time.
	UpdateTime time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewDNSRecordEnvelope builds the durable gcp_dns_record fact for one Cloud DNS
// record. The record name and every target are fingerprinted with the redaction
// key so no DNS name text reaches durable facts; the record type and TTL stay as
// bounded evidence. The stable fact key is derived from the zone identity, record
// type, and raw record name (hashed by facts.StableID, never exposed).
//
// It fails closed on a missing zone, a missing record type, a missing record
// name, or a zero redaction key.
func NewDNSRecordEnvelope(obs DNSRecordObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(obs.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("gcp dns record observation requires a redaction key")
	}
	zoneName := strings.TrimSpace(obs.ManagedZoneFullResourceName)
	if zoneName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp dns record observation requires managed_zone full_resource_name")
	}
	recordType := strings.ToUpper(strings.TrimSpace(obs.RecordType))
	if recordType == "" {
		return facts.Envelope{}, fmt.Errorf("gcp dns record observation requires record_type")
	}
	recordName := strings.TrimSpace(obs.RecordName)
	if recordName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp dns record observation requires record_name")
	}
	targets, targetsTruncated := fingerprintDNSTargets(obs.Targets, recordType, key)
	recordNameFingerprint := dnsRecordNameFingerprint(recordName, recordType, key)

	stableKey := facts.StableID(facts.GCPDNSRecordFactKind, map[string]any{
		"managed_zone_full_resource_name": zoneName,
		"record_type":                     recordType,
		"record_name":                     recordName,
		"content_family":                  obs.Boundary.ContentFamily,
	})

	payload := map[string]any{
		"collector_instance_id":           obs.Boundary.CollectorInstanceID,
		"parent_scope_kind":               string(obs.Boundary.ParentScopeKind),
		"parent_scope_id":                 obs.Boundary.ParentScopeID,
		"asset_type_family":               obs.Boundary.AssetTypeFamily,
		"content_family":                  obs.Boundary.ContentFamily,
		"location_bucket":                 obs.Boundary.LocationBucket,
		"managed_zone_full_resource_name": zoneName,
		"managed_zone_project_id":         strings.TrimSpace(ProjectIDFromFullName(zoneName)),
		"record_type":                     recordType,
		"record_name_fingerprint":         recordNameFingerprint,
		"target_fingerprints":             targets,
		"target_count":                    len(targets),
		"target_truncated":                targetsTruncated,
		"ttl_seconds":                     obs.TTLSeconds,
		"read_time":                       timeOrNil(obs.Boundary.ReadTime),
		"update_time":                     timeOrNil(obs.UpdateTime.UTC()),
		"redaction_policy_version":        RedactionPolicyVersion,
	}

	return newEnvelope(
		obs.Boundary,
		facts.GCPDNSRecordFactKind,
		facts.GCPDNSRecordSchemaVersion,
		stableKey,
		sourceRecordID(obs.SourceRecordID, zoneName+"|"+recordType+"|"+recordNameFingerprint),
		obs.SourceURI,
		payload,
	), nil
}

func dnsRecordNameFingerprint(recordName, recordType string, key redact.Key) string {
	return redact.String(recordName, redactionReasonDNSRecordName, redactionReasonDNSRecordName+":"+recordType, key).Marker
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
		out = append(out, redact.String(t, redactionReasonDNSTarget, redactionReasonDNSTarget+":"+recordType, key).Marker)
	}
	return out, truncated
}
