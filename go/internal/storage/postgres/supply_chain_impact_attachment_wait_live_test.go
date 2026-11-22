// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sbomattest"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Real-queue, real-fact-store proofs for #11105: an image scope's
// supply_chain_impact pass must not load evidence while the same scope
// generation's sbom_attestation_attachment work is still in flight, and the
// attachment's terminal Ack must reopen the parked pass at once. The fact
// store, the impact writer, the attachment writer, the readiness store, and
// the queue are production code against real PostgreSQL. The one stand-in is
// the attachment HANDLER: the test builds the attachment decision itself and
// writes it through the production PostgresSBOMAttestationAttachmentWriter,
// then acks, the order the production handler has (write, then Ack).
//
// The TestReducerContentionGate prefix puts these in the reducer contention
// gate's -run filter (.github/workflows/reducer-contention-gate.yml).

const (
	attachWaitCVE      = "CVE-2026-11105"
	attachWaitCriteria = "cpe:2.3:a:example:server:1.4.2:*:*:*:*:*:*:*"
	attachWaitMatchID  = "b5ec4c98-0000-4000-9000-000000011105"
	attachWaitDocument = "doc-11105"
	attachWaitDigest   = "sha256:1105110511051105110511051105110511051105110511051105110511051105"
	attachWaitRepo     = "repository:r_11105"
)

// attachWait is the harness: one disposable database, one queue clock shared
// by an impact-only and an attachment-only queue, and the production impact
// handler over the real fact store.
type attachWait struct {
	t       *testing.T
	ctx     context.Context
	db      *sql.DB
	clock   time.Time
	impactQ postgres.ReducerQueue
	attachQ postgres.ReducerQueue
	handler supplychaincore.SupplyChainImpactHandler
}

func newAttachWait(t *testing.T) *attachWait {
	t.Helper()
	ctx, db := fanOutLiveDB(t)
	w := &attachWait{t: t, ctx: ctx, db: db, clock: time.Now().UTC().Truncate(time.Second)}
	now := func() time.Time { return w.clock }
	queue := func(owner string, domain reducercontract.Domain) postgres.ReducerQueue {
		q := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, owner, time.Minute)
		q.RetryDelay, q.MaxAttempts, q.Now = time.Second, 3, now
		q.ClaimDomains = []reducercontract.Domain{domain}
		return q
	}
	w.impactQ = queue("impact-worker", reducercontract.DomainSupplyChainImpact)
	w.attachQ = queue("attach-worker", reducercontract.DomainSBOMAttestationAttachment)
	w.handler = supplychaincore.SupplyChainImpactHandler{
		FactLoader:         postgres.NewFactStore(postgres.SQLDB{DB: db}),
		FencingTokenIssuer: postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}},
		Writer: supplychaincore.PostgresSupplyChainImpactWriter{
			DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
			Now: now,
		},
		Now: now,
	}
	wireAttachmentReadiness(&w.handler, db)
	return w
}

func (w *attachWait) envelope(scope, id, kind string, payload map[string]any) facts.Envelope {
	generation := fanOutGen(scope, "g1")
	return facts.Envelope{
		FactID: id, ScopeID: scope, GenerationID: generation, FactKind: kind, StableFactKey: id,
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1, CollectorKind: "synthetic",
		SourceConfidence: facts.SourceConfidenceReported, ObservedAt: w.clock.Add(-time.Hour), Payload: payload,
		SourceRef: facts.Ref{SourceSystem: "synthetic", ScopeID: scope, GenerationID: generation, FactKey: id},
	}
}

func (w *attachWait) upsert(envelopes ...facts.Envelope) {
	w.t.Helper()
	if err := postgres.NewFactStore(postgres.SQLDB{DB: w.db}).UpsertFacts(w.ctx, envelopes); err != nil {
		w.t.Fatalf("upsert facts: %v", err)
	}
}

