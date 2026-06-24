// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdbelastic

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "docdbelastic".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceDocDBElastic + "  "
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{
		ARN:              testClusterARN,
		Name:             "analytics",
		KMSKeyID:         testKMSARN,
		SubnetIDs:        []string{"subnet-0a1b2c3d"},
		SecurityGroupIDs: []string{"sg-0123456789abcdef0"},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceDocDBElastic; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}

// TestScannerCanonicalizesEmptyServiceKind proves an empty service_kind is
// canonicalized to the service constant, exercising the merged empty/matched
// switch arm the service_kind AST guard requires.
func TestScannerCanonicalizesEmptyServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{ARN: testClusterARN, Name: "analytics"}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceDocDBElastic; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q", got, want)
		}
	}
}
