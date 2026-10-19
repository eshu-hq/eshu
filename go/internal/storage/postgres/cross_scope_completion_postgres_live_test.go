// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
)

func TestCrossScopeCompletionIdentityThenCICDConvergesLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 1, 20, 30, 0, 0, time.UTC)

	const (
		producerScope = "repository:5740-completion-producer"
		producerGen   = "generation:5740-completion-producer"
		consumerScope = "repository:5740-completion-consumer"
		consumerGen   = "generation:5740-completion-consumer"
		identityID    = "reducer_5740_completion_identity"
		cicdID        = "reducer_5740_completion_cicd"
		supplyID      = "reducer_5740_completion_supply"
		leaseOwner    = "reducer-5740-completion"
		fanoutOwner   = "fanout-5740-completion"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, producerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, producerScope, producerGen)
	seedContainerImageIdentityAckScope(t, ctx, db, consumerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, consumerScope, consumerGen)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET active_generation_id = CASE scope_id
    WHEN $1 THEN $2
    WHEN $3 THEN $4
END
WHERE scope_id IN ($1, $3)
`, producerScope, producerGen, consumerScope, consumerGen); err != nil {
		t.Fatalf("activate completion scopes: %v", err)
	}
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, identityID, producerScope, producerGen,
		leaseOwner, now.Add(time.Minute), now,
	)
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, cicdID, consumerScope, consumerGen,
		reducer.DomainCICDRunCorrelation, now,
	)
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, supplyID, consumerScope, consumerGen,
		reducer.DomainSupplyChainImpact, now,
	)

	// #7494: the producer ack fences on the claim attempt stamp and a live
	// lease (last_attempt_at = $N AND claim_until > clock_timestamp()). The
	// seed predates that fence, so stamp the claim the way a real claim
	// would: last_attempt_at plus a DB-clock-relative lease. Advancing the
	// stamp UPDATEs the identity epoch (migration 088 trigger), so the
	// intent below carries the post-claim epoch.
	producerClaimedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET last_attempt_at = $1,
    claim_until = clock_timestamp() + INTERVAL '1 minute',
    container_image_identity_claim_epoch = container_image_identity_claim_epoch + 1
WHERE work_item_id = $2
`, producerClaimedAt, identityID); err != nil {
		t.Fatalf("stamp identity producer claim: %v", err)
	}

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    leaseOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	identity := reducercontract.Intent{
		IntentID:   identityID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: 2,
		ClaimedAt:  &producerClaimedAt,
	}
	if err := queue.Ack(ctx, identity, reducercontract.Result{}); err != nil {
		t.Fatalf("ack identity producer: %v", err)
	}
	if err := queue.Ack(ctx, identity, reducercontract.Result{}); !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("duplicate identity ACK error = %v, want claim rejected", err)
	}
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 1)

	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue:      store,
		LeaseOwner: fanoutOwner,
		LeaseTTL:   time.Minute,
		BatchSize:  500,
		Now:        store.Now,
	}
	processed, first, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run identity completion = %+v processed=%t err=%v", first, processed, err)
	}
	if first.EventsProcessed != 1 || first.IntentsEnqueued != 2 {
		t.Fatalf("identity fanout = %+v, want events=1 intents=2", first)
	}
	assertCrossScopeConsumerState(t, ctx, db, cicdID, "pending", false)
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)

	// #7494: the synthetic downstream claim stamps the attempt and takes a
	// DB-clock-relative lease, like a real claim; the fixed-date lease the
	// seed used predates the ack fence and reads expired on the DB clock.
	downstreamClaimedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running', lease_owner = $2,
    claim_until = clock_timestamp() + INTERVAL '1 minute',
    last_attempt_at = $3
