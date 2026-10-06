// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// composedGen is one quiet owed generation of the step-4 corpus.
type composedGen struct{ scope, gen, repo, name, alias string }

var (
	composedTgt2  = composedGen{"git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api"}
	composedSolo2 = composedGen{"git:solo", "solo-2", "repo-solo", "ledger-svc", "orders-api"}
)

// setupComposed seeds the corpus, activates each quiet generation, and owes
// exactly their obligations through the production catch-up.
func setupComposed(t *testing.T, ctx context.Context, database *sql.DB, gens ...composedGen) {
	t.Helper()
	p := seedComposedCorpus(t, ctx, database)
	for _, g := range gens {
		quietOwed(p, g.scope, g.gen, g.repo, g.name, g.alias)
	}
	if got := owePending(t, ctx, database); got != len(gens) {
		t.Fatalf("catch-up owed %d obligations, want %d", got, len(gens))
	}
}

// epochPass is the ingester's maintenance through the deferred barrier path:
// a single-shard fleet that committed runs the whole pass, which takes
// AcquireDeferredMaintenanceRepoExclusiveLocks per batch and per publish.
func epochPass(ctx context.Context, store IngestionStore) error {
	return store.RunDeferredRelationshipMaintenanceAfterShardDrain(ctx,
		DeferredMaintenanceBarrierConfig{ShardCount: 1, ShardIndex: 0, HasCommitted: true}, nil, nil)
}

// composedIterations is how many times the overlap race repeats (default 20).
func composedIterations(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("ESHU_ACTIVATION_COMPOSED_ITERATIONS")
	if raw == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		t.Fatalf("ESHU_ACTIVATION_COMPOSED_ITERATIONS=%q, want a positive integer", raw)
	}
	return n
}

// TestActivationObligationConsumerAndEpochPassOverlapLive (#7584 D3 step 4,
// item 1): a consumer cycle with the production partition-scoped maintainer
// and an ingester epoch pass through the deferred barrier run on overlapping
// repositories, released together from a channel barrier. Both take the
// per-repository exclusive maintenance locks. Every iteration must end with
// no error (no 40P01), no recorded failure, the obligation completed exactly
// once (token 1, at most one callback), the waiting row woken, and the
// durable state equal to one of the two sequential orders.
func TestActivationObligationConsumerAndEpochPassOverlapLive(t *testing.T) {
	references := map[string][]string{}
	for _, order := range []string{"epoch_first", "consumer_first"} {
		ctx, database := openActivationObligationProofDB(t, "act_compose_ref")
		setupComposed(t, ctx, database, composedTgt2)
		consumer := newComposedConsumer(t, database, "compose-reference", time.Minute, 0)
		steps := []func() error{
			func() error { return epochPass(ctx, NewIngestionStore(SQLDB{DB: database})) },
			func() error { _, err := consumer.runner.RunOnce(ctx); return err },
		}
		if order == "consumer_first" {
			slices.Reverse(steps)
		}
		for _, step := range steps {
			if err := step(); err != nil {
				t.Fatalf("%s reference: %v", order, err)
			}
		}
		consumer.requireNoFailures(t, ctx)
		assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "completed", 1)
		references[order] = composedState(t, ctx, database)
	}
	if !slices.Equal(references["epoch_first"], references["consumer_first"]) {
		t.Logf("the two sequential orders end in different durable states; either is accepted")
	}

	// Forced interleavings: one side takes repo-tgt's exclusive lock and
	// holds it until the other side is observed waiting on an advisory lock.
	for _, holder := range []string{"epoch", "consumer"} {
		t.Run("forced_"+holder+"_holds_repo_lock", func(t *testing.T) {
			runForcedLockOverlap(t, holder, references)
		})
	}

	iterations := composedIterations(t)
	contended := 0
	for i := 0; i < iterations; i++ {
		t.Run(fmt.Sprintf("iteration_%02d", i), func(t *testing.T) {
			ctx, database := openActivationObligationProofDB(t, "act_compose_epoch")
			setupComposed(t, ctx, database, composedTgt2)
			consumer := newComposedConsumer(t, database, "compose-consumer", time.Minute, 0)
			epochStore, epochLocks := withLockWait(NewIngestionStore(SQLDB{DB: database}))
			errs := releaseTogether(
				func() error { return epochPass(ctx, epochStore) },
				func() error { _, err := consumer.runner.RunOnce(ctx); return err },
			)
			for n, err := range errs {
				if err != nil {
					t.Fatalf("racer %d: %v (sqlstate %q)", n, err, sqlState(err))
				}
			}
			consumer.requireNoFailures(t, ctx)
			assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "completed", 1)
			if got := consumer.port.total(); got > 1 {
				t.Fatalf("maintenance callbacks = %d, want at most 1", got)
			}
			requireWoken(t, ctx, database, "tgt-2", true)
			got := composedState(t, ctx, database)
			if !slices.Equal(got, references["epoch_first"]) && !slices.Equal(got, references["consumer_first"]) {
				requireComposedState(t, "overlap vs the epoch-first order", got, references["epoch_first"])
			}
			if epochLocks.waited.Load()+consumer.locks.waited.Load() > 0 {
				contended++
			}
			t.Logf("callbacks=%d epoch_locks=%d/%d waited consumer_locks=%d/%d waited",
				consumer.port.total(), epochLocks.waited.Load(), epochLocks.taken.Load(),
				consumer.locks.waited.Load(), consumer.locks.taken.Load())
		})
	}
	t.Logf("OVERLAP %d iterations; %d had at least one exclusive maintenance lock acquisition that waited over 5 ms",
		iterations, contended)
}

