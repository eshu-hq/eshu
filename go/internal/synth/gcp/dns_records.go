// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// dnsRecordEvery gates how often a synthetic DNS record is emitted relative
// to the generated resource count.
const dnsRecordEvery = 11

// syntheticRecordTypes is the bounded DNS record type vocabulary a generated
// record cycles through.
var syntheticRecordTypes = []string{"A", "AAAA", "CNAME", "TXT"}

// buildDNSRecordFacts derives one gcp_dns_record fact for every
// dnsRecordEvery-th generated resource whose asset type is a DNS managed
// zone, or — when none are present in this run's inventory slice — for every
// dnsRecordEvery-th resource overall, so a small ResourceCount still proves
// the kind. The managed zone full resource name is fabricated deterministically
// per index since DNSRecord's schema requires a managed-zone reference, not a
// join against a specific generated zone resource.
func (g *generation) buildDNSRecordFacts() ([]cassette.Fact, error) {
	var facts []cassette.Fact
	for i := range g.resources {
		if i%dnsRecordEvery != 0 {
			continue
		}
		record := g.buildOneDNSRecord(i)

		payload, err := factschema.EncodeGCPDNSRecord(record)
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: encode gcp_dns_record[%d]: %w", i, err)
		}
		fact, err := generateFact(factschema.FactKindGCPDNSRecord, factKindSchemaVersions[factschema.FactKindGCPDNSRecord], payload)
		if err != nil {
			return nil, err
		}
		fact.StableFactKey = fmt.Sprintf("gcp:project:%s:dns:%s:%s", g.opts.ProjectID, record.RecordType, record.RecordNameFingerprint)
		facts = append(facts, fact)
	}
	return facts, nil
}

// buildOneDNSRecord synthesizes one schema-valid gcpv1.DNSRecord. The raw
// record name is never carried (matching the real collector's fingerprint-only
// contract); RecordNameFingerprint is a deterministic hash of a synthetic name
// derived from index and the seed's project id, so re-running the same seed
// reproduces the same fingerprint.
func (g *generation) buildOneDNSRecord(index int) gcpv1.DNSRecord {
	managedZone := fmt.Sprintf("//dns.googleapis.com/projects/%s/managedZones/synth-zone-%d", g.opts.ProjectID, index/dnsRecordEvery)
	recordType := syntheticRecordTypes[index%len(syntheticRecordTypes)]
	recordName := fmt.Sprintf("synth-record-%d.%s.example-internal.", index, g.opts.ProjectID)

	projectID := g.opts.ProjectID
	targetFingerprint := fingerprint(fmt.Sprintf("target-%d", index))
	targetCount := int64(1)
	truncated := false
	ttl := int64(300)

	return gcpv1.DNSRecord{
		ManagedZoneFullResourceName: managedZone,
		RecordType:                  recordType,
		RecordNameFingerprint:       fingerprint(recordName),
		ManagedZoneProjectID:        &projectID,
		TargetFingerprints:          []string{targetFingerprint},
		TargetCount:                 &targetCount,
		TargetTruncated:             &truncated,
		TTLSeconds:                  &ttl,
	}
}

// fingerprint returns a deterministic, non-reversible hex digest of raw,
// mirroring the real collector's "never carry the raw value" contract for DNS
// record names and targets.
func fingerprint(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
