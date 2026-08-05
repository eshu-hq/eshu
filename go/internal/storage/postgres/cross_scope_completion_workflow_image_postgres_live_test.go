// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type workflowImageForcedOrderLoader struct {
	*FactStore
	identityFacts []facts.Envelope
}

func (l workflowImageForcedOrderLoader) ListActiveCICDRunCorrelationFacts(
	context.Context,
	[]string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.identityFacts...), nil
}

type workflowImageForcedOrderWriter struct {
	writes []reducer.CICDRunCorrelationWrite
}

func (w *workflowImageForcedOrderWriter) WriteCICDRunCorrelations(
	_ context.Context,
	write reducer.CICDRunCorrelationWrite,
) (reducer.CICDRunCorrelationWriteResult, error) {
	w.writes = append(w.writes, write)
	canonicalWrites := 0
	for _, decision := range write.Decisions {
		canonicalWrites += decision.CanonicalWrites
	}
	return reducer.CICDRunCorrelationWriteResult{
		CanonicalWrites: canonicalWrites,
		FactsWritten:    len(write.Decisions),
	}, nil
}

func TestWorkflowImageCompletionForcedOrderConvergesLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 4, 20, 0, 0, 0, time.UTC)

	const (
		repositoryID  = "repository:5703-forced-order"
		ciScopeID     = "ci_cd_run:github_actions:5703-forced-order"
		ciGeneration  = "generation:5703-forced-order-ci"
		gitScopeID    = "git-repository-scope:repository:5703-forced-order"
		gitGeneration = "generation:5703-forced-order-git"
		identityID    = "reducer_5703_forced_order_identity"
		correlationID = "reducer_5703_forced_order_correlation"
		leaseOwner    = "reducer-5703-forced-order"
		fanoutOwner   = "fanout-5703-forced-order"
		imageRef      = "ghcr.io/acme/forced-order:v1"
		digest        = "sha256:5703570357035703570357035703570357035703570357035703570357035703"
	)
	seedWorkflowImageForcedOrderScopes(t, ctx, db, repositoryID, ciScopeID, ciGeneration, gitScopeID, gitGeneration)
	insertWorkflowImageForcedOrderRun(t, ctx, db, ciScopeID, ciGeneration, repositoryID, now)

	loader := workflowImageForcedOrderLoader{
		FactStore: NewFactStore(SQLDB{DB: db}),
		identityFacts: []facts.Envelope{{
			FactID:   "identity-5703-forced-order",
			FactKind: "reducer_container_image_identity",
			Payload: map[string]any{
				"image_ref":                       imageRef,
				"digest":                          digest,
				"build_provenance_repository_ids": []string{repositoryID},
			},
		}},
	}
	writer := &workflowImageForcedOrderWriter{}
	handler := reducer.CICDRunCorrelationHandler{FactLoader: loader, Writer: writer}
	intent := reducer.Intent{
		IntentID:     correlationID,
		ScopeID:      ciScopeID,
		GenerationID: ciGeneration,
		SourceSystem: "ci_cd_run",
		Domain:       reducer.DomainCICDRunCorrelation,
		Cause:        "forced-order convergence proof",
	}

	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("initial CI correlation before workflow evidence: %v", err)
	}
	initial := forcedOrderDecision(t, writer.writes[len(writer.writes)-1])
	if initial.CorrelationKind == "workflow_image" || initial.CanonicalWrites != 0 {
		t.Fatalf("initial decision = %#v, want successful pre-evidence non-workflow result", initial)
	}

	insertWorkflowImageForcedOrderEvidence(t, ctx, db, gitScopeID, gitGeneration, repositoryID, imageRef, now.Add(time.Minute))
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, identityID, gitScopeID, gitGeneration,
		leaseOwner, now.Add(2*time.Minute), now.Add(time.Minute),
	)
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, correlationID, ciScopeID, ciGeneration,
		reducer.DomainCICDRunCorrelation, now,
	)

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    leaseOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(time.Minute) },
	}
	identityIntent := reducer.Intent{
		IntentID:   identityID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: 1,
	}
	if err := queue.Ack(ctx, identityIntent, reducer.Result{}); err != nil {
		t.Fatalf("ack workflow-triggered identity producer: %v", err)
	}
	if err := queue.Ack(ctx, identityIntent, reducer.Result{}); !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("duplicate identity completion error = %v, want claim rejected", err)
	}
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 1)

	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue:      store,
		LeaseOwner: fanoutOwner,
		LeaseTTL:   time.Minute,
		BatchSize:  500,
		Now:        store.Now,
	}
	processed, fanout, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("fanout identity completion = %+v processed=%t err=%v", fanout, processed, err)
	}
	if fanout.EventsProcessed != 1 || fanout.IntentsEnqueued != 1 {
		t.Fatalf("identity fanout = %+v, want one event and one CI replay", fanout)
	}
	assertCrossScopeConsumerState(t, ctx, db, correlationID, "pending", false)

	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("replayed CI correlation after workflow evidence: %v", err)
	}
	converged := forcedOrderDecision(t, writer.writes[len(writer.writes)-1])
	if converged.CorrelationKind != "workflow_image" || converged.ImageRef != imageRef || converged.CanonicalWrites != 1 {
		t.Fatalf("converged decision = %#v, want exact workflow-image correlation", converged)
	}

	processed, duplicate, err := runner.RunOnce(ctx)
	if err != nil || processed || duplicate.EventsProcessed != 0 {
		t.Fatalf("duplicate completion run = %+v processed=%t err=%v, want empty idempotent pass", duplicate, processed, err)
	}
}