// runForcedLockOverlap makes holder take repo-tgt's exclusive maintenance
// lock first and keep it until the other side waits on an advisory lock,
// then releases it; both must finish with the same durable truth.
func runForcedLockOverlap(t *testing.T, holder string, references map[string][]string) {
	t.Helper()
	ctx, database := openActivationObligationProofDB(t, "act_compose_forced")
	setupComposed(t, ctx, database, composedTgt2)
	consumer := newComposedConsumer(t, database, "compose-forced", time.Minute, 0)
	epochStore, epochLocks := withLockWait(NewIngestionStore(SQLDB{DB: database}))
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hold := func(ctx context.Context, repoKey string) {
		if repoKey != "repo-tgt" {
			return
		}
		once.Do(func() {
			close(held)
			select {
			case <-release:
			case <-ctx.Done():
			}
		})
	}
	epoch := func() error { return epochPass(ctx, epochStore) }
	consume := func() error { _, err := consumer.runner.RunOnce(ctx); return err }
	first, second := epoch, consume
	if holder == "epoch" {
		epochLocks.afterLock = hold
	} else {
		consumer.locks.afterLock = hold
		first, second = consume, epoch
	}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- first() }()
	select {
	case <-held:
	case err := <-firstDone:
		t.Fatalf("%s finished without taking repo-tgt's lock: %v", holder, err)
	}
	go func() { secondDone <- second() }()
	awaitLockWaiter(t, ctx, database, "%pg_advisory_xact_lock%")
	close(release)
	for _, done := range []chan error{firstDone, secondDone} {
		if err := <-done; err != nil {
			t.Fatalf("forced overlap: %v (sqlstate %q)", err, sqlState(err))
		}
	}
	consumer.requireNoFailures(t, ctx)
	assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "completed", 1)
	requireWoken(t, ctx, database, "tgt-2", true)
	got := composedState(t, ctx, database)
	if !slices.Equal(got, references["epoch_first"]) && !slices.Equal(got, references["consumer_first"]) {
		requireComposedState(t, "forced overlap vs the epoch-first order", got, references["epoch_first"])
	}
	t.Logf("FORCED holder=%s callbacks=%d epoch_locks_waited=%d consumer_locks_waited=%d",
		holder, consumer.port.total(), epochLocks.waited.Load(), consumer.locks.waited.Load())
}

// TestActivationObligationReplicasLive (#7584 D3 step 4, item 2): two
// consumer replicas released together. On one obligation, SKIP LOCKED gives
// it to exactly one replica, which maintains it once. On two distinct
// obligations (one cycle each), both progress: each replica settles a
// different one with one callback. The durable state equals one replica
// settling everything alone.
func TestActivationObligationReplicasLive(t *testing.T) {
	for _, tc := range []struct {
		name        string
		gens        []composedGen
		maxPerCycle int
	}{
		{"same_obligation", []composedGen{composedTgt2}, 0},
		{"distinct_obligations", []composedGen{composedTgt2, composedSolo2}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, database := openActivationObligationProofDB(t, "act_replica_ref")
			setupComposed(t, ctx, database, tc.gens...)
			alone := newComposedConsumer(t, database, "replica-alone", time.Minute, 0)
			if _, err := alone.runner.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			alone.requireNoFailures(t, ctx)
			reference := composedState(t, ctx, database)
			for i := 0; i < 5; i++ {
				t.Run(fmt.Sprintf("iteration_%d", i), func(t *testing.T) {
					runReplicaRace(t, tc.gens, tc.maxPerCycle, reference)
				})
			}
		})
	}
}

func runReplicaRace(t *testing.T, gens []composedGen, maxPerCycle int, reference []string) {
	t.Helper()
	ctx, database := openActivationObligationProofDB(t, "act_replica")
	setupComposed(t, ctx, database, gens...)
	a := newComposedConsumer(t, database, "replica-a", time.Minute, maxPerCycle)
	b := newComposedConsumer(t, database, "replica-b", time.Minute, maxPerCycle)
	var claimedA, claimedB int
	errs := releaseTogether(
		func() (err error) { claimedA, err = a.runner.RunOnce(ctx); return err },
		func() (err error) { claimedB, err = b.runner.RunOnce(ctx); return err },
	)
	for n, err := range errs {
		if err != nil {
			t.Fatalf("replica %d: %v (sqlstate %q)", n, err, sqlState(err))
		}
	}
	a.requireNoFailures(t, ctx)
	b.requireNoFailures(t, ctx)
	if claimedA+claimedB != len(gens) {
		t.Fatalf("claims a=%d b=%d, want %d in all (each obligation claimed once)", claimedA, claimedB, len(gens))
	}
	for _, g := range gens {
		assertObligationStateToken(t, ctx, database, g.scope, g.gen, "completed", 1)
		if got := a.port.callsFor(g.gen) + b.port.callsFor(g.gen); got != 1 {
			t.Fatalf("callbacks for %s = %d, want exactly 1", g.gen, got)
		}
		requireWoken(t, ctx, database, g.gen, true)
	}
	if len(gens) > 1 && (claimedA != 1 || claimedB != 1 || a.port.total() != 1 || b.port.total() != 1) {
		t.Fatalf("distinct obligations: claims a=%d b=%d callbacks a=%d b=%d, want one each",
			claimedA, claimedB, a.port.total(), b.port.total())
	}
	requireComposedState(t, "replica race vs one replica alone", composedState(t, ctx, database), reference)
}
