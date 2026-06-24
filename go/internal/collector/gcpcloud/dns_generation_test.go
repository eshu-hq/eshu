// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testDNSRecordResourceObservation() ResourceObservation {
	return ResourceObservation{
		Name:       "//dns.googleapis.com/projects/123456789/locations/global/managedZones/987654321/rrsets/recordset-001",
		AssetType:  "dns.googleapis.com/ResourceRecordSet",
		Location:   "global",
		Ancestors:  []string{"projects/123456789", "organizations/9988776655"},
		UpdateTime: time.Date(2026, 6, 9, 12, 2, 0, 0, time.UTC),
		DNSRecords: []DNSRecordObservation{
			{
				ManagedZoneFullResourceName: "//dns.googleapis.com/projects/123456789/locations/global/managedZones/987654321",
				RecordType:                  "CNAME",
				RecordName:                  "svc.internal.example.",
				Targets:                     []string{"backend.internal.example.", "fallback.internal.example.", "backend.internal.example."},
				TTLSeconds:                  300,
			},
		},
	}
}

func TestGenerationBuildEmitsDNSRecordObservationsForRecordSets(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)
	gen.ObserveReadTime(time.Date(2026, 6, 9, 12, 10, 0, 0, time.UTC))

	if err := gen.AddPage([]ResourceObservation{testDNSRecordResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPDNSRecordFactKind); got != 1 {
		t.Fatalf("dns fact count = %d, want 1", got)
	}

	dns := firstFactKind(t, envelopes, facts.GCPDNSRecordFactKind)
	for _, env := range envelopes {
		payload := fmt.Sprintf("%#v", env.Payload)
		sourceRef := fmt.Sprintf("%#v", env.SourceRef)
		for _, forbidden := range []string{"svc.internal.example.", "backend.internal.example.", "fallback.internal.example."} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("raw DNS value leaked in %s payload: %s", env.FactKind, payload)
			}
			if strings.Contains(sourceRef, forbidden) {
				t.Fatalf("raw DNS value leaked in %s source ref: %s", env.FactKind, sourceRef)
			}
		}
	}
	if dns.Payload["record_type"] != "CNAME" {
		t.Fatalf("record_type = %#v, want CNAME", dns.Payload["record_type"])
	}
	if dns.Payload["read_time"] == nil {
		t.Fatal("read_time missing from DNS observation")
	}
	targets, ok := dns.Payload["target_fingerprints"].([]string)
	if !ok || len(targets) != 2 {
		t.Fatalf("target_fingerprints = %#v, want two deduped fingerprints", dns.Payload["target_fingerprints"])
	}
}

func TestGenerationBuildSkipsDNSRecordObservationsWithoutRedactionKey(t *testing.T) {
	gen := NewGeneration(testGenerationBoundary(), redact.Key{})
	if err := gen.AddPage([]ResourceObservation{testDNSRecordResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPDNSRecordFactKind); got != 0 {
		t.Fatalf("dns fact count = %d, want 0", got)
	}
}

func firstFactKind(t *testing.T, envelopes []facts.Envelope, kind string) facts.Envelope {
	t.Helper()
	for _, env := range envelopes {
		if env.FactKind == kind {
			return env
		}
	}
	t.Fatalf("missing fact kind %s", kind)
	return facts.Envelope{}
}