func seedWorkflowImageForcedOrderScopes(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	repositoryID, ciScopeID, ciGeneration, gitScopeID, gitGeneration string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ($1, 'ci_cd_run', 'ci_cd_run', $1, 'ci_cd_run', $3, clock_timestamp(), clock_timestamp(), 'active', $2),
	($4, 'repository', 'git', $3, 'git', $3, clock_timestamp(), clock_timestamp(), 'active', $5)
`, ciScopeID, ciGeneration, repositoryID, gitScopeID, gitGeneration); err != nil {
		t.Fatalf("seed forced-order scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status
) VALUES
    ($2, $1, 'synthetic', FALSE, clock_timestamp(), clock_timestamp(), 'active'),
	($4, $3, 'synthetic', TRUE, clock_timestamp(), clock_timestamp(), 'active')
`, ciScopeID, ciGeneration, gitScopeID, gitGeneration); err != nil {
		t.Fatalf("seed forced-order generations: %v", err)
	}
}

func insertWorkflowImageForcedOrderRun(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID, repositoryID string,
	observedAt time.Time,
) {
	t.Helper()
	insertWorkflowImageForcedOrderFact(
		t, ctx, db,
		"ci-run-5703-forced-order", scopeID, generationID, facts.CICDRunFactKind,
		"ci-run-5703-forced-order", "ci_cd_run", observedAt,
		`{"provider":"github_actions","run_id":"run-5703","run_attempt":"1","repository_id":"`+repositoryID+`","commit_sha":"commit-5703","status":"completed","result":"success"}`,
	)
}

func insertWorkflowImageForcedOrderEvidence(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID, repositoryID, imageRef string,
	observedAt time.Time,
) {
	t.Helper()
	insertWorkflowImageForcedOrderFact(
		t, ctx, db,
		"workflow-image-5703-forced-order", scopeID, generationID, facts.CICDWorkflowImageEvidenceFactKind,
		"workflow-image-5703-forced-order", "git", observedAt,
		`{"repository_id":"`+repositoryID+`","commit_sha":"commit-5703","workflow_path":".github/workflows/deploy.yml","command_kind":"docker_build","evidence_class":"workflow_image_ref","image_ref":"`+imageRef+`","source_category":"static_workflow"}`,
	)
}

func insertWorkflowImageForcedOrderFact(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	factID, scopeID, generationID, factKind, stableKey, collectorKind string,
	observedAt time.Time,
	payload string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, fencing_token, source_confidence,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES ($1, $2, $3, $4, $5, '1.0.0', $6, 1, 'observed', $6, $1, $7, $7, FALSE, $8::jsonb)
`, factID, scopeID, generationID, factKind, stableKey, collectorKind, observedAt, payload); err != nil {
		t.Fatalf("insert forced-order fact %s: %v", factID, err)
	}
}

func forcedOrderDecision(
	t *testing.T,
	write reducer.CICDRunCorrelationWrite,
) reducer.CICDRunCorrelationDecision {
	t.Helper()
	if len(write.Decisions) != 1 {
		t.Fatalf("decisions = %#v, want exactly one", write.Decisions)
	}
	return write.Decisions[0]
}