WHERE work_item_id IN ($1, $4)
`, cicdID, leaseOwner, downstreamClaimedAt, supplyID); err != nil {
		t.Fatalf("claim synthetic downstream intents: %v", err)
	}
	if err := queue.Ack(ctx, reducercontract.Intent{
		IntentID:  cicdID,
		Domain:    reducer.DomainCICDRunCorrelation,
		ClaimedAt: &downstreamClaimedAt,
	}, reducercontract.Result{}); err != nil {
		t.Fatalf("ack CI/CD producer: %v", err)
	}
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainCICDRunCorrelation, 1)

	processed, second, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run CI/CD completion = %+v processed=%t err=%v", second, processed, err)
	}
	if second.EventsProcessed != 1 || second.IntentsEnqueued != 1 {
		t.Fatalf("CI/CD fanout = %+v, want events=1 intents=1", second)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "running", true)
	if err := queue.Ack(ctx, reducercontract.Intent{
		IntentID:  supplyID,
		Domain:    reducer.DomainSupplyChainImpact,
		ClaimedAt: &downstreamClaimedAt,
	}, reducercontract.Result{}); err != nil {
		t.Fatalf("ack in-flight supply consumer: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)
	// #7494: the replay re-claim stamps a fresh attempt; each synthetic
	// claim carries its own stamp, like successive real claims.
	replayClaimedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running', lease_owner = $2,
    claim_until = clock_timestamp() + INTERVAL '1 minute',
    last_attempt_at = $3
WHERE work_item_id = $1
`, supplyID, leaseOwner, replayClaimedAt); err != nil {
		t.Fatalf("claim required supply replay: %v", err)
	}
	if err := queue.Ack(ctx, reducercontract.Intent{
		IntentID:  supplyID,
		Domain:    reducer.DomainSupplyChainImpact,
		ClaimedAt: &replayClaimedAt,
	}, reducercontract.Result{}); err != nil {
		t.Fatalf("ack required supply replay: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "succeeded", false)
	// #7494: count only this proof's canonical rows. The production
	// bootstrap seeds its own fact_work_items rows (migration 115's global
	// value-flow refresh), which an unscoped count would sweep in.
	var workItems int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE work_item_id IN ($1, $2, $3)`,
		identityID, cicdID, supplyID).Scan(&workItems); err != nil {
		t.Fatalf("count canonical completion work items: %v", err)
	}
	if workItems != 3 {
		t.Fatalf("completion work items = %d, want exactly 3 canonical rows", workItems)
	}
}

func TestCrossScopeCompletionFanoutSchedulesOnlyEligibleConsumerStatesLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	const (
		scopeID    = "repository:5740-consumer-states"
		generation = "generation:5740-consumer-states"
		owner      = "consumer-state-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate completion consumer-state scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, lease_owner, claim_until,
    payload, created_at, updated_at
)
SELECT 'reducer_5740_state_' || state.status,
       $1, $2, 'reducer', 'supply_chain_impact', state.status,
       'intent', 'reducer_5740_state_' || state.status,
       CASE WHEN state.status IN ('claimed', 'running') THEN $3::text ELSE NULL END,
       CASE WHEN state.status IN ('claimed', 'running') THEN $4::timestamptz ELSE NULL END,
       '{}'::jsonb, $5, $5
FROM unnest(ARRAY['succeeded','claimed','running','pending','retrying','dead_letter'])
     AS state(status)
`, scopeID, generation, owner, now.Add(time.Minute), now); err != nil {
		t.Fatalf("seed completion consumer states: %v", err)
	}
	insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"pending", "", time.Time{}, 0, now.Add(-time.Second),
	)
	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return now }
	lease, ok, err := store.Claim(ctx, owner, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim consumer-state event = %+v ok=%t err=%v", lease, ok, err)
	}
	result, err := store.Fanout(ctx, lease, 500)
	if err != nil {
		t.Fatalf("fanout consumer states: %v", err)
	}
	if result.IntentsEnqueued != 3 {
		t.Fatalf("consumer-state fanout = %+v, want three newly scheduled states", result)
	}
	for _, expected := range []struct {
		id     string
		status string
		replay bool
	}{
		{id: "reducer_5740_state_succeeded", status: "pending", replay: false},
		{id: "reducer_5740_state_claimed", status: "claimed", replay: true},
		{id: "reducer_5740_state_running", status: "running", replay: true},
		{id: "reducer_5740_state_pending", status: "pending", replay: false},
		{id: "reducer_5740_state_retrying", status: "retrying", replay: false},
		{id: "reducer_5740_state_dead_letter", status: "dead_letter", replay: false},
	} {
		assertCrossScopeConsumerState(t, ctx, db, expected.id, expected.status, expected.replay)
	}
}

