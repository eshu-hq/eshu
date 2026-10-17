// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8167 first-generation retract predicate proof: NewPriorGenerationCheck answers from strictly older generations in activation order (ingested_at, generation_id), so a newer generation committed before a first attempt runs does not flip the retract decision; covers newer-only, older-only, both, none, failed and superseded predecessors, ingested_at ties, a missing intent row, and repeat calls; untagged and gated by the ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN env Skip; Postgres only; under 0.5s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// priorCheckProofDB opens the disposable proof database with a fresh schema
// that holds the shipped ingestion_scopes and scope_generations DDL and one
// scope, scope-pg.
func priorCheckProofDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	database := openLivenessProofDB(t, dsn)
	ctx := context.Background()
	schemaName := fmt.Sprintf("prior_check_proof_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	for _, statement := range []string{
		"SET search_path TO " + schemaName,
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("scope_generations"),
		`INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ('scope-pg', 'repository', 'github', 'proof/pg', 'git',
          'proof/pg', now(), now(), 'active')`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("provision prior check proof schema: %v", err)
		}
	}
	return database
}

// insertProofGeneration adds one scope-pg generation ingested minuteOffset
// minutes after a fixed base time. observed_at runs in the opposite direction
// so the proof fails if the predicate orders by observation time instead of
// activation order.
func insertProofGeneration(t *testing.T, database *sql.DB, generationID string, minuteOffset int, status string) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, 'scope-pg', 'push',
          timestamptz '2026-01-01 00:00:00+00' - make_interval(mins => $2::int),
          timestamptz '2026-01-01 00:00:00+00' + make_interval(mins => $2::int),
          $3)`, generationID, minuteOffset, status); err != nil {
		t.Fatalf("insert generation %s: %v", generationID, err)
	}
}

// firstAttempt is a first-attempt intent for scope-pg.
func firstAttempt(generationID string) reducercontract.Intent {
	return reducercontract.Intent{ScopeID: "scope-pg", GenerationID: generationID, AttemptCount: 1}
}

// TestPriorGenerationCheckIgnoresNewerGenerationLive is the #8167
// regression: a first attempt for the scope's first generation must make the
// same retract decision whether a newer generation committed before it ran or
// after. Before the fix the check counted any other generation, so the
// decision flipped from skip to retract on queue timing.
func TestPriorGenerationCheckIgnoresNewerGenerationLive(t *testing.T) {
	database := priorCheckProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	check := NewPriorGenerationCheck(SQLDB{DB: database})

	insertProofGeneration(t, database, "gen-first", 0, "active")
	before, err := reducercontract.DecideFirstGenerationRetract(ctx, check, firstAttempt("gen-first"))
	if err != nil {
		t.Fatalf("decide before newer commit: %v", err)
	}
	insertProofGeneration(t, database, "gen-newer", 5, "pending")
	after, err := reducercontract.DecideFirstGenerationRetract(ctx, check, firstAttempt("gen-first"))
	if err != nil {
		t.Fatalf("decide after newer commit: %v", err)
	}
	if before != after {
		t.Fatalf("retract decision depends on queue timing: before newer commit %+v, after %+v", before, after)
	}
	if !after.Skip || after.Reason != reducercontract.RetractReasonFirstGeneration {
		t.Fatalf("first generation decision = %+v, want skip with reason %q", after, reducercontract.RetractReasonFirstGeneration)
	}

	newer, err := reducercontract.DecideFirstGenerationRetract(ctx, check, firstAttempt("gen-newer"))
	if err != nil {
		t.Fatalf("decide for newer generation: %v", err)
	}
	if newer.Skip || newer.Reason != reducercontract.RetractReasonPriorGeneration {
		t.Fatalf("newer generation decision = %+v, want retract with reason %q", newer, reducercontract.RetractReasonPriorGeneration)
	}
}

// TestPriorGenerationCheckStrictlyOlderCasesLive covers the predicate's edge
// cases on the shipped schema. Each case calls the check twice to show a
// same-generation re-run answers the same way.
func TestPriorGenerationCheckStrictlyOlderCasesLive(t *testing.T) {
	type seed struct {
		id     string
		offset int
		status string
	}
	tests := []struct {
		name   string
		seeds  []seed
		intent string
		want   bool
	}{
		{name: "none", seeds: []seed{{"gen-a", 0, "active"}}, intent: "gen-a", want: false},
		{name: "newer only", seeds: []seed{{"gen-a", 0, "active"}, {"gen-b", 5, "pending"}}, intent: "gen-a", want: false},
		{name: "newer failed only", seeds: []seed{{"gen-a", 0, "active"}, {"gen-b", 5, "failed"}}, intent: "gen-a", want: false},
		{name: "older only", seeds: []seed{{"gen-a", 0, "superseded"}, {"gen-b", 5, "active"}}, intent: "gen-b", want: true},
		{name: "older and newer", seeds: []seed{{"gen-a", 0, "superseded"}, {"gen-b", 5, "active"}, {"gen-c", 10, "pending"}}, intent: "gen-b", want: true},
		{name: "failed older", seeds: []seed{{"gen-a", 0, "failed"}, {"gen-b", 5, "active"}}, intent: "gen-b", want: true},
		{name: "ingested_at tie lower id is older", seeds: []seed{{"gen-a", 0, "superseded"}, {"gen-b", 0, "active"}}, intent: "gen-b", want: true},
		{name: "ingested_at tie higher id is newer", seeds: []seed{{"gen-a", 0, "active"}, {"gen-b", 0, "pending"}}, intent: "gen-a", want: false},
		{name: "missing intent row falls back to any other generation", seeds: []seed{{"gen-a", 0, "active"}}, intent: "gen-pruned", want: true},
		{name: "missing intent row on empty scope", seeds: nil, intent: "gen-pruned", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := priorCheckProofDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for _, s := range tt.seeds {
				insertProofGeneration(t, database, s.id, s.offset, s.status)
			}
			check := NewPriorGenerationCheck(SQLDB{DB: database})
			for call := 1; call <= 2; call++ {
				got, err := check(ctx, "scope-pg", tt.intent)
				if err != nil {
					t.Fatalf("call %d: check error = %v", call, err)
				}
				if got != tt.want {
					t.Fatalf("call %d: prior = %v, want %v", call, got, tt.want)
				}
			}
		})
	}
}
