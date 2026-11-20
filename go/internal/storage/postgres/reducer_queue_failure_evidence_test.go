// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// TestScanReducerIntentCarriesPriorFailureEvidence is the #11075 phase-2
// regression for the deferral clock's durable memory: a claimed row's prior
// failure class, message, and details must ride the claim onto the intent,
// so a handler deferred in a prior evaluation keeps measuring its stale
// clock from the cycle's first deferral instead of restarting it on every
// claim. Rows with no prior failure must scan a nil Failure.
func TestScanReducerIntentCarriesPriorFailureEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 9, 12, 45, 0, 0, time.UTC)
	rawPayload, err := payloadstore.MarshalPayload(map[string]any{
		"entity_key":    "advisory_group:CVE-2026-0001",
		"reason":        "supply_chain_impact_winners_touch",
		"source_system": "vulnerability_intelligence",
	})
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	rows := &fake.Rows{Data: [][]any{{
		"intent-1",
		"scope:vuln",
		"gen:vuln",
		"advisory_group_projection",
		1,
		int64(0),
		now.Add(-8 * 24 * time.Hour),
		now.Add(-8 * 24 * time.Hour),
		now.Add(-45 * time.Minute),
		now.Add(-time.Minute),
		// The claim selects the prior verdict's failure evidence so the
		// handler can keep its deferral anchor across retries.
		"advisory_group_winners_watermark_not_ready",
		"prior deferral message",
		"prior deferral details\nwinners-watermark-deferral: first-deferred-at=2026-11-09T12:00:00Z cycle-started-at=2026-11-09T12:00:00Z",
		rawPayload,
	}}}
	if !rows.Next() {
		t.Fatal("staged row: want Next true")
	}
	intent, err := scanReducerIntent(rows)
	if err != nil {
		t.Fatalf("scanReducerIntent: %v", err)
	}
	if intent.Failure == nil {
		t.Fatal("intent.Failure is nil, want the prior verdict's failure evidence")
	}
	if intent.Failure.FailureClass != "advisory_group_winners_watermark_not_ready" {
		t.Fatalf("FailureClass = %q", intent.Failure.FailureClass)
	}
	if intent.Failure.Message != "prior deferral message" {
		t.Fatalf("Message = %q", intent.Failure.Message)
	}
	if intent.Failure.Details == "" {
		t.Fatal("Details is empty, want the prior deferral marker")
	}
}

// TestScanReducerIntentOmitsFailureWithoutPriorEvidence pins the fresh-row
// side: a claim with no prior failure scans a nil Failure, so the handler
// treats the evaluation as the cycle's first deferral.
func TestScanReducerIntentOmitsFailureWithoutPriorEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 9, 12, 45, 0, 0, time.UTC)
	rawPayload, err := payloadstore.MarshalPayload(map[string]any{
		"entity_key":    "advisory_group:CVE-2026-0001",
		"reason":        "supply_chain_impact_winners_touch",
		"source_system": "vulnerability_intelligence",
	})
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	rows := &fake.Rows{Data: [][]any{{
		"intent-1",
		"scope:vuln",
		"gen:vuln",
		"advisory_group_projection",
		1,
		int64(0),
		now.Add(-8 * 24 * time.Hour),
		now.Add(-8 * 24 * time.Hour),
		now.Add(-45 * time.Minute),
		now.Add(-time.Minute),
		"",
		"",
		"",
		rawPayload,
	}}}
	if !rows.Next() {
		t.Fatal("staged row: want Next true")
	}
	intent, err := scanReducerIntent(rows)
	if err != nil {
		t.Fatalf("scanReducerIntent: %v", err)
	}
	if intent.Failure != nil {
		t.Fatalf("intent.Failure = %+v, want nil for a row with no prior failure", intent.Failure)
	}
}
