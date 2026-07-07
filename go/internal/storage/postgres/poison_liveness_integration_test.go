// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPoisonLivenessIntegration exercises CountPoisonDeadLetters and
// RecoverPoisonDeadLetters against a real Postgres instance. Set
// ESHU_POISON_LIVENESS_PROOF_DSN to a Postgres DSN to run the suite; it is
// skipped otherwise, matching the sibling generation-liveness proof.
//
// Scenario 1 (Detection) proves the gap this ticket closes: three poison
// scopes (dead_letter with no newer generation) are NOT detected by the
// existing generation-liveness gauge (CountActiveGenerationsByAge only counts
// ACTIVE generations; a poison scope's newest generation is 'failed', not
// 'active', so it never appears in fresh/aging/stuck at all) — this is the RED
// proof. The new CountPoisonDeadLetters GREEN-detects exactly the 3 poison
// scopes, while excluding:
//   - a healed decoy: dead_letter with a newer ACTIVE generation for the same
//     scope (the scope already recovered on its own).
//   - a terminal burst-drain: a scope whose dead_letter work succeeded on a
//     later attempt for the SAME generation is not modeled here (dead_letter is
//     a per-row terminal state; a distinct decoy scope with a newer generation
//     already covers the "moved on" case a burst-drain would also produce).
func TestPoisonLivenessIntegration(t *testing.T) {
	dsn := os.Getenv("ESHU_POISON_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POISON_LIVENESS_PROOF_DSN to run the poison liveness integration proof")
	}

	now := time.Now().UTC()

	t.Run("DetectionGapAndGauge", func(t *testing.T) {
		db := openPoisonLivenessProofDB(t, dsn)
		provisionPoisonLivenessSchema(t, db, poisonLivenessDetectionSeedSQL)
		ctx := context.Background()

		// RED proof: the existing generation-liveness gauge does not count any
		// of the 3 poison scopes because they have no ACTIVE generation at all
		// (their newest generation status is 'failed', not 'active').
		livenessStore := NewGenerationLivenessStore(SQLDB{DB: db})
		activeCounts, err := livenessStore.CountActiveGenerationsByAge(ctx, GenerationLivenessPolicy{
			ActivationDeadline: 30 * time.Minute,
		}, now)
		if err != nil {
			t.Fatalf("CountActiveGenerationsByAge() error = %v", err)
		}
		if got := activeCounts["stuck"] + activeCounts["aging"] + activeCounts["fresh"]; got != 1 {
			t.Fatalf(
				"existing gauge active-generation total = %d, want 1 (only scope-healed-decoy's newer active generation; "+
					"the 3 poison scopes and the superseded-decoy generation must NOT surface here)",
				got,
			)
		}

		// GREEN proof: the new poison gauge counts exactly the 3 poison scopes
		// and their 3 poison items (one dead_letter row per scope here), and
		// excludes the healed decoy.
		poisonStore := NewPoisonLivenessStore(SQLDB{DB: db})
		counts, err := poisonStore.CountPoisonDeadLetters(ctx, now)
		if err != nil {
			t.Fatalf("CountPoisonDeadLetters() error = %v", err)
		}
		if got, want := counts.PoisonScopes, int64(3); got != want {
			t.Fatalf("PoisonScopes = %d, want %d", got, want)
		}
		if got, want := counts.PoisonItems, int64(3); got != want {
			t.Fatalf("PoisonItems = %d, want %d", got, want)
		}
		// scope-poison-oldest was dead-lettered 3 hours ago; that must be the
		// oldest age reported (within a generous tolerance for test wall time).
		if counts.OldestPoisonAgeSeconds < 3*time.Hour.Seconds()-60 {
			t.Fatalf("OldestPoisonAgeSeconds = %v, want >= ~3h", counts.OldestPoisonAgeSeconds)
		}
	})

	t.Run("BoundedArmIncrementsThenZero", func(t *testing.T) {
		db := openPoisonLivenessProofDB(t, dsn)
		provisionPoisonLivenessSchema(t, db, poisonLivenessBoundedArmSeedSQL)
		store := NewPoisonLivenessStore(SQLDB{DB: db})
		ctx := context.Background()

		policy := PoisonLivenessPolicy{MaxRecoverAttempts: 2, BatchLimit: 100}

		// Sweep 1: poison_recovery_attempts 0 -> 1, row re-enqueued to pending.
		result1, err := store.RecoverPoisonDeadLetters(ctx, policy, now)
		if err != nil {
			t.Fatalf("sweep 1 RecoverPoisonDeadLetters() error = %v", err)
		}
		if got, want := result1.Recovered, 1; got != want {
			t.Fatalf("sweep 1 Recovered = %d, want %d", got, want)
		}
		var status string
		var attempts int
		if err := db.QueryRowContext(ctx, `
			SELECT status, (payload ->> 'poison_recovery_attempts')::int
			FROM fact_work_items WHERE work_item_id = 'wi-poison-bounded'
		`).Scan(&status, &attempts); err != nil {
			t.Fatalf("query after sweep 1: %v", err)
		}
		if status != "pending" {
			t.Fatalf("status after sweep 1 = %q, want pending", status)
		}
		if attempts != 1 {
			t.Fatalf("poison_recovery_attempts after sweep 1 = %d, want 1", attempts)
		}

		// Simulate the item dead-lettering again (as if the re-driven attempt
		// failed once more), so sweep 2 can select it a second time.
		if _, err := db.ExecContext(ctx, `
			UPDATE fact_work_items SET status = 'dead_letter' WHERE work_item_id = 'wi-poison-bounded'
		`); err != nil {
			t.Fatalf("simulate re-dead-letter: %v", err)
		}

		// Sweep 2: poison_recovery_attempts 1 -> 2 (equals ceiling), row
		// re-enqueued once more (still under the < $2 gate at read time).
		result2, err := store.RecoverPoisonDeadLetters(ctx, policy, now)
		if err != nil {
			t.Fatalf("sweep 2 RecoverPoisonDeadLetters() error = %v", err)
		}
		if got, want := result2.Recovered, 1; got != want {
			t.Fatalf("sweep 2 Recovered = %d, want %d", got, want)
		}
		if err := db.QueryRowContext(ctx, `
			SELECT status, (payload ->> 'poison_recovery_attempts')::int
			FROM fact_work_items WHERE work_item_id = 'wi-poison-bounded'
		`).Scan(&status, &attempts); err != nil {
			t.Fatalf("query after sweep 2: %v", err)
		}
		if attempts != 2 {
			t.Fatalf("poison_recovery_attempts after sweep 2 = %d, want 2", attempts)
		}

		// Simulate a third dead-letter. Sweep 3 must be a no-op: the budget
		// ceiling (2) is reached, so the candidate CTE's < $2 gate excludes it.
		if _, err := db.ExecContext(ctx, `
			UPDATE fact_work_items SET status = 'dead_letter' WHERE work_item_id = 'wi-poison-bounded'
		`); err != nil {
			t.Fatalf("simulate second re-dead-letter: %v", err)
		}
		result3, err := store.RecoverPoisonDeadLetters(ctx, policy, now)
		if err != nil {
			t.Fatalf("sweep 3 RecoverPoisonDeadLetters() error = %v", err)
		}
		if got, want := result3.Recovered, 0; got != want {
			t.Fatalf("sweep 3 (at ceiling) Recovered = %d, want %d (UPDATE 0 at budget ceiling)", got, want)
		}
		if err := db.QueryRowContext(ctx, `
			SELECT status, (payload ->> 'poison_recovery_attempts')::int
			FROM fact_work_items WHERE work_item_id = 'wi-poison-bounded'
		`).Scan(&status, &attempts); err != nil {
			t.Fatalf("query after sweep 3: %v", err)
		}
		if status != "dead_letter" {
			t.Fatalf("status after sweep 3 = %q, want dead_letter (left for operator)", status)
		}
		if attempts != 2 {
			t.Fatalf("poison_recovery_attempts after sweep 3 = %d, want 2 (unchanged, LEAST cap)", attempts)
		}
	})

	// GaugeUsesPartialIndex asserts the bounded query's EXPLAIN plan uses the
	// new fact_work_items_dead_letter_poison_idx partial index when it is
	// available to the planner, proving the index is valid and usable for
	// exactly this query shape (status = 'dead_letter' equality plus the
	// scope_id/generation_id anti-join columns the index covers). At this
	// fixture's tiny row count Postgres's cost-based planner correctly prefers
	// a sequential scan regardless of index availability (a real accuracy
	// signal, not a test bug), so this asserts index USABILITY the same way a
	// small-fixture EXPLAIN proof is expected to: temporarily disable
	// sequential scans (enable_seqscan=off, session-local, reset via defer) so
	// the planner is forced to demonstrate the index can serve the query,
	// while still allowing regular index scans to compete with each other on
	// cost.
	t.Run("GaugeUsesPartialIndex", func(t *testing.T) {
		db := openPoisonLivenessProofDB(t, dsn)
		provisionPoisonLivenessSchema(t, db, poisonLivenessDetectionSeedSQL)
		ctx := context.Background()

		if _, err := db.ExecContext(ctx, "ANALYZE fact_work_items"); err != nil {
			t.Fatalf("ANALYZE fact_work_items: %v", err)
		}
		if _, err := db.ExecContext(ctx, "ANALYZE scope_generations"); err != nil {
			t.Fatalf("ANALYZE scope_generations: %v", err)
		}
		if _, err := db.ExecContext(ctx, "SET enable_seqscan = off"); err != nil {
			t.Fatalf("SET enable_seqscan = off: %v", err)
		}
		defer func() { _, _ = db.ExecContext(ctx, "SET enable_seqscan = on") }()

		rows, err := db.QueryContext(ctx, "EXPLAIN "+countPoisonDeadLettersQuery, now)
		if err != nil {
			t.Fatalf("EXPLAIN countPoisonDeadLettersQuery: %v", err)
		}
		defer func() { _ = rows.Close() }()

		var planLines []string
		for rows.Next() {
			var line string
			if scanErr := rows.Scan(&line); scanErr != nil {
				t.Fatalf("scan EXPLAIN line: %v", scanErr)
			}
			planLines = append(planLines, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate EXPLAIN lines: %v", err)
		}

		found := false
		for _, line := range planLines {
			if strings.Contains(line, "fact_work_items_dead_letter_poison_idx") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("EXPLAIN plan (seqscan disabled) does not reference fact_work_items_dead_letter_poison_idx: %v", planLines)
		}
	})
}

