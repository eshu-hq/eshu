// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestProjectorQueueClaimOrdersBySourceFairness(t *testing.T) {
	t.Parallel()

	query := claimProjectorWorkQuery
	for _, want := range []string{
		"projector_source_inflight_count",
		"projector_source_fair_rank",
		"candidate_scope.source_system",
		"ORDER BY",
		"projector_source_inflight_count ASC",
		"projector_source_fair_rank ASC",
		"updated_at ASC",
		"work_item_id ASC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("projector claim query missing source-fairness fragment %q:\n%s", want, query)
		}
	}
}

func TestProjectorQueueClaimDoesNotFillWorkersFromOneNoisySource(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the projector source fairness proof")
	}

	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)

	now := time.Date(2026, time.June, 28, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		scopeID := fmt.Sprintf("git-scope-%02d", i)
		generationID := fmt.Sprintf("git-gen-%02d", i)
		seedProjectorSourceFairnessScope(t, ctx, db, scopeID, generationID, "git", now)
		insertProjectorSourceFairnessWork(t, ctx, db, scopeID, generationID, now.Add(time.Duration(i)*time.Second))
	}

	seedProjectorSourceFairnessScope(t, ctx, db, "aws-scope-00", "aws-gen-00", "aws", now)
	insertProjectorSourceFairnessWork(t, ctx, db, "aws-scope-00", "aws-gen-00", now.Add(time.Hour))

	queue := ProjectorQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "projector-source-fairness",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(2 * time.Hour) },
	}

	first, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("first Claim() ok = false, want true")
	}
	if got, want := first.Scope.SourceSystem, "git"; got != want {
		t.Fatalf("first claim source = %q, want oldest noisy source %q", got, want)
	}

	second, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("second Claim() error = %v", err)
	}
	if !ok {
		t.Fatal("second Claim() ok = false, want true")
	}
	if got, want := second.Scope.SourceSystem, "aws"; got != want {
		t.Fatalf("second claim source = %q, want quiet source %q while noisy source is already in flight", got, want)
	}
}

func TestReducerQueueBatchClaimOrdersBySourceFairnessWithinDomain(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery
	for _, want := range []string{
		"reducer_source_fair_rank",
		"reducer_source_system",
		"source_inflight_count",
		"payload->>'source_system'",
		"ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, updated_at ASC, work_item_id ASC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch reducer claim query missing source-fairness fragment %q:\n%s", want, query)
		}
	}
}

func TestReducerQueueBatchDoesNotStarveNewerSourceWithinDomain(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the reducer source fairness proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		domain       = reducer.DomainSupplyChainImpact
		noisyCount   = 16
		quietCount   = 4
		batchSize    = 8
		scopeID      = "scope-source-fair"
		generationID = "gen-fair"
	)

	now := time.Date(2026, time.June, 28, 11, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, scopeID, now)

	for i := 0; i < noisyCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("git-noisy-%03d", i),
			scopeID:        scopeID,
			generationID:   generationID,
			domain:         string(domain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("git-noisy-key-%03d", i),
			sourceSystem:   "git",
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	quietBase := now.Add(time.Hour)
	for i := 0; i < quietCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("aws-quiet-%03d", i),
			scopeID:        scopeID,
			generationID:   generationID,
			domain:         string(domain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("aws-quiet-key-%03d", i),
			sourceSystem:   "aws",
			updatedAt:      quietBase.Add(time.Duration(i) * time.Second),
		})
	}

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "reducer-source-fairness",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return quietBase.Add(2 * time.Hour) },
		ClaimDomains:  []reducer.Domain{domain},
	}

	intents, err := queue.ClaimBatch(ctx, batchSize)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	var quietClaimed int
	for _, intent := range intents {
		if intent.SourceSystem == "aws" {
			quietClaimed++
		}
	}
	if quietClaimed == 0 {
		t.Fatalf("ClaimBatch() claimed %d items, none for quiet source; older noisy source monopolized domain %q", len(intents), domain)
	}
}

func seedProjectorSourceFairnessScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	sourceSystem string,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'repository', $2, $1, NULL, $2, $1, $3, $3, 'pending', NULL, '{}'::jsonb)`,
		scopeID, sourceSystem, now); err != nil {
		t.Fatalf("insert projector fairness scope %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', $1, $3, $3, 'pending', NULL, NULL, '{}'::jsonb)`,
		generationID, scopeID, now); err != nil {
		t.Fatalf("insert projector fairness generation %q: %v", generationID, err)
	}
}

func insertProjectorSourceFairnessWork(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	updatedAt time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, payload, created_at, updated_at, visible_at
) VALUES ($1, $2, $3, 'projector', 'source_local', 'pending',
          0, '{}'::jsonb, $4, $4, $4)`,
		projectorWorkItemID(scopeID, generationID), scopeID, generationID, updatedAt); err != nil {
		t.Fatalf("insert projector fairness work %q/%q: %v", scopeID, generationID, err)
	}
}
