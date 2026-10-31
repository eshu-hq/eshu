// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// claimDeadlockLoad describes one concurrent projector claim load run.
type claimDeadlockLoad struct {
	scopes   int
	workers  int
	duration time.Duration
}

// claimDeadlockOutcome counts what the concurrent claim load observed.
type claimDeadlockOutcome struct {
	claims       int64
	deadlocks    int64
	otherErrors  int64
	firstOther   atomic.Value
	doubleClaims int64 // distinct overlapping lease pairs
	superseded   int64 // Heartbeat or Ack refusals on superseded generations (#7130)
	retiredRows  int64 // refinalize rows seeded on already-superseded generations
}

// TestProjectorClaimConcurrentLoadHasNoDeadlock drives many concurrent
// projector claimers against a backlog that keeps producing supersedable
// generations and expired leases, so every maintenance branch of the claim
// statement runs under contention (#7108). Deadlocks are counted from
// pg_stat_database because Claim retries a 40P01 internally; any deadlock the
// server detected fails the test, as does any unexpected claim error.
//
// A lease-uniqueness auditor also fails the test on any two overlapping
// projector leases in one scope. Two claimers whose snapshots disagree on the
// scope's oldest ready row used to lock different rows and both claim (#7115);
// the scope claim fence now excludes that, so any overlap is a regression.
//
// The load also drives the #7130 fences: the producer drops refinalize rows on
// generations a newer Ack already superseded, which the claim must sweep
// instead of claiming, and a worker whose lease expired heartbeats before it
// acks, so the heartbeat's superseded-generation refusal runs under contention.
// ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_SCOPES widens the scope count, for
// example to a 10k-scope backlog.
func TestProjectorClaimConcurrentLoadHasNoDeadlock(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN to a disposable Postgres database")
	}
	load := claimDeadlockLoad{scopes: 40, workers: 16, duration: 20 * time.Second}
	if raw := os.Getenv("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("parse proof seconds: %v", err)
		}
		load.duration = time.Duration(seconds) * time.Second
	}
	if raw := os.Getenv("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_WORKERS"); raw != "" {
		workers, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("parse proof workers: %v", err)
		}
		load.workers = workers
	}
	if raw := os.Getenv("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_SCOPES"); raw != "" {
		scopes, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("parse proof scopes: %v", err)
		}
		load.scopes = scopes
	}
	database := openClaimDeadlockProofDB(t, dsn, load.workers+4)
	before := serverDeadlockCount(t, database)
	outcome := runClaimDeadlockLoad(t, database, load)
	time.Sleep(1500 * time.Millisecond) // let backends flush pg_stat counters
	serverDeadlocks := serverDeadlockCount(t, database) - before
	t.Logf("workers=%d seconds=%.0f claims=%d server_deadlocks=%d claim_deadlock_errors=%d other_errors=%d overlapping_leases=%d superseded_refusals=%d retired_rows_seeded=%d retired_rows_claimed=%d",
		load.workers, load.duration.Seconds(), outcome.claims, serverDeadlocks, outcome.deadlocks,
		outcome.otherErrors, outcome.doubleClaims, outcome.superseded, outcome.retiredRows,
		retiredRowsClaimed(t, database))
	if first := outcome.firstOther.Load(); first != nil {
		t.Logf("first other error: %v", first)
	}
	if serverDeadlocks != 0 || outcome.deadlocks != 0 {
		t.Fatalf("concurrent projector claims deadlocked: server=%d claim_errors=%d", serverDeadlocks, outcome.deadlocks)
	}
	if outcome.otherErrors != 0 {
		t.Fatalf("concurrent claim load hit %d unexpected errors", outcome.otherErrors)
	}
	if outcome.claims == 0 {
		t.Fatal("concurrent claim load claimed nothing; the proof is vacuous")
	}
	if outcome.doubleClaims != 0 {
		t.Fatalf("concurrent claims granted %d overlapping lease pairs in one scope (#7115)", outcome.doubleClaims)
	}
	if claimed := retiredRowsClaimed(t, database); claimed != 0 {
		t.Fatalf("%d refinalize rows on superseded generations were claimed (#7130)", claimed)
	}
}

// retiredRowsClaimed counts refinalize rows the load seeded on superseded
// generations that a claim ever took: attempt_count only moves on a claim.
func retiredRowsClaimed(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	var claimed int64
	if err := database.QueryRow(`
SELECT count(*) FROM fact_work_items
WHERE work_item_id LIKE 'refinalize\_%' AND attempt_count > 0`).Scan(&claimed); err != nil {
		t.Fatalf("count claimed retired rows: %v", err)
	}
	return claimed
}

// serverDeadlockCount reads the deadlocks Postgres detected in this database.
func serverDeadlockCount(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	if _, err := database.Exec("SELECT pg_stat_clear_snapshot()"); err != nil {
		t.Fatalf("clear stats snapshot: %v", err)
	}
	var deadlocks int64
	if err := database.QueryRow(
		"SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()",
	).Scan(&deadlocks); err != nil {
		t.Fatalf("read pg_stat_database.deadlocks: %v", err)
	}
	return deadlocks
}

