// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testDNSRecordObservation() DNSRecordObservation {
	return DNSRecordObservation{
		Boundary:          testBoundary(),
		ZoneARMResourceID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-dns/providers/Microsoft.Network/dnsZones/example.com",
		RecordType:        "a",
		RecordName:        "api",
		Targets:           []string{"10.0.0.4", "10.0.0.5", "10.0.0.4"},
		TTLSeconds:        300,
	}
}

// TestNewDNSRecordEnvelopeFingerprintsNameAndTargets proves the DNS record name
// and every target value are fingerprinted (never raw), while the record type and
// TTL stay as bounded evidence.
func TestNewDNSRecordEnvelopeFingerprintsNameAndTargets(t *testing.T) {
	obs := testDNSRecordObservation()
	key := testRedactionKey(t)

	env, err := NewDNSRecordEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewDNSRecordEnvelope error: %v", err)
	}
	if env.FactKind != facts.AzureDNSRecordFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.Payload["record_type"] != "A" {
		t.Fatalf("record_type = %#v, want upper-cased A", env.Payload["record_type"])
	}
	nameFp, _ := env.Payload["record_name_fingerprint"].(string)
	if nameFp == "" || nameFp == obs.RecordName {
		t.Fatalf("record_name_fingerprint = %q, want non-raw marker", nameFp)
	}
	targets, ok := env.Payload["target_fingerprints"].([]string)
	if !ok || len(targets) != 2 {
		t.Fatalf("target_fingerprints = %#v, want 2 deduped", env.Payload["target_fingerprints"])
	}
	for _, m := range targets {
		if m == "10.0.0.4" || m == "10.0.0.5" {
			t.Fatalf("raw DNS target leaked: %q", m)
		}
	}
	if env.Payload["ttl_seconds"] != int64(300) {
		t.Fatalf("ttl_seconds = %#v", env.Payload["ttl_seconds"])
	}
}

// TestNewDNSRecordEnvelopeRejectsInvalid proves the builder fails closed on a
// missing zone, record type, record name, or a zero redaction key.
func TestNewDNSRecordEnvelopeRejectsInvalid(t *testing.T) {
	key := testRedactionKey(t)
	for name, mutate := range map[string]func(*DNSRecordObservation){
		"missing zone": func(o *DNSRecordObservation) { o.ZoneARMResourceID = "" },
		"missing type": func(o *DNSRecordObservation) { o.RecordType = "" },
		"missing name": func(o *DNSRecordObservation) { o.RecordName = "" },
		"negative ttl": func(o *DNSRecordObservation) { o.TTLSeconds = -1 },
		"overflow ttl": func(o *DNSRecordObservation) { o.TTLSeconds = 1 << 31 },
	} {
		obs := testDNSRecordObservation()
		mutate(&obs)
		if _, err := NewDNSRecordEnvelope(obs, key); err == nil {
			t.Fatalf("%s: error = nil, want non-nil", name)
		}
	}
	if _, err := NewDNSRecordEnvelope(testDNSRecordObservation(), redact.Key{}); err == nil {
		t.Fatal("zero key: error = nil, want non-nil")
	}
}
