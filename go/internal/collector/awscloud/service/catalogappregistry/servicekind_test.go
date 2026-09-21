// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalogappregistry

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestScannerCanonicalizesPaddedServiceKindRegression proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "servicecatalogappregistry".
func TestScannerCanonicalizesPaddedServiceKindRegression(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "\t" + awscloud.ServiceServiceCatalogAppRegistry + " "
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   "app-0abc123",
		ARN:  testApplicationARN,
		Name: "payments",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceServiceCatalogAppRegistry; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q", got, want)
		}
	}
}
