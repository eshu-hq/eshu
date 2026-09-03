// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestContainerImageIdentityFactIDExcludesOutcome(t *testing.T) {
	t.Parallel()

	write := containerImageIdentityFenceWrite(
		time.Date(2026, time.July, 29, 14, 0, 0, 0, time.UTC),
		reducercontract.ContainerImageIdentityExactDigest,
	)
	exact := write.Decisions[0]
	tag := exact
	tag.Outcome = reducercontract.ContainerImageIdentityTagResolved

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
		reducercontract.ContainerImageIdentityExactDigest,
	)
	tag := containerImageIdentityDecisionForOutcome(
		exact.ImageRef,
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		reducercontract.ContainerImageIdentityTagResolved,
	)
	unresolved := exact
	unresolved.Outcome = reducercontract.ContainerImageIdentityUnresolved
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
	if got, want := payload["outcome"], string(reducercontract.ContainerImageIdentityExactDigest); got != want {
		t.Fatalf("published outcome = %v, want %v", got, want)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got := result.RetirementAttempts; got != 0 {
		t.Fatalf("RetirementAttempts = %d, want 0 because canonical publication won", got)
	}
}

// TestContainerImageIdentityPublicationCanNarrowSourceRepositoryIDs is the
// #5887 writer-level regression guard on the same collapse
// TestContainerImageIdentityPublicationPrefersCanonicalExactDigest already
// pins for outcome/payload, but for source_repository_ids specifically.
//
// #5854 made the identity outcome-independent so two decisions for the same
// (scope, generation, image_ref) collide on one factID
// (TestContainerImageIdentityFactIDExcludesOutcome) and
// planContainerImageIdentityPublications keeps only the higher-ranked
// outcome's decision. That decision's SourceRepositoryIDs is whatever the
// higher-ranked decision itself carries -- it is NOT a union with the
// lower-ranked decision's SourceRepositoryIDs, even when the lower-ranked
// decision named more repositories. This test makes that behavior explicit
// and pinned rather than silent: a tag_resolved decision carries TWO source
// repositories (mirroring the golden corpus's pre-#5854 CI-scope row), an
// exact_digest decision for the SAME image_ref carries only ONE, and the
// published row is the exact_digest decision's narrower set -- exactly the
// 2-source-repository-IDs-to-1 collapse issue #5887 traces as the trigger
// for the anchor becoming tier A. If a future identity-key or publication-
// rank change alters which decision wins, or starts merging
// SourceRepositoryIDs across colliding decisions, this test's published
// source_repository_ids value changes and the test fails, giving that
// change a visible signal instead of a silent re-tiering three call frames
// away in supply_chain_impact_anchor_tier.go.
func TestContainerImageIdentityPublicationCanNarrowSourceRepositoryIDs(t *testing.T) {
	t.Parallel()

	const (
		imageRef     = "registry.example.com/team/api:prod"
		digest       = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		deployRepoID = "repository:r_5887_deploy"
		buildRepoID  = "repository:r_5887_build"
	)

	// Weaker evidence, named TWO repositories (mirrors the pre-#5854
	// two-repository CI-scope row from the golden corpus).
	tag := containerImageIdentityDecisionForOutcome(imageRef, digest, reducercontract.ContainerImageIdentityTagResolved)
	tag.SourceRepositoryIDs = []string{deployRepoID, buildRepoID}
	// Stronger evidence for the SAME image_ref, named only ONE repository.
	exact := containerImageIdentityDecisionForOutcome(imageRef, digest, reducercontract.ContainerImageIdentityExactDigest)
	exact.SourceRepositoryIDs = []string{buildRepoID}

	write := ContainerImageIdentityWrite{
		IntentID:     "intent-5887-narrowing",
		ClaimEpoch:   1,
		ScopeID:      "repository:synthetic-5887",
		GenerationID: "generation-5887",
		SourceSystem: "git",
		EvidenceAsOf: time.Date(2026, time.July, 29, 14, 2, 0, 0, time.UTC),
		Decisions:    []ContainerImageIdentityDecision{tag, exact},
	}
	db := &fakeWorkloadIdentityExecer{}
	writer := newContainerImageIdentityUnitWriter(db)
	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}

	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("published rows = %d, want %d (the two decisions collide on one logical key)", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["outcome"], string(reducercontract.ContainerImageIdentityExactDigest); got != want {
		t.Fatalf("published outcome = %v, want the higher-ranked %v", got, want)
	}
	published, ok := payload["source_repository_ids"].([]any)
	if !ok {
		t.Fatalf("source_repository_ids payload type = %T, want []any", payload["source_repository_ids"])
	}
	if got, want := len(published), 1; got != want {
		t.Fatalf(
			"published source_repository_ids = %v (len %d), want the exact_digest decision's narrower single-entry set (len %d): "+
				"this is the exact #5887 mechanism -- the tag_resolved decision's two repositories are dropped, not merged",
			published, got, want,
		)
	}
	if got, want := published[0], buildRepoID; got != want {
		t.Fatalf("published source_repository_ids[0] = %v, want %v", got, want)
	}
}

func TestContainerImageIdentityWriterPublishesFencedTombstone(t *testing.T) {
	t.Parallel()

	decision := ContainerImageIdentityDecision{
		ImageRef:        "registry.example.com/team/api:prod",
		Outcome:         reducercontract.ContainerImageIdentityUnresolved,
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
