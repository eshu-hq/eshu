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

// TestContainerImageIdentityRetireQueryIsASingleDeleteAndNothingElse is the
// cheap belt-and-braces companion to the frozen statement text: whatever the
// statement says, it must be exactly one DELETE targeting exactly fact_records
// and must contain no second write phase.
//
// The retire used to lead with a `WITH stamped AS (UPDATE ...)` CTE that
// re-stamped the keep-set. Once reducerFactBatchInsertQuery began binding the
// same token on the INSERT that CTE became a proven no-op — and not a free one:
// it rewrote every kept row on every execution (a measured second row version
// per canonical decision) and, because it locked the keep-set while the DELETE
// locked the complement with no specified ordering inside a `WITH`, it deadlocks
// ABBA between two concurrent same-scope retires with crossed keep/delete sets,
// the exact stalled-worker shape the fence exists for. The deadlock was
// reproduced on the `5837-drift-reopen` sibling branch rather than here, by one
// harness run twice, as `SQLSTATE 40P01` with a `ShareLock` cycle in both
// directions, and it is a race: the CTE variant deadlocked in most trials of
// every run while the plain fenced DELETE deadlocked in none of twenty. The
// asymmetry is what reproduces; there is no fixed rate to quote.
//
// This assertion is what stops a second write phase — a CTE, a chained UPDATE,
// an INSERT — from being reintroduced. The frozen-text test would also catch it,
// but this one names WHY in its failure message.
//
// # What this test does and does not cover on its own
//
// The keyword scan runs against the UPPERCASED statement. It used to run against
// the raw text, which made every check below case-sensitive: appending
// `; update fact_records set fencing_token = 0` passed all three, measured. The
// same class of hole is why the separator check exists — a keyword list cannot
// see a second statement that uses no listed keyword.
//
// What it still cannot see is a side effect smuggled INSIDE the single DELETE,
// such as a volatile function in the WHERE clause. Only the byte-exact
// comparison against containerImageIdentityRetireStatement catches that, and
// that comparison — not this test — is what actually holds the line. This test
// is the one that explains the consequence in its failure message.
func TestContainerImageIdentityRetireQueryIsASingleDeleteAndNothingElse(t *testing.T) {
	t.Parallel()

	normalized := normalizeReducerSQL(containerImageIdentityRetireQuery)
	upper := strings.ToUpper(normalized)
	if got := strings.Count(upper, "DELETE"); got != 1 {
		t.Fatalf("DELETE keywords in retire statement = %d, want exactly 1:\n%s", got, normalized)
	}
	if !strings.HasPrefix(upper, "DELETE FROM FACT_RECORDS") {
		t.Fatalf("the retire must BE a DELETE against fact_records, with no preamble:\n%s", normalized)
	}
	if strings.Contains(normalized, ";") {
		t.Fatalf(
			"retire statement contains a `;` statement separator; it must be ONE statement. A keyword "+
				"scan cannot see what a second statement does, so the separator itself is the "+
				"assertion:\n%s",
			normalized,
		)
	}
	for _, forbidden := range []string{"UPDATE", "INSERT", "WITH ", "RETURNING", "MERGE", "TRUNCATE"} {
		if strings.Contains(upper, forbidden) {
			t.Fatalf(
				"retire statement contains %q; it must stay a single DELETE. A second write phase "+
					"re-stamps rows the INSERT already stamped (a no-op that still costs a row version) "+
					"and reintroduces the ABBA deadlock between crossed keep/delete sets that the "+
					"5837-drift-reopen branch reproduced on Postgres 16.14 (the CTE deadlocked in most "+
					"trials of every run, the plain DELETE in none of twenty):\n%s",
				forbidden, normalized,
			)
		}
	}
}

// TestContainerImageIdentityFencingTokenOrdersByEvidenceReadTime pins the
// DIRECTION of the fence, which is the part that is easy to get backwards.
//
// The stale worker is the one that READ ITS EVIDENCE EARLIER, not the one that
// wrote later. A worker can stall for a whole lease between loading evidence and
// writing, then land after a worker that loaded fresh evidence and overtook it.
// So the token is derived from EvidenceAsOf — captured before the evidence load
// — and NOT from the writer's clock at write time, which would rank the stale
// worker highest precisely when it matters.
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

// TestContainerImageIdentityWriterPassesFencingTokenToRetire proves the token
// actually reaches the statement as the fifth bind parameter. Without it the
// query's `fencing_token <= $5` guard would compare against whatever the driver
// defaulted to, which is exactly the unfenced delete this change exists to stop.
func TestContainerImageIdentityWriterPassesFencingTokenToRetire(t *testing.T) {
	t.Parallel()

	evidenceAsOf := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	write := containerImageIdentityFenceWrite(evidenceAsOf, ContainerImageIdentityExactDigest)

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db}
	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}

	retire := containerImageIdentityRetireCall(t, db.execs)
	if len(retire.args) != 5 {
		t.Fatalf(
			"retire args = %d, want 5 (fact_kind, scope_id, generation_id, keep-set, fencing token)",
			len(retire.args),
		)
	}
	got, ok := retire.args[4].(int64)
	if !ok {
		t.Fatalf("retire fencing token type = %T, want int64", retire.args[4])
	}
	if want := evidenceAsOf.UnixMicro(); got != want {
		t.Fatalf("retire fencing token = %d, want %d (the evidence read time)", got, want)
	}
}

// TestContainerImageIdentityWriterStampsTheFencingTokenOnTheInsert is the unit
// half of the born-unstamped hole.
//
// The retire runs in a SEPARATE autocommit statement after the insert, so
// nothing the retire does can stamp the row in time — which is why the earlier
// `stamped` CTE could not have closed this hole even had it stayed. If the
// insert leaves the row at the fact_records default of 0, the row is durable and
// visible at 0 for that whole window, and 0 is at or below every other worker's
// token — so a stalled worker's fenced retire landing there deletes the fresher
// row it was supposed to be fenced away from. The insert therefore has to carry
// the token itself.
//
// This asserts the argument the writer actually sends; the live sibling
// TestContainerImageIdentityFreshlyInsertedRowIsFencedBeforeItIsVisibleLive
// proves the resulting behavior against real Postgres with a real interleaving.
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

	rows := decodeBatchedFactCalls(t, containerImageIdentityInsertCalls(db.execs))
	if len(rows) != 1 {
		t.Fatalf("inserted rows = %d, want 1", len(rows))
	}
	if want := evidenceAsOf.UnixMicro(); rows[0].FencingToken != want {
		t.Fatalf(
			"inserted fencing_token = %d, want %d; a row durable at 0 can be deleted by any stalled worker's fenced retire",
			rows[0].FencingToken, want,
		)
	}
}

// TestContainerImageIdentityWriterRejectsMissingEvidenceAsOf keeps the fence
// from being silently skippable.
//
// A zero EvidenceAsOf yields token 0, and fact_records.fencing_token defaults to
// 0, so `fencing_token <= 0` would still match every row: a caller that forgot
// to set the watermark would get a fully UNFENCED retire with nothing saying so.
// That is the silent fallback this repository forbids, so the writer refuses the
// write before issuing any statement at all.
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
		t.Fatal("handler passed a zero EvidenceAsOf; the retire would be unfenced")
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