// poisonLivenessDetectionSeedSQL seeds 3 poison scopes (dead_letter, no newer
// generation), 1 healed decoy (dead_letter with a newer ACTIVE generation for
// the same scope), matching the RED/GREEN detection proof.
const poisonLivenessDetectionSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ('scope-poison-1', 'repo', 'git', 'k1', 'git', 'p1', now(), now(), 'active', NULL),
    ('scope-poison-2', 'repo', 'git', 'k2', 'git', 'p2', now(), now(), 'active', NULL),
    ('scope-poison-oldest', 'repo', 'git', 'k3', 'git', 'p3', now(), now(), 'active', NULL),
    ('scope-healed-decoy', 'repo', 'git', 'k4', 'git', 'p4', now(), now(), 'active', 'gen-healed-new');

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES
    ('gen-poison-1', 'scope-poison-1', 'push', now() - interval '1 hour', now() - interval '1 hour', 'failed', NULL),
    ('gen-poison-2', 'scope-poison-2', 'push', now() - interval '2 hours', now() - interval '2 hours', 'failed', NULL),
    ('gen-poison-oldest', 'scope-poison-oldest', 'push', now() - interval '3 hours', now() - interval '3 hours', 'failed', NULL),
    ('gen-healed-old', 'scope-healed-decoy', 'push', now() - interval '4 hours', now() - interval '4 hours', 'failed', NULL),
    ('gen-healed-new', 'scope-healed-decoy', 'push', now() - interval '1 hour', now() - interval '1 hour', 'active', now() - interval '1 hour');

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    payload, created_at, updated_at
) VALUES
    ('wi-poison-1', 'scope-poison-1', 'gen-poison-1', 'reducer', 'code_call', 'dead_letter',
     '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour'),
    ('wi-poison-2', 'scope-poison-2', 'gen-poison-2', 'reducer', 'code_call', 'dead_letter',
     '{}'::jsonb, now() - interval '2 hours', now() - interval '2 hours'),
    ('wi-poison-oldest', 'scope-poison-oldest', 'gen-poison-oldest', 'reducer', 'code_call', 'dead_letter',
     '{}'::jsonb, now() - interval '3 hours', now() - interval '3 hours'),
    -- Healed decoy: dead_letter on the OLD generation, but scope-healed-decoy
    -- now has a newer ACTIVE generation (gen-healed-new) — must be excluded.
    ('wi-healed-decoy', 'scope-healed-decoy', 'gen-healed-old', 'reducer', 'code_call', 'dead_letter',
     '{}'::jsonb, now() - interval '4 hours', now() - interval '4 hours');
`

// poisonLivenessBoundedArmSeedSQL seeds one isolated poison scope for the
// bounded-arm attempt-budget proof.
const poisonLivenessBoundedArmSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ('scope-poison-bounded', 'repo', 'git', 'kb', 'git', 'pb', now(), now(), 'active', NULL);

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES
    ('gen-poison-bounded', 'scope-poison-bounded', 'push', now() - interval '1 hour', now() - interval '1 hour', 'failed', NULL);

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    payload, created_at, updated_at
) VALUES
    ('wi-poison-bounded', 'scope-poison-bounded', 'gen-poison-bounded', 'reducer', 'code_call', 'dead_letter',
     '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour');
`
