// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package applicationautoscaling

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact,
// including warning facts. The Scan switch trims only for the comparison, so
// without the write-back the padded string would leak into each fact's
// service_kind and break graph joins/filters that key on the canonical
// "applicationautoscaling". Warning observations carry the client's original
// boundary, so the scanner must overwrite their service_kind with the canonical
// value too; otherwise warning fact IDs diverge from resource/relationship facts.
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceApplicationAutoScaling + "  "
	warningBoundary := boundary
	client := fakeClient{snapshot: Snapshot{
		ScalableTargets: []ScalableTarget{{
			ServiceNamespace:  "dynamodb",
			ResourceID:        "table/orders",
			ScalableDimension: "dynamodb:table:ReadCapacityUnits",
		}},
		// Warnings carry the un-canonicalized boundary the SDK client built from
		// the caller-supplied boundary; the scanner must canonicalize it.
		Warnings: []awscloud.WarningObservation{{
			Boundary:       warningBoundary,
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Application Auto Scaling DescribeScalingPolicies throttled",
			SourceRecordID: "applicationautoscaling_scaling_policies_throttled_dynamodb",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	sawWarning := false
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSWarningFactKind {
			sawWarning = true
		}
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceApplicationAutoScaling; got != want {
			t.Fatalf("envelope (%s) service_kind = %#v, want %q (padded service_kind must be canonicalized)",
				envelope.FactKind, got, want)
		}
	}
	if !sawWarning {
		t.Fatalf("expected a warning envelope to be emitted")
	}
}