func insertCrossScopeCompletionBaseConsumer(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	scopeID string,
	generationID string,
	domain reducercontract.Domain,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', $4, 'succeeded',
          'intent', $1, '{}'::jsonb, $5, $5)
`, workItemID, scopeID, generationID, domain, now); err != nil {
		t.Fatalf("insert %s base consumer: %v", domain, err)
	}
}

func assertCrossScopeCompletionEventCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	domain reducercontract.Domain,
	want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM cross_scope_completion_events WHERE producer_domain = $1
`, domain).Scan(&got); err != nil {
		t.Fatalf("count %s completion events: %v", domain, err)
	}
	if got != want {
		t.Fatalf("%s completion events = %d, want %d", domain, got, want)
	}
}

func assertCrossScopeConsumerState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	wantStatus string,
	wantReplay bool,
) {
	t.Helper()
	var status string
	var replay bool
	if err := db.QueryRowContext(ctx, `
SELECT status, cross_scope_replay_required
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&status, &replay); err != nil {
		t.Fatalf("read completion consumer %s: %v", workItemID, err)
	}
	if status != wantStatus || replay != wantReplay {
		t.Fatalf("completion consumer %s = status:%s replay:%t, want %s/%t", workItemID, status, replay, wantStatus, wantReplay)
	}
}

// identitySuppressionLiveWriter wires the real digest-v3 support publish so
// the suppression proofs below exercise the production previous-last_set_id
// comparison, not a canned verdict.
func identitySuppressionLiveWriter(db *sql.DB) containerimage.PostgresContainerImageIdentitySupportWriter {
	return containerimage.PostgresContainerImageIdentitySupportWriter{
		ActivationLookup: NewContainerImageIdentityScopeStateStore(SQLDB{DB: db}),
		ClaimedExecer:    ContainerImageIdentityClaimedExecer{DB: SQLDB{DB: db}},
	}
}

// stampIdentitySuppressionLiveClaim stamps a hand-seeded identity producer row
// the way a real queue claim would (#7691): last_attempt_at plus a
// DB-clock-relative lease and the next claim epoch, with the v2/v3
// authorization flags the support publish fences on set to the row's current
// claimed status in the same statement. It returns the epoch the ack intent
// must carry.
func stampIdentitySuppressionLiveClaim(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
) (claimedAt time.Time, claimEpoch int64) {
	t.Helper()
	claimedAt = time.Now().UTC().Truncate(time.Microsecond)
	if err := db.QueryRowContext(ctx, `
UPDATE fact_work_items
SET last_attempt_at = $1,
    claim_until = clock_timestamp() + INTERVAL '1 minute',
    container_image_identity_claim_epoch = container_image_identity_claim_epoch + 1,
    container_image_identity_v2_required = TRUE,
    container_image_identity_v2_authorized_status = status,
    container_image_identity_v3_required = TRUE,
    container_image_identity_v3_authorized_status = status
WHERE work_item_id = $2
RETURNING container_image_identity_claim_epoch
`, claimedAt, workItemID).Scan(&claimEpoch); err != nil {
		t.Fatalf("stamp identity suppression claim %s: %v", workItemID, err)
	}
	return claimedAt, claimEpoch
}

// seedIdentitySuppressionLiveGenerationAs inserts a generation in a
// non-active status so a later test step can supersede the current active
// generation first (scope_generations_active_scope_idx admits only one
// active generation per scope).
func seedIdentitySuppressionLiveGenerationAs(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	status string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    $2, $1, 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), $3
)
`, scopeID, generationID, status); err != nil {
		t.Fatalf("seed suppression generation %s: %v", generationID, err)
	}
}

