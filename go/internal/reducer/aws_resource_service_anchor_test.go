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

// TestExtractCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeysWhenNoDecision
// proves issue #5714/#5055's still-live AWS instance: applyCloudResourceServiceAnchorFields
// used to return nil for a resource with no service-anchor decision at all
// (resource_type/attributes carrying nothing cloudResourceServiceAnchorDecisionForPayload
// recognizes), which OMITTED all 7 service-anchor keys from the row. The shared
// canonicalCloudResourceUpsertCypher SETs all 7 unconditionally for every batch
// row; the pinned NornicDB backend does not evaluate a missing UNWIND row map
// key in a SET clause as null, it persists a stringified representation of the
// row expression instead (issue #4995's proved mechanism, TestExtractGCPCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeys).
// So a plain AWS resource (no decision) batched alongside an anchor-bearing AWS
// resource corrupted these 7 properties on the plain resource's node. This test
// asserts PRESENCE (not just empty value) for exactly that no-decision case,
// mirroring the GCP/Azure running-image parity-key tests: a missing key and ""
// both stringify to "" via anyToString, so presence is what actually catches
// this regression.
func TestExtractCloudResourceNodeRowsSetsExplicitServiceAnchorParityKeysWhenNoDecision(t *testing.T) {
	t.Parallel()

	rows, quarantined, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "sample-account",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-1",
			"name":          "main",
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %v, want none", quarantined)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]

	for _, key := range []string{
		"workload_id",
		"service_name",
		"service_anchor_status",
		"service_anchor_source",
		"service_anchor_reason",
		"service_anchor_name_tokens",
	} {
		value, ok := row[key]
		if !ok {
			t.Fatalf("row[%q] is absent; the shared upsert Cypher's row.%s reference "+
				"would resolve against a missing map key on the pinned NornicDB backend "+
				"and persist a stringified-row literal instead of an empty value", key, key)
		}
		if value != "" {
			t.Fatalf("row[%q] = %#v, want empty string (no service-anchor decision)", key, value)
		}
	}

	namesValue, ok := row["service_anchor_names"]
	if !ok {
		t.Fatal("row[\"service_anchor_names\"] is absent; must be an explicit empty value")
	}
	names, ok := namesValue.([]string)
	if !ok {
		t.Fatalf("row[\"service_anchor_names\"] type = %T, want []string", namesValue)
	}
	if len(names) != 0 {
		t.Fatalf("row[\"service_anchor_names\"] = %#v, want empty slice", names)
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
	if quarantined[0].Classification != "input_invalid" {
		t.Fatalf("quarantined[0].Classification = %q, want input_invalid", quarantined[0].Classification)
	}
}
