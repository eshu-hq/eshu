// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// containerImageIdentityDecisionForOutcome builds one canonical decision for an
// image reference classified as outcome. Both durably-written outcomes
// (exact_digest and tag_resolved) carry CanonicalWrites=1, so both survive
// containerImageIdentityCanonicalDecisions' filter.
func containerImageIdentityDecisionForOutcome(
	imageRef string,
	digest string,
	outcome ContainerImageIdentityOutcome,
) ContainerImageIdentityDecision {
	return ContainerImageIdentityDecision{
		ImageRef:         imageRef,
		Digest:           digest,
		RepositoryID:     "oci-registry://registry.example.com/team/api",
		Outcome:          outcome,
		Reason:           "test decision for " + string(outcome),
		CanonicalWrites:  1,
		IdentityStrength: "tag_observation_with_digest",
	}
}

// containerImageIdentityFenceWrite builds a one-decision write whose evidence
// was read at evidenceAsOf (audit timestamp only, #5874) and whose fencing
// token is the database-issued value fencingToken — the value that actually
// decides which of two racing workers holds precedence on the write conflict
// guard and the admission CAS.
func containerImageIdentityFenceWrite(
	evidenceAsOf time.Time,
	fencingToken int64,
	outcome ContainerImageIdentityOutcome,
) ContainerImageIdentityWrite {
	return ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ClaimEpoch:   1,
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		EvidenceAsOf: evidenceAsOf,
		FencingToken: fencingToken,
		Decisions: []ContainerImageIdentityDecision{
			containerImageIdentityDecisionForOutcome(
				"registry.example.com/team/api:prod",
				testContainerDigest,
				outcome,
			),
		},
	}
}

// TestContainerImageIdentityFencingTokenIsTheIssuedTokenNotEvidenceTime pins
// the #5874 replacement invariant: the token stamped on the row and the
// admission CAS is EXACTLY the database-issued value the caller supplied
// (ContainerImageIdentityWrite.FencingToken), never re-derived from
// EvidenceAsOf. The prior wall-clock scheme (EvidenceAsOf.UnixMicro()) is
// gone; see containerImageIdentityFencingToken's doc comment for why deriving
// from EvidenceAsOf again would reintroduce the exact cross-replica
// clock-skew inversion this change closes. This deliberately does NOT assert
// "later evidence read implies higher token" -- no token issued before the
// reads can guarantee that ordering (the irreducible nextval()-to-first-SELECT
// window); the design instead guarantees convergence via admission CAS +
// reopen replay, which TestContainerImageIdentityWriterAdmissionConvergence
// covers.
func TestContainerImageIdentityFencingTokenIsTheIssuedTokenNotEvidenceTime(t *testing.T) {
	t.Parallel()

	evidenceAsOf := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	const issuedToken int64 = 42

	got := containerImageIdentityFencingToken(
		containerImageIdentityFenceWrite(evidenceAsOf, issuedToken, ContainerImageIdentityExactDigest),
	)
	if got != issuedToken {
		t.Fatalf("containerImageIdentityFencingToken() = %d, want the issued token %d, not a value derived from EvidenceAsOf", got, issuedToken)
	}
}

// TestContainerImageIdentityWriterStampsTheFencingTokenOnTheInsert proves the
// watermark reaches the durable row, on the only statement that carries it.
//
// A row left at the fact_records default of 0 makes the insert's conflict guard
// inert for this domain: `0 <= EXCLUDED.fencing_token` holds for every incoming
// pass, so a stalled worker's poorer payload overwrites a fresher worker's row
// and the guard never fires. Stamping on the insert is what gives the guard
// something to compare against.
func TestContainerImageIdentityWriterStampsTheFencingTokenOnTheInsert(t *testing.T) {
	t.Parallel()

	evidenceAsOf := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	const issuedToken int64 = 7
	db := &fakeWorkloadIdentityExecer{}
	writer := newContainerImageIdentityUnitWriter(db)
	if _, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityFenceWrite(evidenceAsOf, issuedToken, ContainerImageIdentityExactDigest),
	); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}

	rows := decodeBatchedFactCalls(t, db.execs)
	if len(rows) != 1 {
		t.Fatalf("inserted rows = %d, want 1", len(rows))
	}
	if rows[0].FencingToken != issuedToken {
		t.Fatalf(
			"inserted fencing_token = %d, want %d; a row durable at 0 makes the conflict guard inert, "+
				"so a stalled worker's poorer payload wins",
			rows[0].FencingToken, issuedToken,
		)
	}
}

