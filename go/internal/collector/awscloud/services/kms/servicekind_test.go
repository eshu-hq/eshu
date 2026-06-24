// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kms

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "kms".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceKMS + "  "
	keyID := "1234abcd-12ab-34cd-56ef-1234567890ab"
	client := fakeClient{keys: []Key{{
		ID:         keyID,
		ARN:        "arn:aws:kms:us-east-1:123456789012:key/" + keyID,
		KeyManager: "CUSTOMER",
		KeyUsage:   "ENCRYPT_DECRYPT",
		KeySpec:    "SYMMETRIC_DEFAULT",
		KeyState:   "Enabled",
		Origin:     "AWS_KMS",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceKMS; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}
