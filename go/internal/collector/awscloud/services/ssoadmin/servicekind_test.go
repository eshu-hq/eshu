// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssoadmin

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "ssoadmin".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceSSOAdmin + "  "
	client := fakeClient{snapshot: Snapshot{
		Instances: []Instance{{
			ARN:             "arn:aws:sso:::instance/ssoins-1111111111111111",
			IdentityStoreID: "d-9999999999",
			Name:            "primary",
			OwnerAccountID:  "123456789012",
			Status:          "ACTIVE",
		}},
	}}

	envelopes, err := newTestScanner(t, client).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceSSOAdmin; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}
