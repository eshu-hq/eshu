// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestContainerImageIdentityFactIDExcludesOutcome(t *testing.T) {
	t.Parallel()

	write := containerImageIdentityFenceWrite(
		time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC),
		ContainerImageIdentityExactDigest,
	)
	exact := write.Decisions[0]
	tag := exact
	tag.Outcome = ContainerImageIdentityTagResolved

	if got, want := containerImageIdentityFactID(write, exact), containerImageIdentityFactID(write, tag); got != want {
		t.Fatalf("logical fact IDs differ by outcome: exact=%q tag=%q", got, want)
	}
	if got, want := containerImageIdentityStableFactKey(write, exact), containerImageIdentityStableFactKey(write, tag); got != want {
		t.Fatalf("stable fact keys differ by outcome: exact=%q tag=%q", got, want)
	}
	if got, want := canonicalContainerImageIdentityID(write, exact), canonicalContainerImageIdentityID(write, tag); got != want {
		t.Fatalf("canonical IDs differ by outcome: exact=%q tag=%q", got, want)
	}
	if got, want := legacyContainerImageIdentityFactID(write, exact), legacyContainerImageIdentityFactID(write, tag); got == want {
		t.Fatalf("legacy fact IDs unexpectedly collide: exact=%q tag=%q", got, want)
	}
}

func TestContainerImageIdentityPublicationPrefersCanonicalExactDigest(t *testing.T) {
	t.Parallel()

	exact := containerImageIdentityDecisionForOutcome(
		"registry.example.com/team/api:prod",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContainerImageIdentityExactDigest,
	)
	tag := containerImageIdentityDecisionForOutcome(
		exact.ImageRef,
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ContainerImageIdentityTagResolved,
	)
	unresolved := exact
	unresolved.Outcome = ContainerImageIdentityUnresolved
	unresolved.CanonicalWrites = 0

	write := ContainerImageIdentityWrite{
		IntentID:           "intent-5854-preference",
		ClaimEpoch:         1,
		ScopeID:            "repository:synthetic",
		GenerationID:       "generation-5854",
		SourceSystem:       "git",
		EvidenceAsOf:       time.Date(2026, time.July, 29, 14, 1, 0, 0, time.UTC),
		Decisions:          []ContainerImageIdentityDecision{tag, exact, unresolved},
		TombstoneDecisions: []ContainerImageIdentityDecision{unresolved},
	}
	db := &fakeWorkloadIdentityExecer{}
	writer := newContainerImageIdentityUnitWriter(db)
	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}

	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("published rows = %d, want %d", got, want)
	}
	if rows[0].IsTombstone {
		t.Fatal("published row is a tombstone, want canonical decision to win")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["outcome"], string(ContainerImageIdentityExactDigest); got != want {
		t.Fatalf("published outcome = %v, want %v", got, want)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got := result.RetirementAttempts; got != 0 {
		t.Fatalf("RetirementAttempts = %d, want 0 because canonical publication won", got)
	}
}

func TestContainerImageIdentityWriterPublishesFencedTombstone(t *testing.T) {
	t.Parallel()

	decision := ContainerImageIdentityDecision{
		ImageRef:        "registry.example.com/team/api:prod",
		Outcome:         ContainerImageIdentityUnresolved,
		CanonicalWrites: 0,
	}
	write := ContainerImageIdentityWrite{
		IntentID:           "intent-5854-tombstone",
		ClaimEpoch:         1,
		ScopeID:            "repository:synthetic",
		GenerationID:       "generation-5854",
		SourceSystem:       "git",
		EvidenceAsOf:       time.Date(2026, time.July, 29, 14, 2, 0, 0, time.UTC),
		Decisions:          []ContainerImageIdentityDecision{decision},
		TombstoneDecisions: []ContainerImageIdentityDecision{decision},
	}
	db := &fakeWorkloadIdentityExecer{}
	writer := newContainerImageIdentityUnitWriter(db)
	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}

	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("published rows = %d, want %d", got, want)
	}
	if !rows[0].IsTombstone {
		t.Fatal("published row is live, want tombstone")
	}
	if got, want := rows[0].FencingToken, write.EvidenceAsOf.UnixMicro(); got != want {
		t.Fatalf("tombstone fencing token = %d, want %d", got, want)
	}
	if got, want := result.RetirementAttempts, 1; got != want {
		t.Fatalf("RetirementAttempts = %d, want %d", got, want)
	}
}
