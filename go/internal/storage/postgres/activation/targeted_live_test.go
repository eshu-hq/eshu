// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestActivationObligationRealCatalogChangeIsHeldThenCompletedLive (#7584)
// drives the production partition-scoped maintainer
// through real refusals: a quiet generation of an existing repository is owed
// while a new repository scope is onboarded. With a stable repository holding
// a memo, the targeted pass refuses with catalog_changed; without one it
// refuses with no_memo_baseline. Either way the consumer holds (one callback
// per lease, nothing inside the lease), the epoch whole pass the onboarding
// commit would trigger runs (here, by the test), and the next cycle after the
// lease completes both obligations without another callback. The consumer
// itself never runs a whole-corpus pass.
func TestActivationObligationRealCatalogChangeIsHeldThenCompletedLive(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stableRepo bool
		reason     string
	}{
		{"catalog_changed", true, "catalog_changed"},
		{"no_memo_baseline", false, "no_memo_baseline"},
	} {
		t.Run(tc.name, func(t *testing.T) { runRealRefusalHold(t, tc.stableRepo, tc.reason) })
	}
}

func runRealRefusalHold(t *testing.T, stableRepo bool, reason string) {
	t.Helper()
	ctx, database := openActivationObligationProofDB(t, "activation_real_catalog")
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "7584-real-catalog-projector", time.Minute)
	activate := func(scopeID, generationID, repoID string, later time.Duration) {
		t.Helper()
		fact := activationRepositoryFact("fact-"+generationID, scopeID, generationID, repoID,
			"https://github.com/acme/"+repoID+".git")
		fact.ObservedAt = fact.ObservedAt.Add(later)
		commitActivationRepository(t, ctx, store, fact, repoID)
		work := claimActivationProjectorWork(t, ctx, queue, scopeID, generationID)
		if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
			t.Fatal(err)
		}
	}
	activate("git:catalog-x", "gen-x-1", "repo-x", 0)
	// A stable repository keeps an active memo through the whole proof, so
	// the targeted pass has a baseline to detect the catalog change against.
	if stableRepo {
		activate("git:catalog-z", "gen-z-1", "repo-z", 0)
	}
	// The epoch whole pass writes every active partition's memo at the
	// current catalog fingerprint: the baseline the targeted pass needs.
	if err := postgres.NewIngestionStore(postgres.SQLDB{DB: database}).RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	activate("git:catalog-x", "gen-x-2", "repo-x", time.Hour) // quiet generation, owed
	activate("git:catalog-y", "gen-y-1", "repo-y", time.Hour) // onboarding changes the catalog

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("real-catalog"))
	if err != nil {
		t.Fatal(err)
	}
	port := &countingRealMaintainer{inner: postgres.NewActivationMaintainer(postgres.NewIngestionStore(postgres.SQLDB{DB: database}), nil, instruments)}
	runner := &maintenance.ActivationObligationRunner{
		Store:       activation.RunnerStore{Store: activation.NewStore(postgres.SQLDB{DB: database})},
		Maintainer:  port,
		Config:      maintenance.ActivationObligationRunnerConfig{Owner: "7584-real-catalog-consumer", Lease: 500 * time.Millisecond},
		Instruments: instruments,
	}
	for i := 0; i < 2; i++ { // the second cycle runs inside the lease
		if _, err := runner.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertObligationStateToken(t, ctx, database, "git:catalog-x", "gen-x-2", "leased", 1)
	assertObligationStateToken(t, ctx, database, "git:catalog-y", "gen-y-1", "leased", 1)
	if got := port.calls.Load(); got != 2 {
		t.Fatalf("callbacks inside one lease = %d, want 2 (one per owed generation)", got)
	}
	rm := collectRealCatalogMetrics(t, ctx, reader)
	if got := counterValue(rm, "eshu_dp_activation_obligation_failures_total", "reason", reason); got != 2 {
		t.Fatalf("%s holds = %d, want 2", reason, got)
	}
	if got := counterValue(rm, "eshu_dp_activation_obligation_failures_total", "reason", "maintenance"); got != 0 {
		t.Fatalf("maintenance failures = %d, want 0 (a refusal is a hold)", got)
	}
	if err := postgres.NewIngestionStore(postgres.SQLDB{DB: database}).RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatal(err) // the epoch pass the onboarding commit triggers
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertObligationStateToken(t, ctx, database, "git:catalog-x", "gen-x-2", "completed", 2)
	assertObligationStateToken(t, ctx, database, "git:catalog-y", "gen-y-1", "completed", 2)
	if got := port.calls.Load(); got != 2 {
		t.Fatalf("callbacks after the epoch pass = %d, want still 2", got)
	}
	rm = collectRealCatalogMetrics(t, ctx, reader)
	if got := histogramCount(rm, "eshu_dp_deferred_backfill_duration_seconds", ""); got != 0 {
		t.Fatalf("whole-corpus passes on the consumer's instruments = %d, want 0", got)
	}
	if got := histogramCount(rm, "eshu_dp_deferred_backfill_targeted_duration_seconds", reason); got != 2 {
		t.Fatalf("targeted %s refused passes = %d, want 2", reason, got)
	}
}