// vulnerabilityFacts is one CVE and its affected-product criteria.
func (w *attachWait) vulnerabilityFacts(scope string) []facts.Envelope {
	return []facts.Envelope{
		w.envelope(scope, "fact:11105:cve:"+scope, chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": attachWaitCVE, "cve_id": attachWaitCVE, "cvss_score": 8.6, "aliases": []any{attachWaitCVE},
		}),
		w.envelope(scope, "fact:11105:product:"+scope, chain.VulnerabilityAffectedProductFactKind, map[string]any{
			"cve_id": attachWaitCVE, "criteria": attachWaitCriteria, "match_criteria_id": attachWaitMatchID,
			"vulnerable": true, "source": "nvd",
		}),
	}
}

// documentFacts is the SBOM document's component carrying the CPE.
func (w *attachWait) documentFacts(scope string) []facts.Envelope {
	return []facts.Envelope{w.envelope(scope, "fact:11105:component:"+scope, chain.SBOMComponentFactKind, map[string]any{
		"document_id": attachWaitDocument, "cpe": attachWaitCriteria, "name": "example server", "version": "1.4.2",
	})}
}

// identityFacts anchors the attached image digest to a repository.
func (w *attachWait) identityFacts(scope string) []facts.Envelope {
	return []facts.Envelope{w.envelope(scope, "fact:11105:identity:"+scope, reducercontract.ContainerImageIdentityFactKind, map[string]any{
		"digest": attachWaitDigest, "repository_id": attachWaitRepo, "image_ref": "registry.example/acme/api:11105",
		"identity_format": containerimage.ContainerImageIdentityFormatImageRef, "canonical_writes": 1,
	})}
}

