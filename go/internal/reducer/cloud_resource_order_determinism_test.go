// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractCloudResourceNodeRowsResolvesDuplicateUIDByMaxOrderKey is the
// within-scope half of the #5007 contention regression: two facts for the same
// uid with DIVERGENT observed state (different state, observed_at, source_fact_id)
// must resolve deterministically to the latest observation regardless of slice
// order. On origin/main this extractor took "last fact for a uid wins" by slice
// position, so reversing the input flipped the winner. With the
// max-(observed_at, source_fact_id) rule the winner is the same in both orders.
// RED before the rule, GREEN after.
func TestExtractCloudResourceNodeRowsResolvesDuplicateUIDByMaxOrderKey(t *testing.T) {
	t.Parallel()

	older := facts.Envelope{
		FactKind:   facts.AWSResourceFactKind,
		FactID:     "fact-older",
		ObservedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_ec2_volume", "resource_id": "vol-shared", "state": "stopped",
		},
	}
	newer := facts.Envelope{
		FactKind:   facts.AWSResourceFactKind,
		FactID:     "fact-newer",
		ObservedAt: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_ec2_volume", "resource_id": "vol-shared", "state": "running",
		},
	}

	forward, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{older, newer})
	if err != nil {
		t.Fatalf("forward extract error: %v", err)
	}
	reversed, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{newer, older})
	if err != nil {
		t.Fatalf("reversed extract error: %v", err)
	}
	if len(forward) != 1 || len(reversed) != 1 {
		t.Fatalf("expected one node per order, got forward=%d reversed=%d", len(forward), len(reversed))
	}
	if forward[0]["state"] != "running" || reversed[0]["state"] != "running" {
		t.Fatalf("winner state must be running (latest observation) in both orders: forward=%v reversed=%v",
			forward[0]["state"], reversed[0]["state"])
	}
	if forward[0]["source_fact_id"] != "fact-newer" || reversed[0]["source_fact_id"] != "fact-newer" {
		t.Fatalf("winner source_fact_id must be fact-newer in both orders, got forward=%v reversed=%v",
			forward[0]["source_fact_id"], reversed[0]["source_fact_id"])
	}
	if forward[0][sourceOrderKeyField] != reversed[0][sourceOrderKeyField] {
		t.Fatalf("source_order_key must be identical across input orders: forward=%v reversed=%v",
			forward[0][sourceOrderKeyField], reversed[0][sourceOrderKeyField])
	}
}

// TestExtractCloudResourceNodeRowsIdenticalPayloadTiesBreakDeterministically is
// the identical-payload half of the #5007 contention regression: two facts with
// byte-identical observed state but different source_fact_id (pure envelope
// contention) must still resolve to a deterministic winner (max source_fact_id)
// independent of slice order.
func TestExtractCloudResourceNodeRowsIdenticalPayloadTiesBreakDeterministically(t *testing.T) {
	t.Parallel()

	sameInstant := time.Date(2026, time.March, 3, 9, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"account_id": "111122223333", "region": "us-east-1",
		"resource_type": "aws_ec2_vpc", "resource_id": "vpc-shared", "name": "main", "state": "available",
	}
	low := facts.Envelope{FactKind: facts.AWSResourceFactKind, FactID: "fact-aaa", ObservedAt: sameInstant, Payload: payload}
	high := facts.Envelope{FactKind: facts.AWSResourceFactKind, FactID: "fact-bbb", ObservedAt: sameInstant, Payload: payload}

	forward, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{low, high})
	if err != nil {
		t.Fatalf("forward extract error: %v", err)
	}
	reversed, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{high, low})
	if err != nil {
		t.Fatalf("reversed extract error: %v", err)
	}
	if len(forward) != 1 || len(reversed) != 1 {
		t.Fatalf("expected one node per order, got forward=%d reversed=%d", len(forward), len(reversed))
	}
	if forward[0]["source_fact_id"] != "fact-bbb" || reversed[0]["source_fact_id"] != "fact-bbb" {
		t.Fatalf("identical-payload winner must tie-break on max source_fact_id (fact-bbb) in both orders, got forward=%v reversed=%v",
			forward[0]["source_fact_id"], reversed[0]["source_fact_id"])
	}
}
