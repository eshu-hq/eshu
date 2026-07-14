// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerInputInvalidFactStoreLive is the real-Postgres proof for issue
// #4630's durable per-fact quarantine table:
//
//  1. WriteQuarantinedFacts persists a batch of records with every column
//     the read surface needs.
//  2. IDEMPOTENT REPLAY: writing the exact same records a second time (what
//     a retried reducer intent or a re-projected generation does) produces
//     no duplicate rows and no error — the ON CONFLICT (scope_id,
//     generation_id, fact_id, missing_field, domain) DO NOTHING clause
//     actually works against real Postgres, not just a fake in-memory map.
//  3. PER-DOMAIN TRUTH PRESERVED: the exact same fact_id/missing_field
//     quarantined by TWO DIFFERENT reducer domains (for example aws_resource
//     decoded independently by both the AWS resource materialization domain
//     and a relationship/IAM/security-group join-path domain) produces TWO
//     durable rows, not one collapsed row — domain is part of the natural
//     key/ON CONFLICT target, so a domain-filtered read never falsely
//     returns empty for the second domain's quarantine (codex review on PR
//     #5252, issue #4630).
//  4. FK CASCADE RETENTION: deleting the owning scope_generations row cascades
//     to delete the quarantine rows, so retention/cleanup of an old
//     generation does not leave orphaned quarantine rows behind.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run TestReducerInputInvalidFactStoreLive -count=1
func TestReducerInputInvalidFactStoreLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres reducer_input_invalid_facts proof")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db := SQLDB{DB: sqlDB}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := ApplyBootstrap(ctx, db); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	suffix := fmt.Sprintf("4630-live-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	now := time.Now().UTC()

	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text,
		        jsonb_build_object('repo_id', $1::text))
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeID, now, generationID,
	); err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
		ON CONFLICT (generation_id) DO NOTHING`,
		generationID, scopeID, now,
	); err != nil {
		t.Fatalf("seed scope_generations: %v", err)
	}

	store := NewReducerInputInvalidFactStore(db)
	records := []reducer.QuarantinedFactRecord{
		{
			FactID: "fact-" + suffix + "-1", FactKind: "aws_resource", MissingField: "account_id",
			FailureClass: "input_invalid", Domain: "aws_resource_materialization",
			ScopeID: scopeID, GenerationID: generationID, DecidedAt: now,
		},
		{
			FactID: "fact-" + suffix + "-2", FactKind: "aws_resource", MissingField: "region",
			FailureClass: "input_invalid", Domain: "aws_resource_materialization",
			ScopeID: scopeID, GenerationID: generationID, DecidedAt: now,
		},
	}

	if err := store.WriteQuarantinedFacts(ctx, records); err != nil {
		t.Fatalf("WriteQuarantinedFacts() error = %v", err)
	}

	var count int
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM reducer_input_invalid_facts WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&count); err != nil {
		t.Fatalf("count after first write: %v", err)
	}
	if count != 2 {
		t.Fatalf("row count after first write = %d, want 2", count)
	}

	// Idempotent replay: the exact same batch again (decided_at advanced, as a
	// real retry's timestamp would be) must not duplicate rows or error.
	replay := make([]reducer.QuarantinedFactRecord, len(records))
	copy(replay, records)
	for i := range replay {
		replay[i].DecidedAt = now.Add(time.Minute)
	}
	if err := store.WriteQuarantinedFacts(ctx, replay); err != nil {
		t.Fatalf("WriteQuarantinedFacts() replay error = %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM reducer_input_invalid_facts WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&count); err != nil {
		t.Fatalf("count after replay: %v", err)
	}
	if count != 2 {
		t.Fatalf("row count after idempotent replay = %d, want 2 (ON CONFLICT DO NOTHING must dedupe on the natural key)", count)
	}

	// PER-DOMAIN TRUTH: the exact same fact_id/missing_field quarantined by a
	// SECOND, DIFFERENT domain must land as a second durable row, not collide
	// with (and be silently dropped by) the first domain's row. This is the
	// codex P2 finding on PR #5252: without domain in the natural key/ON
	// CONFLICT target, the second domain's insert would be a no-op and a
	// domain-filtered read for the second domain would falsely return empty.
	secondDomainRecords := []reducer.QuarantinedFactRecord{
		{
			FactID: records[0].FactID, FactKind: records[0].FactKind, MissingField: records[0].MissingField,
			FailureClass: "input_invalid", Domain: "aws_relationship_join",
			ScopeID: scopeID, GenerationID: generationID, DecidedAt: now,
		},
	}
	if err := store.WriteQuarantinedFacts(ctx, secondDomainRecords); err != nil {
		t.Fatalf("WriteQuarantinedFacts() second-domain error = %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM reducer_input_invalid_facts WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&count); err != nil {
		t.Fatalf("count after second-domain write: %v", err)
	}
	if count != 3 {
		t.Fatalf("row count after second-domain quarantine of the same fact/field = %d, want 3 (2 original + 1 new per-domain row; domain must be part of the natural key)", count)
	}
	var domainsForFact int
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(DISTINCT domain) FROM reducer_input_invalid_facts WHERE scope_id = $1 AND generation_id = $2 AND fact_id = $3 AND missing_field = $4`,
		scopeID, generationID, records[0].FactID, records[0].MissingField,
	).Scan(&domainsForFact); err != nil {
		t.Fatalf("count distinct domains for fact: %v", err)
	}
	if domainsForFact != 2 {
		t.Fatalf("distinct domains for the same fact/field = %d, want 2 (aws_resource_materialization and aws_relationship_join both preserved)", domainsForFact)
	}

	// Idempotent replay WITHIN the second domain: writing it again must not
	// duplicate that domain's row either.
	if err := store.WriteQuarantinedFacts(ctx, secondDomainRecords); err != nil {
		t.Fatalf("WriteQuarantinedFacts() second-domain replay error = %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM reducer_input_invalid_facts WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&count); err != nil {
		t.Fatalf("count after second-domain replay: %v", err)
	}
	if count != 3 {
		t.Fatalf("row count after second-domain idempotent replay = %d, want unchanged 3", count)
	}

	// FK cascade retention: deleting the owning generation cascades to the
	// quarantine rows.
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM scope_generations WHERE generation_id = $1`, generationID); err != nil {
		t.Fatalf("delete scope_generations: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM reducer_input_invalid_facts WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&count); err != nil {
		t.Fatalf("count after generation delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("row count after FK cascade delete = %d, want 0", count)
	}

	// Cleanup the scope row (no FK from ingestion_scopes back to
	// reducer_input_invalid_facts, so this is independent of the assertion
	// above; done for hygiene on a shared live database).
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID); err != nil {
		t.Fatalf("cleanup ingestion_scopes: %v", err)
	}
}