// publishIdentitySuppressionLiveSet publishes one support set through the
// production writer and returns the previous-last_set_id verdict the ACK
// gate reads.
func publishIdentitySuppressionLiveSet(
	t *testing.T,
	ctx context.Context,
	writer containerimage.PostgresContainerImageIdentitySupportWriter,
	intentID string,
	scopeID string,
	generationID string,
	claimEpoch int64,
	digest string,
) bool {
	t.Helper()
	epoch, err := writer.ContainerImageIdentityActivationEpoch(ctx, scopeID, generationID)
	if err != nil {
		t.Fatalf("read identity activation epoch: %v", err)
	}
	result, err := writer.WriteContainerImageIdentityDecisions(ctx, containerimage.ContainerImageIdentityWrite{
		IntentID:        intentID,
		ClaimEpoch:      claimEpoch,
		ActivationEpoch: epoch,
		ScopeID:         scopeID,
		GenerationID:    generationID,
		SourceSystem:    "git",
		Cause:           "8284-live-proof",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0).UTC(),
		Decisions: []containerimage.ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/8284@" + digest,
				Digest:           digest,
				RepositoryID:     "repository:8284",
				Outcome:          reducercontract.ContainerImageIdentityExactDigest,
				CanonicalWrites:  1,
				IdentityStrength: "immutable_digest",
			},
		},
	})
	if err != nil {
		t.Fatalf("publish identity support set: %v", err)
	}
	return result.SupportChanged
}

// ackIdentitySuppressionLiveItem ACKs one identity intent with the paired
// support-changed verdict, the way the handler's Result carries it.
func ackIdentitySuppressionLiveItem(
	t *testing.T,
	ctx context.Context,
	queue ReducerQueue,
	intent reducercontract.Intent,
	changed bool,
) {
	t.Helper()
	if err := queue.Ack(ctx, intent, reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainContainerImageIdentity,
		Status:   reducercontract.ResultStatusSucceeded,
		SubSignals: containerimage.WithSupportChangedSignal(
			nil,
			changed,
		),
	}); err != nil {
		t.Fatalf("ack identity item %s: %v", intent.IntentID, err)
	}
}

