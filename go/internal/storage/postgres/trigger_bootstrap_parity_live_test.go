// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	webhookstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// TestWebhookTriggerStoreRunsOnBootstrapSchemaLive proves a schema built by
// ApplyBootstrap alone (what bootstrap-data-plane and bootstrap-index apply)
// carries everything the webhook trigger store needs (#7777). The ingester and
// collector-git claim triggers without running the store's EnsureSchema, so the
// claim, fenced handoff, and stale-claim reap must work on the migrations
// alone, and EnsureSchema must find nothing left to add: the same columns and
// the same indexes on webhook_refresh_triggers before and after it runs.
// Because the store DDL uses IF NOT EXISTS throughout, that second check
// cannot see an object both sides define differently, so the bootstrap shape
// must also equal the shape EnsureSchema builds alone on an empty schema.
//
// It runs in the live-postgres-readiness runner. Run locally against a
// disposable PostgreSQL:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
//	  go test ./internal/storage/postgres -run TestWebhookTriggerStoreRunsOnBootstrapSchemaLive -count=1
func TestWebhookTriggerStoreRunsOnBootstrapSchemaLive(t *testing.T) {
	// The live-postgres-readiness runner sets only the family DSN pair; a
	// family DSN without its disposable acknowledgment fails closed.
	if familyDSN := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); familyDSN != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", familyDSN)
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN to run the #7777 webhook bootstrap parity proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	adminDB := openActiveOCIWarningIndexProofDB(t, dsn)
	schema, database := openWebhookParityProofSchema(ctx, t, adminDB, dsn, "bootstrap")
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	bootstrapShape := webhookTriggerTableShape(ctx, t, database, schema)

	storeOnlySchema, storeOnlyDB := openWebhookParityProofSchema(ctx, t, adminDB, dsn, "store")
	if err := webhookstore.NewWebhookTriggerStore(SQLDB{DB: storeOnlyDB}).EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema() on an empty schema: %v", err)
	}
	if storeOnlyShape := webhookTriggerTableShape(ctx, t, storeOnlyDB, storeOnlySchema); !slices.Equal(bootstrapShape, storeOnlyShape) {
		t.Fatalf("webhook_refresh_triggers differs between the bootstrap migrations and a store-only schema\nbootstrap:  %v\nstore-only: %v",
			bootstrapShape, storeOnlyShape)
	}

	store := webhookstore.NewWebhookTriggerStore(SQLDB{DB: database})
	base := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	handed := storeBootstrapProofTrigger(ctx, t, store, "3333333333333333333333333333333333333333", base)
	claimed, err := store.ClaimQueuedTriggers(ctx, "ingester", base.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers() on the bootstrap schema: %v", err)
	}
	if len(claimed) != 1 || claimed[0].TriggerID != handed.TriggerID || claimed[0].ClaimFencingToken < 1 {
		t.Fatalf("claimed = %+v, want trigger %s with a positive fencing token", claimed, handed.TriggerID)
	}
	if err := store.MarkTriggersHandedOff(ctx, claimed, base.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkTriggersHandedOff() on the bootstrap schema: %v", err)
	}

	stale := storeBootstrapProofTrigger(ctx, t, store, "4444444444444444444444444444444444444444", base)
	if _, err := store.ClaimQueuedTriggers(ctx, "ingester", base.Add(time.Minute), 10); err != nil {
		t.Fatalf("ClaimQueuedTriggers() for the stale claim: %v", err)
	}
	requeued, exhausted, err := store.ReapExpiredTriggerClaims(ctx, base.Add(time.Hour), 5, 10, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() on the bootstrap schema: %v", err)
	}
	if len(requeued) != 1 || requeued[0].TriggerID != stale.TriggerID || len(exhausted) != 0 {
		t.Fatalf("reap requeued %+v exhausted %+v, want only trigger %s requeued", requeued, exhausted, stale.TriggerID)
	}

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema() on the bootstrap schema: %v", err)
	}
	ensuredShape := webhookTriggerTableShape(ctx, t, database, schema)
	if !slices.Equal(bootstrapShape, ensuredShape) {
		t.Fatalf("webhook_refresh_triggers differs between the bootstrap migrations and the store schema\nbootstrap: %v\nensured:   %v",
			bootstrapShape, ensuredShape)
	}
}

// openWebhookParityProofSchema creates a uniquely named scratch schema that is
// dropped at cleanup and returns it with a connection whose search_path is
// that schema.
func openWebhookParityProofSchema(
	ctx context.Context,
	t *testing.T,
	adminDB *sql.DB,
	dsn string,
	label string,
) (string, *sql.DB) {
	t.Helper()
	schema := fmt.Sprintf("eshu_7777_webhook_%s_%d", label, time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create %s proof schema: %v", label, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop %s proof schema: %v", label, err)
		}
	})
	database, err := sql.Open("pgx", activeOCIWarningIndexSchemaDSN(t, dsn, schema))
	if err != nil {
		t.Fatalf("open %s proof database: %v", label, err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return schema, database
}

func storeBootstrapProofTrigger(
	ctx context.Context,
	t *testing.T,
	store *webhookstore.WebhookTriggerStore,
	targetSHA string,
	receivedAt time.Time,
) webhook.StoredTrigger {
	t.Helper()
	stored, err := store.StoreTrigger(ctx, webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionAccepted,
		DeliveryID:           "delivery-" + targetSHA[:8],
		RepositoryExternalID: "42",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		BeforeSHA:            "1111111111111111111111111111111111111111",
		TargetSHA:            targetSHA,
	}, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() on the bootstrap schema: %v", err)
	}
	return stored
}

// webhookTriggerTableShape returns the sorted columns (name, type,
// nullability, default) and index definitions of webhook_refresh_triggers in
// schema, with the schema name removed so two reads compare by shape alone.
func webhookTriggerTableShape(ctx context.Context, t *testing.T, database *sql.DB, schema string) []string {
	t.Helper()
	var shape []string
	rows, err := database.QueryContext(ctx, `
SELECT 'column ' || column_name || ' ' || data_type || ' nullable=' || is_nullable || ' default=' || COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = 'webhook_refresh_triggers'
UNION ALL
SELECT 'index ' || indexdef
FROM pg_indexes
WHERE schemaname = $1 AND tablename = 'webhook_refresh_triggers'`, schema)
	if err != nil {
		t.Fatalf("read webhook_refresh_triggers shape: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan webhook_refresh_triggers shape: %v", err)
		}
		shape = append(shape, strings.ReplaceAll(line, schema+".", ""))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read webhook_refresh_triggers shape: %v", err)
	}
	if len(shape) == 0 {
		t.Fatal("webhook_refresh_triggers has no columns in the bootstrap schema")
	}
	slices.Sort(shape)
	return shape
}