// withDSNParam appends one query parameter to a URL-form DSN.
func withDSNParam(dsn, param string) string {
	if strings.Contains(dsn, "?") {
		return dsn + "&" + param
	}
	return dsn + "?" + param
}

// openClaimDeadlockProofDB bootstraps a fresh disposable database on the
// public schema and returns a pool on it. The database is dropped when the
// test ends.
//
// Database-level isolation (not an isolated schema): the lane's 241a/241b
// authority migrations are public-only, so ApplyBootstrapWithOptions skips
// them on an isolated schema and the later 242a definition then fails with
// a missing eshu_security schema. A fresh database keeps the full install
// and stays private to the proof. It follows the Lane F
// disposable-public pattern (openDisposablePublicBootstrapDB).
func openClaimDeadlockProofDB(t *testing.T, dsn string, conns int) *sql.DB {
	t.Helper()
	pool := openDisposablePublicBootstrapDB(t, claimDeadlockProofAdminDSN(t, dsn), claimDeadlockProofOptIn(t, dsn),
		"set ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN and ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DISPOSABLE=1 "+
			"(or the ESHU_POSTGRES_TEST_DSN/_DISPOSABLE pair) to run the projector claim proofs",
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: database})
		})
	pool.SetMaxOpenConns(conns)
	pool.SetMaxIdleConns(conns)
	return pool
}

// claimDeadlockProofAdminDSN resolves the administrative DSN openClaimDeadlockProofDB
// creates its disposable database through: the passed family DSN retargeted
// at the postgres maintenance database (scheduled runners name a fresh
// per-group database the helper must not install into).
func claimDeadlockProofAdminDSN(t *testing.T, dsn string) string {
	t.Helper()
	admin, err := postgresproof.WithDatabase(dsn, "postgres")
	if err != nil {
		t.Fatalf("retarget claim deadlock proof DSN: %v", err)
	}
	return admin
}

// claimDeadlockProofOptIn routes the disposable opt-in for the DSN the
// caller resolved: the claim-deadlock family's own opt-in (fail-closed when
// the family DSN is set without it), the supersession family's for the
// write-marker replay proof, the fairness family's for the isolated-schema
// derivation guard's reference database, else the generic fallback the
// local pair sets. An opt-in other than "1" skips in
// openDisposablePublicBootstrapDB: a proof that cannot isolate itself must
// not run half-isolated.
func claimDeadlockProofOptIn(t *testing.T, dsn string) string {
	t.Helper()
	if family := strings.TrimSpace(os.Getenv("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN")); family != "" && dsn == family {
		if os.Getenv("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN is set without ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DISPOSABLE=1")
		}
		return "1"
	}
	for _, family := range [][2]string{
		{"ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN", "ESHU_PROJECTOR_SUPERSESSION_PROOF_DISPOSABLE"},
		{"ESHU_REDUCER_FAIRNESS_PROOF_DSN", "ESHU_REDUCER_FAIRNESS_PROOF_DISPOSABLE"},
	} {
		if value := strings.TrimSpace(os.Getenv(family[0])); value != "" && dsn == value {
			return os.Getenv(family[1])
		}
	}
	return os.Getenv("ESHU_POSTGRES_DISPOSABLE")
}

// proofTargetDSN rewrites adminDSN to the database control is connected to
// (learned from current_database()), so race and lock-order proofs open
// genuinely independent sessions against the same disposable-database
// fixture instead of the shared administrative database. Host, credentials,
// and TLS carry over; only the database changes.
func proofTargetDSN(ctx context.Context, t *testing.T, control *sql.DB, adminDSN string) string {
	t.Helper()
	var database string
	if err := control.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		t.Fatalf("read proof database: %v", err)
	}
	target, err := postgresproof.WithDatabase(adminDSN, database)
	if err != nil {
		t.Fatalf("retarget proof DSN at %q: %v", database, err)
	}
	return target
}

// runClaimDeadlockLoad seeds scopes, then runs a generation producer, claim
// workers, and a lease-uniqueness auditor until the load duration elapses.
func runClaimDeadlockLoad(t *testing.T, database *sql.DB, load claimDeadlockLoad) *claimDeadlockOutcome {
	t.Helper()
	base := time.Now().UTC()
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
)
SELECT id, 'repository', 'git', id, 'git', id, $2, $2, 'active'
FROM (SELECT format('scope-%s', CASE WHEN i < 1000 THEN lpad(i::text, 3, '0') ELSE i::text END) AS id FROM generate_series(0, $1 - 1) AS i) AS scopes
`, load.scopes, base); err != nil {
		t.Fatalf("insert scopes: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), load.duration)
	defer cancel()
	outcome := &claimDeadlockOutcome{}
	var overlaps sync.Map // distinct overlapping lease pairs
	var generation atomic.Int64
	enqueue := func(scopeID string) error {
		gen := fmt.Sprintf("gen-%09d", generation.Add(1))
		now := time.Now().UTC()
		if _, err := database.ExecContext(context.Background(), `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, $2, 'push', $3, $3, 'pending')
