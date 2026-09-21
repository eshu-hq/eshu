// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestPublishedGenerationRecommitIsIdempotentLive proves that a retry of an
// already-published generation does not rewrite its chronology or facts.
func TestPublishedGenerationRecommitIsIdempotentLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	database := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, database, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-recommit', 'repository', 'git', 'proof/recommit', 'git',
          'proof/recommit', now() - interval '2 minutes',
          now() - interval '2 minutes', 'active', 'gen-old');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint,
    observed_at, ingested_at, status, activated_at
) VALUES
    ('gen-old', 'scope-recommit', 'snapshot', 'old-hint',
     now() - interval '2 minutes', now() - interval '2 minutes',
     'active', now() - interval '2 minutes'),
    ('gen-new', 'scope-recommit', 'snapshot', 'new-hint',
     now() - interval '1 minute', now() - interval '1 minute',
     'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-recommit_gen-old', 'scope-recommit', 'gen-old',
          'projector', 'source_local', 'running', 1, 'proof-worker',
          now() + interval '2 minutes', now(), '{}'::jsonb, now(), now());
`)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	store := NewIngestionStore(SQLDB{DB: database})
	skip, err := store.shouldSkipUnchangedGeneration(ctx, "scope-recommit", "old-hint")
	if err != nil {
		t.Fatalf("check freshness guard: %v", err)
	}
	if skip {
		t.Fatal("older published generation unexpectedly skipped with newer pending hint")
	}

	now := time.Now().UTC()
	scopeValue := scope.IngestionScope{
		ScopeID: "scope-recommit", ScopeKind: scope.KindRepository,
		SourceSystem: "git", CollectorKind: scope.CollectorGit,
		PartitionKey: "proof/recommit",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "gen-old", ScopeID: "scope-recommit",
		TriggerKind: scope.TriggerKindSnapshot, Status: scope.GenerationStatusPending,
		FreshnessHint: "old-hint", ObservedAt: now, IngestedAt: now,
	}
	if err := store.CommitScopeGeneration(ctx, scopeValue, generation, nil); err != nil {
		t.Fatalf("re-commit published generation: %v", err)
	}

	assertPublished := func(stage string) {
		t.Helper()
		var status string
		var activatedAt sql.NullTime
		var ingestedAt time.Time
		var pointer sql.NullString
		if err := database.QueryRowContext(ctx, `
SELECT generation.status, generation.activated_at, generation.ingested_at,
       scope.active_generation_id
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
WHERE generation.generation_id = 'gen-old'`).Scan(&status, &activatedAt, &ingestedAt, &pointer); err != nil {
			t.Fatalf("read %s state: %v", stage, err)
		}
		if status != "active" || !activatedAt.Valid ||
			!ingestedAt.Before(now.Add(-time.Minute)) ||
			!pointer.Valid || pointer.String != "gen-old" {
			t.Fatalf("%s: generation=%q activated=%v ingested=%v pointer=%v; want original published state", stage, status, activatedAt, ingestedAt, pointer)
		}
	}
	assertPublished("after re-commit")

	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	oldWork := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-recommit"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-old"},
		AttemptCount: 1,
	}
	if err := queue.Heartbeat(ctx, oldWork); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("older work heartbeat = %v; want superseded", err)
	}
	assertPublished("after superseding heartbeat")
}

// TestFinalizedGenerationRecommitSkipsFactsLive checks the claim-retry shape
// with no freshness hint for every state that must never be reopened.
func TestFinalizedGenerationRecommitSkipsFactsLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	for _, status := range []string{"active", "completed", "superseded", "failed"} {
		t.Run(status, func(t *testing.T) {
			database := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, database, "")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			scopeStatus := "failed"
			var pointer any
			if status == "active" {
				scopeStatus = "active"
				pointer = "gen-finalized"
			}
			if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-finalized', 'repository', 'git', 'proof/finalized', 'git',
          'proof/finalized', now() - interval '2 minutes',
          now() - interval '2 minutes', $1, $2)`, scopeStatus, pointer); err != nil {
				t.Fatalf("seed finalized scope: %v", err)
			}
			if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-finalized', 'scope-finalized', 'snapshot',
          now() - interval '2 minutes', now() - interval '2 minutes', $1,
          CASE WHEN $1 = 'active' THEN now() - interval '2 minutes' ELSE NULL END)`, status); err != nil {
				t.Fatalf("seed finalized generation: %v", err)
			}

			now := time.Now().UTC()
			stream := make(chan facts.Envelope, 1)
			stream <- facts.Envelope{FactID: "replay-fact"}
			close(stream)
			store := NewIngestionStore(SQLDB{DB: database})
			scopeValue := scope.IngestionScope{
				ScopeID: "scope-finalized", ScopeKind: scope.KindRepository,
				SourceSystem: "git", CollectorKind: scope.CollectorGit,
				PartitionKey: "proof/finalized",
			}
			generation := scope.ScopeGeneration{
				GenerationID: "gen-finalized", ScopeID: "scope-finalized",
				TriggerKind: scope.TriggerKindSnapshot,
				Status:      scope.GenerationStatusPending,
				ObservedAt:  now, IngestedAt: now,
			}
			if err := store.CommitScopeGeneration(ctx, scopeValue, generation, stream); err != nil {
				t.Fatalf("skip finalized re-commit: %v", err)
			}
			if got := len(stream); got != 0 {
				t.Fatalf("undrained fact stream has %d rows", got)
			}

			var gotStatus, gotScopeStatus string
			var gotIngestedAt time.Time
			var gotPointer sql.NullString
			if err := database.QueryRowContext(ctx, `
SELECT generation.status, generation.ingested_at, scope.status,
       scope.active_generation_id
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
WHERE generation.generation_id = 'gen-finalized'`).Scan(
				&gotStatus, &gotIngestedAt, &gotScopeStatus, &gotPointer,
			); err != nil {
				t.Fatalf("read finalized state: %v", err)
			}
			if gotStatus != status || gotScopeStatus != scopeStatus ||
				!gotIngestedAt.Before(now.Add(-time.Minute)) ||
				gotPointer.Valid != (pointer != nil) {
				t.Fatalf("status=%q scope=%q ingested=%v pointer=%v; want original finalized state",
					gotStatus, gotScopeStatus, gotIngestedAt, gotPointer)
			}
		})
	}
}