// claimIdentitySuppressionLiveConsumer claims a fanned-out succeeded consumer
// the way a real downstream worker would, so the proof can run it to
// succeeded again.
func claimIdentitySuppressionLiveConsumer(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	leaseOwner string,
) time.Time {
	t.Helper()
	claimedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running', lease_owner = $2,
    claim_until = clock_timestamp() + INTERVAL '1 minute',
    last_attempt_at = $3
WHERE work_item_id = $1
`, workItemID, leaseOwner, claimedAt); err != nil {
		t.Fatalf("claim suppression consumer %s: %v", workItemID, err)
	}
	return claimedAt
}

// TestCrossScopeCompletionSuppressesUnchangedIdentityReplayLive is the #8284
// suppression proof: a byte-identical identity replay still ACKs to
// succeeded, but emits no completion event, so the fanout never reopens the
// downstream consumer. A changed set on the same scope still emits.
func TestCrossScopeCompletionSuppressesUnchangedIdentityReplayLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.October, 19, 12, 30, 0, 0, time.UTC)

	const (
		producerScope = "repository:8284-suppression-producer"
		producerGen   = "generation:8284-suppression-producer"
		consumerScope = "repository:8284-suppression-consumer"
		consumerGen   = "generation:8284-suppression-consumer"
		firstID       = "reducer_8284_suppression_first"
		replayID      = "reducer_8284_suppression_replay"
		changedID     = "reducer_8284_suppression_changed"
		supplyID      = "reducer_8284_suppression_supply"
		leaseOwner    = "reducer-8284-suppression"
		fanoutOwner   = "fanout-8284-suppression"
	)
	const (
		digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, producerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, producerScope, producerGen)
	seedContainerImageIdentityAckScope(t, ctx, db, consumerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, consumerScope, consumerGen)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET active_generation_id = CASE scope_id
    WHEN $1 THEN $2
    WHEN $3 THEN $4
END
WHERE scope_id IN ($1, $3)
`, producerScope, producerGen, consumerScope, consumerGen); err != nil {
		t.Fatalf("activate suppression scopes: %v", err)
	}
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, supplyID, consumerScope, consumerGen,
		reducer.DomainSupplyChainImpact, now,
	)

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    leaseOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	writer := identitySuppressionLiveWriter(db)
	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue:      store,
		LeaseOwner: fanoutOwner,
		LeaseTTL:   time.Minute,
		BatchSize:  500,
		Now:        store.Now,
	}

	// First publish: new content, so the verdict is changed and the ACK
	// emits. The fanout reopens the succeeded consumer.
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, firstID, producerScope, producerGen,
		leaseOwner, now.Add(time.Minute), now,
	)
	firstClaimedAt, firstEpoch := stampIdentitySuppressionLiveClaim(t, ctx, db, firstID)
	if changed := publishIdentitySuppressionLiveSet(
		t, ctx, writer, firstID, producerScope, producerGen, firstEpoch, digestA,
	); !changed {
		t.Fatalf("first publish supportChanged = false, want true for new content")
	}
	ackIdentitySuppressionLiveItem(t, ctx, queue, reducercontract.Intent{
		IntentID:   firstID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: firstEpoch,
		ClaimedAt:  &firstClaimedAt,
	}, true)
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 1)
	processed, first, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run first completion = %+v processed=%t err=%v", first, processed, err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)

	// Run the reopened consumer back to succeeded.
	supplyClaimedAt := claimIdentitySuppressionLiveConsumer(t, ctx, db, supplyID, leaseOwner)
	if err := queue.Ack(ctx, reducercontract.Intent{
		IntentID:  supplyID,
		Domain:    reducer.DomainSupplyChainImpact,
		ClaimedAt: &supplyClaimedAt,
	}, reducercontract.Result{}); err != nil {
		t.Fatalf("ack suppression consumer replay: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "succeeded", false)

	// Byte-identical replay: the publish verdict is unchanged and the ACK
	// emits nothing, so a further fanout has nothing to process and the
	// consumer stays succeeded.
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, replayID, producerScope, producerGen,
		leaseOwner, now.Add(time.Minute), now,
	)
	replayClaimedAt, replayEpoch := stampIdentitySuppressionLiveClaim(t, ctx, db, replayID)
	if changed := publishIdentitySuppressionLiveSet(
		t, ctx, writer, replayID, producerScope, producerGen, replayEpoch, digestA,
	); changed {
		t.Fatalf("replay publish supportChanged = true, want false for identical content")
	}
	ackIdentitySuppressionLiveItem(t, ctx, queue, reducercontract.Intent{
		IntentID:   replayID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: replayEpoch,
		ClaimedAt:  &replayClaimedAt,
	}, false)
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 0)
	if processed, _, err := runner.RunOnce(ctx); err != nil || processed {
		t.Fatalf("replay fanout processed=%t err=%v, want no event to process", processed, err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "succeeded", false)

	// Changed content on the same scope still emits and reopens.
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, changedID, producerScope, producerGen,
		leaseOwner, now.Add(time.Minute), now,
	)
	changedClaimedAt, changedEpoch := stampIdentitySuppressionLiveClaim(t, ctx, db, changedID)
	if changed := publishIdentitySuppressionLiveSet(
		t, ctx, writer, changedID, producerScope, producerGen, changedEpoch, digestB,
	); !changed {
		t.Fatalf("changed publish supportChanged = false, want true for new content")
	}
	ackIdentitySuppressionLiveItem(t, ctx, queue, reducercontract.Intent{
		IntentID:   changedID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: changedEpoch,
		ClaimedAt:  &changedClaimedAt,
	}, true)
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 1)
	processed, _, err = runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run changed completion processed=%t err=%v", processed, err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)
}

// TestCrossScopeCompletionIdentityActivationRaceReopensConsumerLive is the
// #8284 activation-race proof: a producer scope that activates after the
// consumer's first success, then publishes content that differs from its
// preserved last_set_id, still emits and reopens the consumer. The verdict
// compares against last_set_id (which activation preserves), not
// active_set_id (which activation resets to NULL): a byte-identical first
// publish after the activation stays suppressed.
func TestCrossScopeCompletionIdentityActivationRaceReopensConsumerLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.October, 19, 12, 30, 0, 0, time.UTC)

	const (
		producerScope = "repository:8284-activation-producer"
		producerGenA  = "generation:8284-activation-a"
		producerGenB  = "generation:8284-activation-b"
		consumerScope = "repository:8284-activation-consumer"
		consumerGen   = "generation:8284-activation-consumer"
		firstID       = "reducer_8284_activation_first"
		secondID      = "reducer_8284_activation_second"
		supplyID      = "reducer_8284_activation_supply"
		leaseOwner    = "reducer-8284-activation"
		fanoutOwner   = "fanout-8284-activation"
	)
	const (
		digestA = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		digestB = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, producerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, producerScope, producerGenA)
	seedContainerImageIdentityAckScope(t, ctx, db, consumerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, consumerScope, consumerGen)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET active_generation_id = CASE scope_id
    WHEN $1 THEN $2
    WHEN $3 THEN $4
