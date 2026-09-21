// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package emr

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "emr".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceEMR + "  "
	client := fakeClient{
		clusters: []Cluster{{
			ARN:   "arn:aws:elasticmapreduce:us-east-1:123456789012:cluster/j-PADDED",
			ID:    "j-PADDED",
			Name:  "padded",
			State: "RUNNING",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceEMR; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}
