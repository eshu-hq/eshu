// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The #11105 replay and retry matrix on the real queue (arbiter ruling item
// 4). Every case uses the attachWait harness: real fact store, real impact
// writer, real readiness store, real ReducerQueue claim/fail/ack SQL.

// seedAttachWaitScope registers one image scope generation with every fact
// except the attachment, so a finding appears only once the attachment fact
// is written.
func seedAttachWaitScope(w *attachWait, scope string) {
	fanOutSeedScope(w.t, w.ctx, w.db, scope, "g1")
	w.upsert(append(append(w.vulnerabilityFacts(scope), w.documentFacts(scope)...), w.identityFacts(scope)...)...)
}

// deferFirstImpactPass claims the impact row and requires the #11105
// deferral, leaving the row retrying at attempt 1.
func (w *attachWait) deferFirstImpactPass(label, impactID string) {
	w.t.Helper()
	intent := w.claim(w.impactQ, label, true)
	if got := failureClassOf(w.runImpact(intent)); got != supplyChainImpactAttachmentNotReadyClass {
		w.t.Fatalf("%s: class %q, want %s", label, got, supplyChainImpactAttachmentNotReadyClass)
	}
	if status, _, attempts := w.item(impactID); status != "retrying" || attempts != 1 {
		w.t.Fatalf("%s: impact row = %s attempts=%d, want retrying at 1", label, status, attempts)
	}
}

