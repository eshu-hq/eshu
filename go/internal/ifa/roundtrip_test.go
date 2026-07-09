// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestRoundTripTypedPayloadsDemoOrgOduIsBaselineGreen proves the honest-green
// case first (apirecording discipline, replay/apirecording_test.go:151-176):
// every fact the demo-org synthetic GCP cassette generates must survive
// Encode->Decode->re-Encode byte-identically before either deliberate break
// below is trusted to mean anything.
func TestRoundTripTypedPayloadsDemoOrgOduIsBaselineGreen(t *testing.T) {
	t.Parallel()

	odu := demoOrgRoundtripOdu().Odu
	if len(odu.Facts) == 0 {
		t.Fatal("odu:demo-org-roundtrip carries zero facts")
	}
	if err := RoundTripTypedPayloads(odu); err != nil {
		t.Fatalf("RoundTripTypedPayloads(odu:demo-org-roundtrip) = %v, want nil", err)
	}
}

// TestRoundTripTypedPayloadsDetectsMissingRequiredField is the deliberate
// teeth-1 break: a gcp_collection_warning payload missing its required
// "outcome" key must dead-letter as a classified *factschema.DecodeError
// with ClassificationInputInvalid, not a generic error or a silent pass —
// proving RoundTripTypedPayloads surfaces the same classified error a
// reducer handler would act on.
func TestRoundTripTypedPayloadsDetectsMissingRequiredField(t *testing.T) {
	t.Parallel()

	odu := Odu{
		Name: "odu:scratch-missing-required-field",
		Facts: []facts.Envelope{
			{
				FactKind:      factschema.FactKindGCPCollectionWarning,
				SchemaVersion: "1.0.0",
				StableFactKey: "scratch:missing-outcome",
				Payload: map[string]any{
					"warning_kind": "permission_hidden",
					// "outcome" deliberately omitted: a required field.
				},
			},
		},
	}

	err := RoundTripTypedPayloads(odu)
	if err == nil {
		t.Fatal("RoundTripTypedPayloads with a missing required field = nil error, want a classified decode error")
	}
	var decodeErr *factschema.DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("error = %v, want it to unwrap to a *factschema.DecodeError", err)
	}
	if decodeErr.Classification != factschema.ClassificationInputInvalid {
		t.Errorf("Classification = %q, want %q", decodeErr.Classification, factschema.ClassificationInputInvalid)
	}
	if decodeErr.Field != "outcome" {
		t.Errorf("Field = %q, want %q", decodeErr.Field, "outcome")
	}
}

// TestRoundTripTypedPayloadsDetectsSilentlyDroppedUnknownField is the
// deliberate teeth-2 break: gcpv1.DNSRecord (unlike Resource/Relationship)
// carries no Attributes pass-through remainder, so a top-level payload key
// with no named struct field decodes without error but is silently dropped on
// re-encode. RoundTripTypedPayloads must report this as a payload mismatch
// naming the offending fact rather than treating the drop as a decode success
// (see sdk/go/factschema/decode_map.go's decodeMapIntoWith, which only
// rebuilds an Attributes remainder when the struct declares one).
func TestRoundTripTypedPayloadsDetectsSilentlyDroppedUnknownField(t *testing.T) {
	t.Parallel()

	const stableKey = "scratch:dns-record-with-unmodeled-field"
	odu := Odu{
		Name: "odu:scratch-silently-dropped-field",
		Facts: []facts.Envelope{
			{
				FactKind:      factschema.FactKindGCPDNSRecord,
				SchemaVersion: "1.0.0",
				StableFactKey: stableKey,
				Payload: map[string]any{
					"managed_zone_full_resource_name": "//dns.googleapis.com/projects/scratch/managedZones/zone-0",
					"record_type":                     "A",
					"record_name_fingerprint":         "deadbeef",
					// DNSRecord declares no named field or Attributes remainder
					// for this key: EncodeGCPDNSRecord can never re-emit it, so
					// the round trip must report a mismatch, not a false pass.
					"unmodeled_field_not_in_dns_record_schema": "must not be silently dropped",
				},
			},
		},
	}

	err := RoundTripTypedPayloads(odu)
	if err == nil {
		t.Fatal("RoundTripTypedPayloads with an unmodeled payload key = nil error, want a round-trip mismatch")
	}
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		t.Fatalf("error = %v, want a canonical-byte mismatch, not a *factschema.DecodeError (decode must succeed; only re-encode loses the field)", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte(stableKey)) {
		t.Errorf("error = %v, want it to name the offending fact %q", err, stableKey)
	}
	if !bytes.Contains([]byte(err.Error()), []byte(factschema.FactKindGCPDNSRecord)) {
		t.Errorf("error = %v, want it to name the offending fact kind %q", err, factschema.FactKindGCPDNSRecord)
	}
}