END
WHERE scope_id IN ($1, $3)
`, producerScope, producerGenA, consumerScope, consumerGen); err != nil {
		t.Fatalf("activate race scopes: %v", err)
	}
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, supplyID, consumerScope, consumerGen,
		reducer.DomainSupplyChainImpact, now,
	)

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    leaseOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	writer := identitySuppressionLiveWriter(db)
	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue:      store,
		LeaseOwner: fanoutOwner,
		LeaseTTL:   time.Minute,
		BatchSize:  500,
		Now:        store.Now,
	}
	fanoutAndSucceedConsumer := func() {
		t.Helper()
		processed, _, err := runner.RunOnce(ctx)
		if err != nil || !processed {
			t.Fatalf("run race completion processed=%t err=%v", processed, err)
		}
		assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)
		supplyClaimedAt := claimIdentitySuppressionLiveConsumer(t, ctx, db, supplyID, leaseOwner)
		if err := queue.Ack(ctx, reducercontract.Intent{
			IntentID:  supplyID,
			Domain:    reducer.DomainSupplyChainImpact,
			ClaimedAt: &supplyClaimedAt,
		}, reducercontract.Result{}); err != nil {
			t.Fatalf("ack race consumer replay: %v", err)
		}
		assertCrossScopeConsumerState(t, ctx, db, supplyID, "succeeded", false)
	}

	// Consumer's first success, driven by the producer's first publish.
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, firstID, producerScope, producerGenA,
		leaseOwner, now.Add(time.Minute), now,
	)
	firstClaimedAt, firstEpoch := stampIdentitySuppressionLiveClaim(t, ctx, db, firstID)
	if changed := publishIdentitySuppressionLiveSet(
		t, ctx, writer, firstID, producerScope, producerGenA, firstEpoch, digestA,
	); !changed {
		t.Fatalf("first publish supportChanged = false, want true for new content")
	}
	ackIdentitySuppressionLiveItem(t, ctx, queue, reducercontract.Intent{
		IntentID:   firstID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: firstEpoch,
		ClaimedAt:  &firstClaimedAt,
	}, true)
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 1)
	fanoutAndSucceedConsumer()

	// The producer activates a new generation after that success. The
	// activation resets active_set_id but preserves last_set_id.
	seedIdentitySuppressionLiveGenerationAs(t, ctx, db, producerScope, producerGenB, "pending")
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1
`, producerGenA); err != nil {
		t.Fatalf("supersede producer first generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'active' WHERE generation_id = $1
`, producerGenB); err != nil {
		t.Fatalf("activate producer second generation row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, producerScope, producerGenB); err != nil {
		t.Fatalf("activate producer second generation: %v", err)
	}
	var activeSetID []byte
	var lastSetID []byte
	if err := db.QueryRowContext(ctx, `
SELECT active_set_id, last_set_id FROM container_image_identity_scope_state WHERE scope_id = $1
`, producerScope).Scan(&activeSetID, &lastSetID); err != nil {
		t.Fatalf("read producer scope state after activation: %v", err)
	}
	if activeSetID != nil {
		t.Fatalf("active_set_id after activation = %x, want NULL", activeSetID)
	}
	if len(lastSetID) == 0 {
		t.Fatalf("last_set_id after activation is empty, want the preserved first set")
	}

	// First publish after the activation carries different content: the
	// verdict compares against the preserved last_set_id, so it is changed,
	// the ACK emits, and the fanout reopens the consumer.
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, secondID, producerScope, producerGenB,
		leaseOwner, now.Add(time.Minute), now,
	)
	secondClaimedAt, secondEpoch := stampIdentitySuppressionLiveClaim(t, ctx, db, secondID)
	if changed := publishIdentitySuppressionLiveSet(
		t, ctx, writer, secondID, producerScope, producerGenB, secondEpoch, digestB,
	); !changed {
		t.Fatalf("post-activation changed publish supportChanged = false, want true")
	}
	ackIdentitySuppressionLiveItem(t, ctx, queue, reducercontract.Intent{
		IntentID:   secondID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: secondEpoch,
		ClaimedAt:  &secondClaimedAt,
	}, true)
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 1)
	processed, _, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run post-activation completion processed=%t err=%v", processed, err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)
}
