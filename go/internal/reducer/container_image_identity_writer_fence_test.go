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
// was read at evidenceAsOf — the moment that decides which of two racing
// workers holds the fresher view of the world.
func containerImageIdentityFenceWrite(
	evidenceAsOf time.Time,
	outcome ContainerImageIdentityOutcome,
) ContainerImageIdentityWrite {
	return ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		EvidenceAsOf: evidenceAsOf,
		Decisions: []ContainerImageIdentityDecision{
			containerImageIdentityDecisionForOutcome(
				"registry.example.com/team/api:prod",
				testContainerDigest,
				outcome,
			),
		},
	}
}

// TestContainerImageIdentityFencingTokenOrdersByEvidenceReadTime pins the
// DIRECTION of the fence, which is the part that is easy to get backwards.
//
// The stale worker is the one that READ ITS EVIDENCE EARLIER, not the one that
// wrote later. A worker can stall for a whole lease between loading evidence and
// writing, then land after a worker that loaded fresh evidence and overtook it.
// So the token is derived from EvidenceAsOf — captured before the evidence load
// — and NOT from the writer's clock at write time, which would rank the stalled
// worker highest precisely when it matters, letting its poorer payload win the
// insert's conflict guard.
func TestContainerImageIdentityFencingTokenOrdersByEvidenceReadTime(t *testing.T) {
	t.Parallel()

	stale := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	fresh := stale.Add(90 * time.Second)

	staleToken := containerImageIdentityFencingToken(
		containerImageIdentityFenceWrite(stale, ContainerImageIdentityTagResolved),
	)
	freshToken := containerImageIdentityFencingToken(
		containerImageIdentityFenceWrite(fresh, ContainerImageIdentityExactDigest),
	)

	if staleToken >= freshToken {
		t.Fatalf(
			"fencing tokens = stale %d, fresh %d; the later evidence read must rank higher",
			staleToken, freshToken,
		)
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
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db}
	if _, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		containerImageIdentityFenceWrite(evidenceAsOf, ContainerImageIdentityExactDigest),
	); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}

	rows := decodeBatchedFactCalls(t, db.execs)
	if len(rows) != 1 {
		t.Fatalf("inserted rows = %d, want 1", len(rows))
	}
	if want := evidenceAsOf.UnixMicro(); rows[0].FencingToken != want {
		t.Fatalf(
			"inserted fencing_token = %d, want %d; a row durable at 0 makes the conflict guard inert, "+
				"so a stalled worker's poorer payload wins",
			rows[0].FencingToken, want,
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
		FactLoader: loader,
		Writer:     writer,
		Now:        clock,
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