// seedWorkItem inserts one reducer work item of the scope's g1 generation.
// A retrying row is invisible until visibleAt, with one counted attempt.
func (w *attachWait) seedWorkItem(id, scope string, domain reducercontract.Domain, status string, visibleAt time.Time) {
	w.t.Helper()
	attempts := 0
	if status == "retrying" {
		attempts = 1
	}
	if _, err := w.db.ExecContext(w.ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, failure_class, visible_at, next_attempt_at, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'scope', $2, $5, $6, CASE WHEN $5 = 'retrying' THEN 'transient' END, $7, $7,
  jsonb_build_object('entity_key', $1::text, 'reason', 'attachment wait proof', 'fact_id', $1::text, 'source_system', 'oci'),
  $8, $8)`, id, scope, fanOutGen(scope, "g1"), string(domain), status, attempts, visibleAt, w.clock.Add(-time.Minute)); err != nil {
		w.t.Fatalf("seed work item %s: %v", id, err)
	}
}

// writeAttachment plays the attachment handler's write through the
// production writer: the attachment fact lands under the scope that ran it.
func (w *attachWait) writeAttachment(scope string, intent reducercontract.Intent) {
	w.t.Helper()
	if err := w.tryWriteAttachment(scope, intent); err != nil {
		w.t.Fatalf("write attachment: %v", err)
	}
}

// tryWriteAttachment is writeAttachment for worker goroutines, which must not
// call t.Fatalf.
func (w *attachWait) tryWriteAttachment(scope string, intent reducercontract.Intent) error {
	writer := sbomattest.PostgresSBOMAttestationAttachmentWriter{DB: postgres.SQLDB{DB: w.db}, Now: func() time.Time { return w.clock }}
	_, err := writer.WriteSBOMAttestationAttachments(w.ctx, sbomattest.SBOMAttestationAttachmentWrite{
		IntentID: intent.IntentID, ScopeID: scope, GenerationID: fanOutGen(scope, "g1"), SourceSystem: "oci",
		Decisions: []sbomattest.SBOMAttestationAttachmentDecision{{
			DocumentID: attachWaitDocument, SubjectDigest: attachWaitDigest,
			AttachmentStatus: sbomattest.SBOMAttachmentAttachedVerified, CanonicalWrites: 1,
		}},
	})
	return err
}

// claim claims one row from q and fails the test when the outcome differs.
func (w *attachWait) claim(q postgres.ReducerQueue, label string, want bool) reducercontract.Intent {
	w.t.Helper()
	intent, claimed, err := q.Claim(w.ctx)
	if err != nil || claimed != want {
		w.t.Fatalf("%s: Claim() claimed=%v err=%v, want claimed=%v", label, claimed, err, want)
	}
	return intent
}

// runImpact handles one claimed impact intent and acks or fails it the way
// the reducer worker does. It returns the handler error.
func (w *attachWait) runImpact(intent reducercontract.Intent) error {
	w.t.Helper()
	result, err := w.handler.Handle(w.ctx, intent)
	if err != nil {
		if failErr := w.impactQ.Fail(w.ctx, intent, err); failErr != nil {
			w.t.Fatalf("impact Fail(): %v (handle error %v)", failErr, err)
		}
		return err
	}
	if ackErr := w.impactQ.Ack(w.ctx, intent, result); ackErr != nil {
		w.t.Fatalf("impact Ack(): %v", ackErr)
	}
	return nil
}

// attachedFindings counts the active, attachment-backed findings for the CVE
// in scope: the finding the attachment exists to produce.
func (w *attachWait) attachedFindings(scope string) int {
	w.t.Helper()
	var count int
	if err := w.db.QueryRowContext(w.ctx, `
SELECT count(*) FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.scope_id = $1 AND f.fact_kind = $2 AND NOT f.is_tombstone
  AND f.payload->>'cve_id' = $3 AND f.payload->>'subject_digest' = $4`,
		scope, facts.ReducerSupplyChainImpactFindingFactKind, attachWaitCVE, attachWaitDigest).Scan(&count); err != nil {
		w.t.Fatalf("count findings: %v", err)
	}
	return count
}

func (w *attachWait) item(id string) (string, string, int) {
	w.t.Helper()
	var status string
	var class sql.NullString
	var attempts int
	if err := w.db.QueryRowContext(w.ctx, `SELECT status, failure_class, attempt_count FROM fact_work_items WHERE work_item_id = $1`, id).
		Scan(&status, &class, &attempts); err != nil {
		w.t.Fatalf("read %s: %v", id, err)
	}
	return status, class.String, attempts
}

func failureClassOf(err error) string {
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	return ""
}

// TestReducerContentionGateSupplyChainImpactWaitsForItsOwnAttachmentLive is T1:
// the attachment row is retrying with a future visible_at, so the impact row
// of the same scope generation is claimable (the conflict fence blocks only a
// claimed or running sibling). The pass must defer without spending an
// attempt and write nothing; the attachment's terminal Ack must reopen it at
// once (before the 10-minute backstop), and the reopened pass must publish
// the attachment-backed finding with no unrelated trigger.
func TestReducerContentionGateSupplyChainImpactWaitsForItsOwnAttachmentLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:image"
	fanOutSeedScope(t, w.ctx, w.db, scope, "g1")
	w.upsert(append(append(w.vulnerabilityFacts(scope), w.documentFacts(scope)...), w.identityFacts(scope)...)...)
	w.seedWorkItem("attach-11105", scope, reducercontract.DomainSBOMAttestationAttachment, "retrying", w.clock.Add(2*time.Minute))
	w.seedWorkItem("impact-11105", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))

	intent := w.claim(w.impactQ, "first impact claim (attachment retrying, not claimed)", true)
	err := w.runImpact(intent)
	if got := failureClassOf(err); got != supplyChainImpactAttachmentNotReadyClass {
		t.Fatalf("first impact pass: error = %v (class %q), want the %s deferral; attached findings = %d (the pass ran before its own attachment)",
			err, got, supplyChainImpactAttachmentNotReadyClass, w.attachedFindings(scope))
	}
	if status, class, attempts := w.item("impact-11105"); status != "retrying" || class != supplyChainImpactAttachmentNotReadyClass || attempts != 1 {
		t.Fatalf("deferred impact row = %s/%s/attempts=%d, want retrying/%s/1", status, class, attempts, supplyChainImpactAttachmentNotReadyClass)
	}
	if got := w.attachedFindings(scope); got != 0 {
		t.Fatalf("attached findings after the deferral = %d, want 0 (a deferral writes nothing)", got)
	}

	// Past the attachment's backoff, inside the impact wait's backstop: the
	// parked impact row stays invisible until the attachment acks.
	w.clock = w.clock.Add(3 * time.Minute)
	w.claim(w.impactQ, "impact claim while parked", false)
	attach := w.claim(w.attachQ, "attachment claim", true)
	w.writeAttachment(scope, attach)
	if err := w.attachQ.Ack(w.ctx, attach, reducercontract.Result{}); err != nil {
		t.Fatalf("attachment Ack(): %v", err)
	}

	reopened := w.claim(w.impactQ, "impact claim after the attachment Ack", true)
	if reopened.AttemptCount != 1 {
		t.Fatalf("reopened AttemptCount = %d, want 1 (the deferral is non-counting)", reopened.AttemptCount)
	}
	if err := w.runImpact(reopened); err != nil {
		t.Fatalf("reopened impact pass: %v", err)
	}
	if got := w.attachedFindings(scope); got != 1 {
		t.Fatalf("attached findings after the reopened pass = %d, want 1", got)
	}
	if status, _, attempts := w.item("impact-11105"); status != "succeeded" || attempts != 1 {
		t.Fatalf("impact terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

// TestReducerContentionGateSupplyChainImpactWithoutAttachmentWorkProceedsLive is
// T2: a scope generation with no attachment work item runs its impact pass
// on the first claim, spends one attempt, and publishes what its own
// evidence supports (here the attachment fact already committed).
func TestReducerContentionGateSupplyChainImpactWithoutAttachmentWorkProceedsLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:no-attachment-work"
	fanOutSeedScope(t, w.ctx, w.db, scope, "g1")
	w.upsert(append(append(w.vulnerabilityFacts(scope), w.documentFacts(scope)...), w.identityFacts(scope)...)...)
	w.writeAttachment(scope, reducercontract.Intent{IntentID: "attach-11105-earlier"})
	w.seedWorkItem("impact-11105-t2", scope, reducercontract.DomainSupplyChainImpact, "pending", w.clock.Add(-time.Minute))

	intent := w.claim(w.impactQ, "impact claim", true)
	if err := w.runImpact(intent); err != nil {
		t.Fatalf("impact pass with no attachment work: %v", err)
	}
	if status, _, attempts := w.item("impact-11105-t2"); status != "succeeded" || attempts != 1 {
		t.Fatalf("impact terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
	if got := w.attachedFindings(scope); got != 1 {
		t.Fatalf("attached findings = %d, want 1", got)
	}
}

// attachWaitTransientError is the retryable failure an attachment handler
// returns when a registry read blips; Fail parks the row retrying with a
// future visible_at, the window in which its impact sibling is claimable.
type attachWaitTransientError struct{}

func (attachWaitTransientError) Error() string   { return "transient registry read failure" }
func (attachWaitTransientError) Retryable() bool { return true }

// workItemCount counts every fact_work_items row of the scope, so a proof can
// show that no unrelated or manually enqueued item took part.
func (w *attachWait) workItemCount(scope string) int {
	w.t.Helper()
	var count int
	if err := w.db.QueryRowContext(w.ctx, `SELECT count(*) FROM fact_work_items WHERE scope_id = $1`, scope).Scan(&count); err != nil {
		w.t.Fatalf("count work items: %v", err)
	}
	return count
}

// TestReducerContentionGateSupplyChainImpactLateAttachmentReachesFindingViaEnqueueLive
// is the #11105 run-then-commit proof on the production queue path. Both
// intents of one scope generation go through ReducerQueue.Enqueue in one call
// (the way the projector emits them) and no work item is seeded by SQL. The
// attachment sorts first and is claimed first, then fails transiently, so its
// row retries with a future visible_at and the impact row becomes claimable.
// The impact pass runs in that window and defers without writing. The
// attachment then commits and is acked, and that Ack alone must reopen the
// parked impact row, which publishes the attachment-backed finding. The proof
// holds the scope to exactly the two enqueued rows, so no unrelated trigger,
// second enqueue, or manual reopen can have produced the finding.
func TestReducerContentionGateSupplyChainImpactLateAttachmentReachesFindingViaEnqueueLive(t *testing.T) {
	w := newAttachWait(t)
	const scope = "scope:11105:enqueue-path"
	seedAttachWaitScope(w, scope)

	generation := fanOutGen(scope, "g1")
	intents := []projectorintent.ReducerIntent{
		{
			ScopeID: scope, GenerationID: generation, Domain: reducercontract.DomainSBOMAttestationAttachment,
			EntityKey: "sbom_attestation_attachment:" + scope, Reason: "sbom or attestation subject evidence observed",
			FactID: "fact:11105:referrer:" + scope, SourceSystem: "oci",
		},
		{
			ScopeID: scope, GenerationID: generation, Domain: reducercontract.DomainSupplyChainImpact,
			EntityKey: "supply_chain_impact:" + scope, Reason: "supply chain evidence observed",
			FactID: "fact:11105:cve:" + scope, SourceSystem: "synthetic",
		},
	}
	if res, err := w.impactQ.Enqueue(w.ctx, intents); err != nil || res.Count != 2 {
		t.Fatalf("Enqueue() count=%d err=%v, want 2 rows admitted", res.Count, err)
	}

	// The attachment is claimed first and fails transiently: it retries with a
	// future visible_at, so the impact sibling is no longer fenced.
	attach := w.claim(w.attachQ, "first attachment claim", true)
	if err := w.attachQ.Fail(w.ctx, attach, attachWaitTransientError{}); err != nil {
		t.Fatalf("attachment Fail(): %v", err)
	}
	if status, _, attempts := w.item(attach.IntentID); status != "retrying" || attempts != 1 {
		t.Fatalf("attachment row = %s attempts=%d, want retrying at 1", status, attempts)
	}

	// The impact pass runs before the attachment commits: it defers and
	// writes nothing.
	impact := w.claim(w.impactQ, "impact claim while the attachment retries", true)
	if got := failureClassOf(w.runImpact(impact)); got != supplyChainImpactAttachmentNotReadyClass {
		t.Fatalf("impact pass before the attachment: class %q, want the %s deferral; attached findings = %d",
			got, supplyChainImpactAttachmentNotReadyClass, w.attachedFindings(scope))
	}
	if got := w.attachedFindings(scope); got != 0 {
		t.Fatalf("attached findings after the deferral = %d, want 0", got)
	}

	// Past the attachment's backoff and inside the impact backstop: the parked
	// impact row stays fenced until the attachment acks.
	w.clock = w.clock.Add(3 * time.Minute)
	w.claim(w.impactQ, "impact claim while parked", false)
	retry := w.claim(w.attachQ, "attachment retry claim", true)
	if retry.IntentID != attach.IntentID {
		t.Fatalf("attachment retry claimed %s, want %s", retry.IntentID, attach.IntentID)
	}
	w.writeAttachment(scope, retry)
	if err := w.attachQ.Ack(w.ctx, retry, reducercontract.Result{}); err != nil {
		t.Fatalf("attachment Ack(): %v", err)
	}

	// The attachment Ack is the only event since the park: the impact row must
	// be claimable now, without a clock advance, a second enqueue, or any
	// other trigger, and the finding must appear.
	reopened := w.claim(w.impactQ, "impact claim after the attachment Ack", true)
	if reopened.IntentID != impact.IntentID || reopened.AttemptCount != 1 {
		t.Fatalf("reopened impact = %s attempt %d, want %s at attempt 1 (the deferral is non-counting)",
			reopened.IntentID, reopened.AttemptCount, impact.IntentID)
	}
	if err := w.runImpact(reopened); err != nil {
		t.Fatalf("reopened impact pass: %v", err)
	}
	if got := w.attachedFindings(scope); got != 1 {
		t.Fatalf("attached findings after the attachment commit = %d, want 1", got)
	}
	if status, _, attempts := w.item(impact.IntentID); status != "succeeded" || attempts != 1 {
		t.Fatalf("impact terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
	if got := w.workItemCount(scope); got != 2 {
		t.Fatalf("work items in scope = %d, want exactly the 2 enqueued (no unrelated or manual trigger)", got)
	}
}