// TestRoundTripTypedPayloadsNumberBoundary proves the "no extra number-type
// normalization" decision holds at the edge of its assumption. A JSON-parsed
// payload delivers whole numbers as float64, and RoundTripTypedPayloads
// compares that original float64 against the int64 the typed struct re-encodes.
// 2^53 is the largest integer float64 represents exactly, so it is the honest
// upper bound of "encoding/json formats a whole-number float64 identically to
// an int64": exercise a gcp_dns_record with ttl_seconds at 2^53 delivered as
// float64 and require the direct CanonicalizeValue comparator still agrees.
// Every realistic gcp count/ttl is far below this; a fact family that could
// carry a value above 2^53 would need its own boundary proof before reusing
// this comparator (see README's number-representation note).
func TestRoundTripTypedPayloadsNumberBoundary(t *testing.T) {
	t.Parallel()

	const boundary = int64(1) << 53 // 9007199254740992, float64's exact-integer limit
	odu := Odu{
		Name: "odu:scratch-number-boundary",
		Facts: []facts.Envelope{
			{
				FactKind:      factschema.FactKindGCPDNSRecord,
				SchemaVersion: "1.0.0",
				StableFactKey: "scratch:number-boundary",
				Payload: map[string]any{
					"managed_zone_full_resource_name": "//dns.googleapis.com/projects/scratch/managedZones/zone-0",
					"record_type":                     "A",
					"record_name_fingerprint":         "deadbeef",
					// Delivered as float64, exactly as a JSON parse of the cassette
					// would; decode coerces it into the *int64 field and re-encode
					// emits an int64 — the two must canonicalize byte-identically.
					"ttl_seconds": float64(boundary),
				},
			},
		},
	}
	if err := RoundTripTypedPayloads(odu); err != nil {
		t.Fatalf("RoundTripTypedPayloads with ttl_seconds=2^53 (float64) = %v, want nil: the direct comparator must agree at float64's exact-integer boundary", err)
	}
}

// TestRoundTripTypedPayloadsUnregisteredKindFailsClosed proves a fact kind
// with no entry in gcpRoundTripByKind reports an error naming the kind
// instead of silently skipping it — a coverage gap in the caller's Odù must
// never look like a passing round trip.
func TestRoundTripTypedPayloadsUnregisteredKindFailsClosed(t *testing.T) {
	t.Parallel()

	odu := Odu{
		Name: "odu:scratch-unregistered-kind",
		Facts: []facts.Envelope{
			{FactKind: "not_a_registered_gcp_kind", StableFactKey: "scratch:unregistered", Payload: map[string]any{}},
		},
	}
	err := RoundTripTypedPayloads(odu)
	if err == nil {
		t.Fatal("RoundTripTypedPayloads with an unregistered fact kind = nil error, want an error naming the kind")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("not_a_registered_gcp_kind")) {
		t.Errorf("error = %v, want it to name the unregistered kind", err)
	}
}

// TestDemoOrgRoundtripOduCanonicalizesDeterministically proves two
// independent constructions of the demo-org Odù (each freshly generating the
// synthetic GCP cassette via demoOrgRoundtripOdu, not sharing the cached
// catalogSeed instance) canonicalize to byte-identical output, so the
// underlying seeded generator and fact ordering are fully deterministic, not
// only the same in-memory slice re-serialized twice.
func TestDemoOrgRoundtripOduCanonicalizesDeterministically(t *testing.T) {
	t.Parallel()

	first, err := CanonicalizeOdu(context.Background(), demoOrgRoundtripOdu().Odu, nil)
	if err != nil {
		t.Fatalf("CanonicalizeOdu(first) error = %v", err)
	}
	second, err := CanonicalizeOdu(context.Background(), demoOrgRoundtripOdu().Odu, nil)
	if err != nil {
		t.Fatalf("CanonicalizeOdu(second) error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("two independent generations of odu:demo-org-roundtrip canonicalized differently:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