type countingRealMaintainer struct {
	inner  maintenance.ActivationMaintainer
	calls  atomic.Int32
	mu     sync.Mutex
	perGen map[string]int
}

func (c *countingRealMaintainer) MaintainActivation(ctx context.Context, work maintenance.ActivationObligation) error {
	c.calls.Add(1)
	c.mu.Lock()
	if c.perGen == nil {
		c.perGen = map[string]int{}
	}
	c.perGen[work.GenerationID]++
	c.mu.Unlock()
	return c.inner.MaintainActivation(ctx, work)
}

func (c *countingRealMaintainer) callsFor(generationID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.perGen[generationID]
}

func collectRealCatalogMetrics(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

// TestActivationObligationRealCollisionLoserIsInapplicableLive: two
// repository scopes share one repo_id. The production maintainer runs the
// partition-scoped pass for the loser's obligation, the pass classifies it
// inapplicable before any refusal, and the consumer retires it after exactly
// one callback; it is never reclaimed.
func TestActivationObligationRealCollisionLoserIsInapplicableLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_real_collision")
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "7584-real-collision-projector", time.Minute)
	for _, r := range []struct {
		scope, generation string
		later             time.Duration
	}{{"git:real-loser", "gen-real-loser", 0}, {"git:real-winner", "gen-real-winner", time.Hour}} {
		fact := activationRepositoryFact("fact-"+r.generation, r.scope, r.generation,
			"repo-real-shared", "https://github.com/acme/real-shared.git")
		fact.ObservedAt = fact.ObservedAt.Add(r.later)
		commitActivationRepository(t, ctx, store, fact, "repo-real-shared")
		work := claimActivationProjectorWork(t, ctx, queue, r.scope, r.generation)
		if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
			t.Fatal(err)
		}
	}
	port := &countingRealMaintainer{inner: postgres.NewActivationMaintainer(postgres.NewIngestionStore(postgres.SQLDB{DB: database}), nil, nil)}
	runner := &maintenance.ActivationObligationRunner{
		Store:      activation.RunnerStore{Store: activation.NewStore(postgres.SQLDB{DB: database})},
		Maintainer: port,
		Config:     maintenance.ActivationObligationRunnerConfig{Owner: "7584-real-collision-consumer", Lease: 300 * time.Millisecond},
	}
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertObligationStateToken(t, ctx, database, "git:real-loser", "gen-real-loser", "inapplicable", 1)
	if got := port.callsFor("gen-real-loser"); got != 1 {
		t.Fatalf("callbacks for the collision loser = %d, want 1", got)
	}
	time.Sleep(450 * time.Millisecond)
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertObligationStateToken(t, ctx, database, "git:real-loser", "gen-real-loser", "inapplicable", 1)
	// The winner (no memo baseline yet) is held and retried after its lease;
	// the loser is never reclaimed, so its single callback is not repeated.
	if got := port.callsFor("gen-real-loser"); got != 1 {
		t.Fatalf("callbacks for the collision loser after the lease = %d, want still 1", got)
	}
	if got := port.callsFor("gen-real-winner"); got != 2 {
		t.Fatalf("callbacks for the held winner = %d, want 2 (one per lease)", got)
	}
}

// TestActivationObligationBlockedMaintenanceIsCancelledBeforeTheLeaseLive:
// a maintenance callback that blocks is cancelled before the lease
// ends; nothing completes, the obligation stays leased to the first owner
// until expiry, and then another claimer reclaims it with a higher token.
func TestActivationObligationBlockedMaintenanceIsCancelledBeforeTheLeaseLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_blocked_maintenance")
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "7584-blocked-projector", time.Minute)
	commitActivationRepository(t, ctx, store, activationRepositoryFact("fact-blocked", "git:blocked", "gen-blocked",
		"repo-blocked", "https://github.com/acme/blocked.git"), "repo-blocked")
	work := claimActivationProjectorWork(t, ctx, queue, "git:blocked", "gen-blocked")
	if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	var cancelledBy error
	blocked := &countingActivationMaintainer{decide: func(ctx context.Context, _ maintenance.ActivationObligation) error {
		<-ctx.Done()
		cancelledBy = ctx.Err()
		return ctx.Err()
	}}
	runner := newLiveActivationRunner(database, blocked, time.Second)
	started := time.Now()
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second || !errors.Is(cancelledBy, context.DeadlineExceeded) {
		t.Fatalf("blocked callback ended after %s with %v, want cancelled by its deadline before the 1 s lease", elapsed, cancelledBy)
	}
	assertObligationStateToken(t, ctx, database, "git:blocked", "gen-blocked", "leased", 1)
	time.Sleep(time.Second) // past the lease on any clock
	reclaimed, err := activation.NewStore(postgres.SQLDB{DB: database}).Claim(ctx, "7584-second-claimer", time.Minute)
	if err != nil || reclaimed == nil || reclaimed.GenerationID != "gen-blocked" || reclaimed.LeaseToken != 2 {
		t.Fatalf("second claimer = %+v err=%v, want gen-blocked with token 2", reclaimed, err)
	}
}
