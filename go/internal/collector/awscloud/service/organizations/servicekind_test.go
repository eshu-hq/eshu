// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package organizations

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
// joins/filters that key on the canonical "organizations".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceOrganizations + "  "
	key := testRedactionKey(t)
	client := fakeClient{snapshot: Snapshot{
		Organization: Organization{
			ID:                "o-exampleorgid",
			ARN:               "arn:aws:organizations::123456789012:organization/o-exampleorgid",
			ManagementAccount: "123456789012",
			FeatureSet:        "ALL",
		},
		Roots: []Root{{
			ID:   "r-root",
			ARN:  "arn:aws:organizations::123456789012:root/o-exampleorgid/r-root",
			Name: "Root",
		}},
		Accounts: []Account{{
			ID:        "123456789012",
			ARN:       "arn:aws:organizations::123456789012:account/o-exampleorgid/123456789012",
			Email:     "root@example.com",
			Name:      "management",
			Status:    "ACTIVE",
			ParentID:  "r-root",
			JoinedAt:  time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
			JoinedVia: "CREATED",
		}},
	}}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceOrganizations; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}
