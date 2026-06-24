// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicediscovery

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "servicediscovery".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	scanner := Scanner{Client: fakeClient{namespaces: inventory()}}
	envelopes, err := scanner.Scan(context.Background(), awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         "  " + awscloud.ServiceServiceDiscovery + "  ",
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:servicediscovery:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceServiceDiscovery; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}