// TestReducerContentionGateSupplyChainImpactAttachmentDeadLetterProceedsLive:
// an attachment that dead-letters is terminal. The parked impact pass is not
// woken by the dead-letter Fail, so it waits out the 10-minute backstop, then
// proceeds without the attachment and records producer_failed.
func TestReducerContentionGateSupplyChainImpactAttachmentDeadLetterProceedsLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:dead-letter"
	seedAttachWaitScope(w, scope)
	w.seedWorkItem("attach-dl", scope, reducercontract.DomainSBOMAttestationAttachment, "retrying", w.clock.Add(2*time.Minute))
	w.seedWorkItem("impact-dl", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))
	parkedAt := w.clock
	w.deferFirstImpactPass("first impact pass", "impact-dl")

	w.clock = w.clock.Add(3 * time.Minute)
	attach := w.claim(w.attachQ, "attachment claim", true)
	if err := w.attachQ.Fail(w.ctx, attach, errors.New("unparseable attestation")); err != nil {
		t.Fatalf("attachment Fail(): %v", err)
	}
	if status, _, _ := w.item("attach-dl"); status != "dead_letter" {
		t.Fatalf("attachment row = %s, want dead_letter", status)
	}
	w.claim(w.impactQ, "impact claim before the backstop", false)

	w.clock = parkedAt.Add(postgres.WaitingBackstopDelay + time.Second)
	intent := w.claim(w.impactQ, "impact claim after the backstop", true)
	if intent.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1", intent.AttemptCount)
	}
	if err := w.runImpact(intent); err != nil {
		t.Fatalf("impact pass after a dead-lettered attachment: %v", err)
	}
	if status, _, attempts := w.item("impact-dl"); status != "succeeded" || attempts != 1 {
		t.Fatalf("impact terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
	if got := w.attachedFindings(scope); got != 0 {
		t.Fatalf("attached findings = %d, want 0 (the attachment never committed)", got)
	}
}

// TestReducerContentionGateSupplyChainImpactAttachmentParkAfterAckLive: the
// attachment reaches succeeded after the impact probe read it in flight but
// before the impact Fail parks. The set-aside re-check sees the terminal
// sibling and reopens the wait in the same Fail, so the pass is claimable at
// once instead of sitting out the backstop.
func TestReducerContentionGateSupplyChainImpactAttachmentParkAfterAckLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:park-after-ack"
	seedAttachWaitScope(w, scope)
	w.seedWorkItem("attach-pa", scope, reducercontract.DomainSBOMAttestationAttachment, "retrying", w.clock.Add(2*time.Minute))
	w.seedWorkItem("impact-pa", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))

	intent := w.claim(w.impactQ, "impact claim", true)
	_, handleErr := w.handler.Handle(w.ctx, intent)
	if got := failureClassOf(handleErr); got != supplyChainImpactAttachmentNotReadyClass {
		t.Fatalf("Handle() class %q, want %s", got, supplyChainImpactAttachmentNotReadyClass)
	}
	w.writeAttachment(scope, reducercontract.Intent{IntentID: "attach-pa"})
	if _, err := w.db.ExecContext(w.ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = 'attach-pa'`); err != nil {
		t.Fatalf("finish attachment: %v", err)
	}
	if err := w.impactQ.Fail(w.ctx, intent, handleErr); err != nil {
		t.Fatalf("impact Fail(): %v", err)
	}
	reopened := w.claim(w.impactQ, "impact claim right after the park", true)
	if err := w.runImpact(reopened); err != nil {
		t.Fatalf("reopened pass: %v", err)
	}
	if got := w.attachedFindings(scope); got != 1 {
		t.Fatalf("attached findings = %d, want 1", got)
	}
}

// TestReducerContentionGateSupplyChainImpactAttachmentDuplicateAckIsANoopLive:
// a repeated Ack of the same attachment claim is rejected by the lease fence
// before any wake runs, so it neither errors the reopened impact row nor
// makes it claimable twice.
func TestReducerContentionGateSupplyChainImpactAttachmentDuplicateAckIsANoopLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:duplicate-ack"
	seedAttachWaitScope(w, scope)
	w.seedWorkItem("attach-dup", scope, reducercontract.DomainSBOMAttestationAttachment, "retrying", w.clock.Add(2*time.Minute))
	w.seedWorkItem("impact-dup", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))
	w.deferFirstImpactPass("first impact pass", "impact-dup")

	w.clock = w.clock.Add(3 * time.Minute)
	attach := w.claim(w.attachQ, "attachment claim", true)
	w.writeAttachment(scope, attach)
	if err := w.attachQ.Ack(w.ctx, attach, reducercontract.Result{}); err != nil {
		t.Fatalf("attachment Ack(): %v", err)
	}
	if err := w.attachQ.Ack(w.ctx, attach, reducercontract.Result{}); !errors.Is(err, postgres.ErrReducerClaimRejected) {
		t.Fatalf("duplicate attachment Ack() = %v, want ErrReducerClaimRejected", err)
	}
	reopened := w.claim(w.impactQ, "impact claim after the Acks", true)
	w.claim(w.impactQ, "second impact claim", false)
	if err := w.runImpact(reopened); err != nil {
		t.Fatalf("reopened pass: %v", err)
	}
	if status, _, attempts := w.item("impact-dup"); status != "succeeded" || attempts != 1 {
		t.Fatalf("impact terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

// failingAttachmentReadiness makes every probe unanswerable with a torn
// connection, which factload.ClassifyFactLoadError turns into the retryable
// counting class. (Any other probe error dead-letters on its first attempt,
// also counting.)
type failingAttachmentReadiness struct{}

func (failingAttachmentReadiness) SBOMAttestationAttachmentState(
	context.Context, string, string,
) (securityalert.ImpactProducerState, error) {
	return securityalert.ImpactProducerState{}, fmt.Errorf("read attachment state: %w", io.ErrUnexpectedEOF)
}

// TestReducerContentionGateSupplyChainImpactAttachmentDeferralsStayFrozenLive:
// across more deferral cycles than MaxAttempts (each one re-claimed after the
// backstop while the attachment keeps backing off), attempt_count stays at 1
// and nothing is written. The wait is bounded by the attachment's own sign of
// life, so 50 minutes of deferral with a live attachment is not a give-up.
// Then the probe becomes unanswerable: that is a COUNTING failure, so the row
// dead-letters on the queue budget instead of retrying forever.
func TestReducerContentionGateSupplyChainImpactAttachmentDeferralsStayFrozenLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:frozen"
	seedAttachWaitScope(w, scope)
	w.seedWorkItem("attach-fz", scope, reducercontract.DomainSBOMAttestationAttachment, "retrying", w.clock.Add(2*time.Minute))
	w.seedWorkItem("impact-fz", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))

	// backOff plays the attachment backing off again: its visible_at and last
	// attempt move with the clock, which is what keeps the bound from lapsing.
	backOff := func(cycle int) {
		t.Helper()
		if _, err := w.db.ExecContext(w.ctx, `UPDATE fact_work_items SET visible_at = $2, next_attempt_at = $2, last_attempt_at = $3
WHERE work_item_id = $1`, "attach-fz", w.clock.Add(2*time.Minute), w.clock); err != nil {
			t.Fatalf("cycle %d: back off attachment: %v", cycle, err)
		}
	}
	for cycle := 1; cycle <= 5; cycle++ {
		backOff(cycle)
		w.deferFirstImpactPass("deferral cycle", "impact-fz")
		w.clock = w.clock.Add(postgres.WaitingBackstopDelay + time.Second)
	}
	if got := w.attachedFindings(scope); got != 0 {
		t.Fatalf("attached findings = %d, want 0 while deferring", got)
	}

	w.handler.AttachmentReadiness = failingAttachmentReadiness{}
	for attempt := 1; attempt <= 3; attempt++ {
		backOff(5 + attempt)
		intent := w.claim(w.impactQ, fmt.Sprintf("counting claim %d (row %v)", attempt, rowState(w, "impact-fz")), true)
		err := w.runImpact(intent)
		if err == nil || failureClassOf(err) == supplyChainImpactAttachmentNotReadyClass {
			t.Fatalf("counting attempt %d: error = %v (class %q), want a counting probe error", attempt, err, failureClassOf(err))
		}
		w.clock = w.clock.Add(time.Hour)
	}
	if status, _, attempts := w.item("impact-fz"); status != "dead_letter" || attempts != 3 {
		t.Fatalf("impact row = %s attempts=%d, want dead_letter at 3 (the probe error spends the budget)", status, attempts)
	}
}

// TestReducerContentionGateSupplyChainImpactAttachmentSupersededGenerationLive:
// the probe reads only its own generation. g1's attachment was superseded
// (neither in flight nor failed) while g2's attachment is retrying; g1's
// impact pass proceeds on its first claim and records nothing, because
// another generation's in-flight attachment is not its sibling.
func TestReducerContentionGateSupplyChainImpactAttachmentSupersededGenerationLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:superseded"
	seedAttachWaitScope(w, scope)
	w.seedWorkItem("attach-g1", scope, reducercontract.DomainSBOMAttestationAttachment, "pending", w.clock.Add(-time.Minute))
	if _, err := w.db.ExecContext(w.ctx, `UPDATE fact_work_items SET status = 'superseded' WHERE work_item_id = 'attach-g1'`); err != nil {
		t.Fatalf("supersede g1 attachment: %v", err)
	}
	if _, err := w.db.ExecContext(w.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'pending', '{}'::jsonb)`, fanOutGen(scope, "g2"), scope, w.clock); err != nil {
		t.Fatalf("seed g2 generation: %v", err)
	}
	if _, err := w.db.ExecContext(w.ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, visible_at, payload, created_at, updated_at)
