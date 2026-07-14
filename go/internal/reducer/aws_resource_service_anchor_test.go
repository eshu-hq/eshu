// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCloudResourceNodeRowsAdmitsStrongServiceAnchor(t *testing.T) {
	t.Parallel()

	rows, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "sample-account",
			"region":        "us-east-1",
			"resource_type": "aws_vpclattice_listener",
			"resource_id":   "listener/orders-api/https",
			"name":          "https-listener",
			"attributes": map[string]any{
				"service_name": "orders-api",
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}

	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := anyToString(row["service_anchor_status"]), "strong"; got != want {
		t.Fatalf("service_anchor_status = %q, want %q", got, want)
	}
	if got, want := anyToString(row["service_name"]), "orders-api"; got != want {
		t.Fatalf("service_name = %q, want %q", got, want)
	}
	if got, want := anyToString(row["service_anchor_source"]), "attributes.service_name"; got != want {
		t.Fatalf("service_anchor_source = %q, want %q", got, want)
	}
}

func TestExtractCloudResourceNodeRowsKeepsAmbiguousServiceAnchorsOutOfStrongFields(t *testing.T) {
	t.Parallel()

	rows, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "sample-account",
			"region":        "us-east-1",
			"resource_type": "aws_vpclattice_listener",
			"resource_id":   "listener/shared/https",
			"name":          "shared-listener",
			"attributes": map[string]any{
				"service_names": []any{"orders-api", "billing-api"},
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}

	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := anyToString(row["service_anchor_status"]), "ambiguous"; got != want {
		t.Fatalf("service_anchor_status = %q, want %q", got, want)
	}
	if got := anyToString(row["service_name"]); got != "" {
		t.Fatalf("service_name = %q, want empty for ambiguous anchors", got)
	}
	if got, want := anyToString(row["service_anchor_reason"]), "multiple_service_anchors"; got != want {
		t.Fatalf("service_anchor_reason = %q, want %q", got, want)
	}
	if got, want := anyToString(row["service_anchor_name_tokens"]), "billing-api orders-api"; got != want {
		t.Fatalf("service_anchor_name_tokens = %q, want %q", got, want)
	}
}

func TestExtractCloudResourceNodeRowsDoesNotPromoteGenericAWSServiceNameAttribute(t *testing.T) {
	t.Parallel()

	rows, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "sample-account",
			"region":        "us-east-1",
			"resource_type": "aws_servicequotas_service_quota",
			"resource_id":   "quota/compute/vcpu",
			"name":          "compute-quota",
			"attributes": map[string]any{
				"service_name": "Compute",
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}

	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got := anyToString(row["service_anchor_status"]); got != "" {
		t.Fatalf("service_anchor_status = %q, want empty for generic AWS metadata", got)
	}
	if got := anyToString(row["service_name"]); got != "" {
		t.Fatalf("service_name = %q, want empty for generic AWS metadata", got)
	}
}

// TestExtractCloudResourceNodeRowsMalformedServiceNameQuarantines proves the
// #4631 typed-attribute-decode fix: a nested attributes.service_name present
// as a non-string on an allow-listed resource type must dead-letter as a
// visible input_invalid quarantine, not silently produce a node with no
// service anchor (which would look identical to a resource that genuinely
// carries no anchor at all).
func TestExtractCloudResourceNodeRowsMalformedServiceNameQuarantines(t *testing.T) {
	t.Parallel()

	rows, quarantined, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "sample-account",
			"region":        "us-east-1",
			"resource_type": "aws_vpclattice_listener",
			"resource_id":   "listener/bad/https",
			"name":          "bad-listener",
			"attributes": map[string]any{
				"service_name": 12345,
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 0; got != want {
		t.Fatalf("len(rows) = %d, want %d for a quarantined resource", got, want)
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1 for a malformed service_name", len(quarantined))
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want input_invalid", quarantined[0].classification)
	}
}