// TestContainerImageIdentityWriterRejectsMissingEvidenceAsOf keeps the fence
// from being silently skippable.
//
// A zero EvidenceAsOf yields token 0, and fact_records.fencing_token defaults to
// 0, so the row would be indistinguishable from one written by a domain that
// never opted in: `0 <= EXCLUDED.fencing_token` matches every later pass and the
// guard admits stale content unconditionally. That is the silent fallback this
// repository forbids, so the writer refuses the write before issuing any
// statement at all.
func TestContainerImageIdentityWriterRejectsMissingEvidenceAsOf(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db}
	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
	})
	if err == nil {
		t.Fatal("WriteContainerImageIdentityDecisions() error = nil, want an error for a missing evidence-as-of watermark")
	}
	if !strings.Contains(err.Error(), "evidence_as_of") {
		t.Fatalf("error = %v, want it to name evidence_as_of", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("statements issued = %d, want 0; an unfenced write must not reach the database", len(db.execs))
	}
}

func TestContainerImageIdentityWriterRejectsMissingClaimEpochForLegacyCleanup(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	write := containerImageIdentityFenceWrite(
		time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC),
		1,
		ContainerImageIdentityExactDigest,
	)
	write.ClaimEpoch = 0
	write.LegacyFactIDs = []string{"reducer_container_image_identity:synthetic-legacy"}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err == nil || !strings.Contains(err.Error(), "claim_epoch") {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want missing claim_epoch", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("statements issued = %d, want 0 for an unfenced cutover", len(db.execs))
	}
}

// TestContainerImageIdentityHandlerStampsEvidenceReadTimeBeforeLoading proves
// the watermark is taken BEFORE the evidence load, not after it.
//
// Taken after, the token would include however long the load itself took, so a
// worker that stalled inside a slow cross-scope load would rank ahead of the
// worker that read the database after it — inverting the fence in the one shape
// it exists for. The loader records the clock at the moment it is entered and
// this test compares that against the watermark the writer received.
func TestContainerImageIdentityHandlerStampsEvidenceReadTimeBeforeLoading(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	var ticks int64
	clock := func() time.Time {
		ticks++
		return base.Add(time.Duration(ticks) * time.Second)
	}

	loader := &containerImageIdentityFenceProbeLoader{clock: clock}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:         loader,
		Writer:             writer,
		Now:                clock,
		FencingTokenIssuer: &stubContainerImageIdentityFencingTokenIssuer{tokens: []int64{1}},
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "test",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if writer.write.EvidenceAsOf.IsZero() {
		t.Fatal("handler passed a zero EvidenceAsOf; the durable write would be unfenced")
	}
	if !writer.write.EvidenceAsOf.Before(loader.calledAt) {
		t.Fatalf(
			"EvidenceAsOf = %s, loader entered at %s; the watermark must be taken BEFORE the evidence load",
			writer.write.EvidenceAsOf, loader.calledAt,
		)
	}
}

// containerImageIdentityFenceProbeLoader records the clock reading at the moment
// the first evidence load is entered, so a test can prove the watermark predates
// it.
type containerImageIdentityFenceProbeLoader struct {
	clock    func() time.Time
	calledAt time.Time
}

func (l *containerImageIdentityFenceProbeLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return nil, nil
}

func (l *containerImageIdentityFenceProbeLoader) ListFactsByKind(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	if l.calledAt.IsZero() {
		l.calledAt = l.clock()
	}
	return nil, nil
}