`, gen, scopeID, now); err != nil {
			return err
		}
		queue := NewProjectorQueue(SQLDB{DB: database}, "producer", time.Second)
		return queue.Enqueue(context.Background(), scopeID, gen)
	}
	record := func(err error) {
		var pgErr *pgconn.PgError
		switch {
		case errors.As(err, &pgErr) && pgErr.Code == "40P01":
			atomic.AddInt64(&outcome.deadlocks, 1)
		case ctx.Err() != nil:
		case errors.Is(err, ErrProjectorClaimRejected):
		case errors.Is(err, failure.ErrWorkAckDeferred):
			// Ack's scope lock timeout is a documented deferral the service retries.
		case errors.Is(err, failure.ErrWorkSuperseded):
			atomic.AddInt64(&outcome.superseded, 1)
		default:
			if atomic.AddInt64(&outcome.otherErrors, 1) == 1 {
				outcome.firstOther.Store(err)
			}
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			// Bursts of generations for a few scopes create supersedable rows.
			scopeID := fmt.Sprintf("scope-%03d", rand.IntN(load.scopes))
			for j := 0; j < 1+rand.IntN(3); j++ {
				if err := enqueue(scopeID); err != nil {
					record(err)
				}
			}
			// A refinalize row left on a generation a newer Ack superseded
			// (#7130). It is seeded unclaimed; the claim must sweep it.
			if rand.IntN(4) == 0 {
				result, err := database.ExecContext(context.Background(), `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, visible_at, payload, created_at, updated_at
)
SELECT 'refinalize_' || generation_id, scope_id, generation_id, 'projector',
       'source_local', 'pending', 0, now(), '{}'::jsonb, now(), now()
FROM scope_generations
WHERE scope_id = $1 AND status = 'superseded'
ORDER BY ingested_at DESC
LIMIT 1
ON CONFLICT (work_item_id) DO NOTHING`, scopeID)
				if err != nil {
					record(err)
				} else if n, _ := result.RowsAffected(); n > 0 {
					atomic.AddInt64(&outcome.retiredRows, n)
				}
			}
			time.Sleep(time.Duration(rand.IntN(3)) * time.Millisecond)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			rows, err := database.QueryContext(ctx, `
SELECT a.work_item_id || '#' || a.attempt_count || '|' || b.work_item_id || '#' || b.attempt_count
FROM fact_work_items AS a
JOIN fact_work_items AS b
  ON b.scope_id = a.scope_id
 AND b.work_item_id > a.work_item_id
WHERE a.stage = 'projector' AND b.stage = 'projector'
  AND a.status IN ('claimed', 'running')
  AND b.status IN ('claimed', 'running')
  -- Both leases were stamped from the workers' clock, so an overlap of
  -- [last_attempt_at, claim_until) is a real double claim.
  AND a.last_attempt_at < b.claim_until
  AND b.last_attempt_at < a.claim_until`)
			if err == nil {
				for rows.Next() {
					var pair string
					if rows.Scan(&pair) == nil {
						if _, seen := overlaps.LoadOrStore(pair, true); !seen {
							atomic.AddInt64(&outcome.doubleClaims, 1)
							t.Logf("overlapping leases %s", pair)
						}
					}
				}
				_ = rows.Close()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	for w := 0; w < load.workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			// Short leases plus occasional stalls produce expired claims for
			// the duplicate and sibling reclaim branches.
			queue := NewProjectorQueue(SQLDB{DB: database}, fmt.Sprintf("worker-%d", worker), 150*time.Millisecond)
			queue.RetryDelay = time.Millisecond
			for ctx.Err() == nil {
				work, ok, err := queue.Claim(ctx)
				if err != nil {
					record(err)
					continue
				}
				if !ok {
					time.Sleep(2 * time.Millisecond)
					continue
				}
				atomic.AddInt64(&outcome.claims, 1)
				switch rand.IntN(10) {
				case 0:
					// The lease expires; the stale worker then heartbeats and,
					// if the heartbeat still holds, acks like the service.
					time.Sleep(200 * time.Millisecond)
					if err := queue.Heartbeat(ctx, work); err != nil {
						record(err)
						continue
					}
					if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
						record(err)
					}
				case 1:
					if err := queue.Fail(ctx, work, &retryableTestError{message: "proof retry"}); err != nil {
						record(err)
					}
				default:
					time.Sleep(time.Duration(rand.IntN(5)) * time.Millisecond)
					if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
						record(err)
					}
				}
			}
		}(w)
	}
	wg.Wait()
	return outcome
}
