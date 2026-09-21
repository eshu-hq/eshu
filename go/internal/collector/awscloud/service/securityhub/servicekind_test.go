// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityhub

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "securityhub".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	key, err := redact.NewKey([]byte("securityhub-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceSecurityHub + "  "
	client := fakeClient{snapshot: Snapshot{
		Hub: Hub{
			ARN: "arn:aws:securityhub:us-east-1:123456789012:hub/default",
		},
	}}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceSecurityHub; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}