VALUES ('attach-g2', $1, $2, 'reducer', 'sbom_attestation_attachment', 'scope', $1, 'retrying', 1, $3, '{}'::jsonb, $4, $4)`,
		scope, fanOutGen(scope, "g2"), w.clock.Add(5*time.Minute), w.clock.Add(-time.Minute)); err != nil {
		t.Fatalf("seed g2 attachment: %v", err)
	}
	w.seedWorkItem("impact-g1", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))

	intent := w.claim(w.impactQ, "g1 impact claim", true)
	if err := w.runImpact(intent); err != nil {
		t.Fatalf("g1 impact pass: %v, want it to proceed past a superseded sibling", err)
	}
	if status, _, attempts := w.item("impact-g1"); status != "succeeded" || attempts != 1 {
		t.Fatalf("g1 impact terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

// TestReducerContentionGateSupplyChainImpactAttachmentBoundExceededLive: an
// attachment row in flight with no sign of life for longer than
// crossscope.ProducerReadinessMaxWait (every last-workable column older than
// 30 minutes) no longer holds the pass: it proceeds on its first claim.
func TestReducerContentionGateSupplyChainImpactAttachmentBoundExceededLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:bound"
	seedAttachWaitScope(w, scope)
	w.seedWorkItem("attach-bd", scope, reducercontract.DomainSBOMAttestationAttachment, "retrying", w.clock)
	stale := w.clock.Add(-31 * time.Minute)
	if _, err := w.db.ExecContext(w.ctx, `UPDATE fact_work_items SET created_at = $2, visible_at = $2, next_attempt_at = $2,
  last_attempt_at = $2, claim_until = NULL, reopened_at = NULL WHERE work_item_id = $1`, "attach-bd", stale); err != nil {
		t.Fatalf("age attachment: %v", err)
	}
	w.seedWorkItem("impact-bd", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))

	intent := w.claim(w.impactQ, "impact claim", true)
	if err := w.runImpact(intent); err != nil {
		t.Fatalf("impact pass past the bound: %v", err)
	}
	if status, _, attempts := w.item("impact-bd"); status != "succeeded" || attempts != 1 {
		t.Fatalf("impact terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

func rowState(w *attachWait, id string) string {
	status, class, attempts := w.item(id)
	return fmt.Sprintf("%s/%s/%d", status, class, attempts)
}

// TestReducerContentionGateSupplyChainImpactAttachmentForeignAckIsAHintLive
// pins what the existing producer-terminal wake does with a narrowed wait:
// besides the narrowed (scope, generation, sbom_attestation_attachment) key,
// wakeProducerTerminalAfterAck runs the cross-scope leg for the acked domain,
// which reopens every scope-bound wait naming that domain in ANY scope. So an
// attachment Ack in scope B reopens scope A's parked impact pass. That wake is
// only a hint: A re-defers without spending an attempt and parks again, and
// its own attachment Ack still reopens it. The cost is one claim, one probe,
// and one Fail per parked pass per foreign attachment Ack, with no evidence
// load (the probe runs first).
func TestReducerContentionGateSupplyChainImpactAttachmentForeignAckIsAHintLive(t *testing.T) {
	w := newAttachWait(t)
	const scopeA, scopeB = "scope:11105:hint-a", "scope:11105:hint-b"
	for _, scope := range []string{scopeA, scopeB} {
		seedAttachWaitScope(w, scope)
	}
	w.seedWorkItem("attach-ha", scopeA, reducercontract.DomainSBOMAttestationAttachment, "retrying", w.clock.Add(time.Hour))
	w.seedWorkItem("impact-ha", scopeA, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))
	w.deferFirstImpactPass("scope A first pass", "impact-ha")
	w.claim(w.impactQ, "scope A while parked", false)

	w.seedWorkItem("attach-hb", scopeB, reducercontract.DomainSBOMAttestationAttachment, "pending", w.clock.Add(-time.Minute))
	attachB := w.claim(w.attachQ, "scope B attachment", true)
	w.writeAttachment(scopeB, attachB)
	if err := w.attachQ.Ack(w.ctx, attachB, reducercontract.Result{}); err != nil {
		t.Fatalf("scope B attachment Ack(): %v", err)
	}
	// The foreign Ack reopened A: the pass is claimable and re-defers.
	w.deferFirstImpactPass("scope A after scope B's Ack", "impact-ha")
	w.claim(w.impactQ, "scope A parked again", false)
}
