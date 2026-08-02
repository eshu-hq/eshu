// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCICDSupportPaginationUsesOneActiveSetSnapshotPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the active-set snapshot proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	const (
		scopeID      = "repository:snapshot-consistency-live"
		generationID = "generation:snapshot-consistency-live"
		supportCount = listFactsByKindPageSize + 1
	)
	digest := "sha256:" + strings.Repeat("74", 32)
	cleanupContainerImageIdentitySupportFactsLive(t, ctx, db, scopeID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentitySupportFactsLive(t, cleanupCtx, db, scopeID)
	})
	seedContainerImageIdentitySnapshotSetsLive(
		t, ctx, db, scopeID, generationID, digest, supportCount,
	)

	switchingDB := &activeSetSwitchingDB{
		SQLDB:   SQLDB{DB: db},
		scopeID: scopeID,
	}
	rows, err := NewFactStore(switchingDB).ListActiveCICDRunCorrelationFacts(
		ctx,
		[]string{digest},
		nil,
	)
	if err != nil {
		t.Fatalf("list CI/CD support facts: %v", err)
	}
	if switchingDB.switchErr != nil {
		t.Fatalf("switch active support set after first page: %v", switchingDB.switchErr)
	}
	if !switchingDB.switched {
		t.Fatal("active support set was not switched between pages")
	}
	if len(rows) != supportCount {
		t.Fatalf("support rows = %d, want %d", len(rows), supportCount)
	}
	for _, row := range rows {
		if imageRef := stringPayloadValue(row.Payload, "image_ref"); !strings.Contains(imageRef, "/set-a-") {
			t.Fatalf("support page mixed active sets: image_ref = %q, want set-a snapshot", imageRef)
		}
	}
}

func TestSBOMSupportStreamsUseOneActiveSetSnapshotPostgresLive(t *testing.T) {
	runActiveSetSnapshotLive(
		t,
		"sbom",
		func(_ string, queryNumber int) bool { return queryNumber == 1 },
		func(ctx context.Context, store *FactStore, digest string) ([]facts.Envelope, error) {
			return store.ListActiveSBOMAttestationAttachmentFacts(ctx, []string{digest})
		},
	)
}

func TestSupplySupportPagesUseOneActiveSetSnapshotPostgresLive(t *testing.T) {
	runActiveSetSnapshotLive(
		t,
		"supply",
		nil,
		func(ctx context.Context, store *FactStore, digest string) ([]facts.Envelope, error) {
			loaded, truncated, err := store.ListActiveSupplyChainImpactFacts(
				ctx,
				reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{digest}},
			)
			if truncated {
				t.Fatal("supply-chain support facts unexpectedly truncated")
			}
			return loaded, err
		},
	)
}

func TestSQLDBBeginReadOnlyRepeatableReadOptionsPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the transaction-option proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tx, err := (SQLDB{DB: db}).BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		t.Fatalf("begin read-only repeatable-read transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
SELECT current_setting('transaction_isolation'), current_setting('transaction_read_only')
`)
	if err != nil {
		t.Fatalf("query transaction options: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("transaction options row missing: %v", rows.Err())
	}
	var isolation, readOnly string
	if err := rows.Scan(&isolation, &readOnly); err != nil {
		t.Fatalf("scan transaction options: %v", err)
	}
	if isolation != "repeatable read" || readOnly != "on" {
		t.Fatalf("transaction options = isolation:%q read-only:%q, want repeatable read/on", isolation, readOnly)
	}
}

func runActiveSetSnapshotLive(
	t *testing.T,
	name string,
	switchAfterQuery func(string, int) bool,
	load func(context.Context, *FactStore, string) ([]facts.Envelope, error),
) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the active-set snapshot proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	scopeID := "repository:snapshot-consistency-" + name + "-live"
	generationID := "generation:snapshot-consistency-" + name + "-live"
	digest := "sha256:" + strings.Repeat("73", 32)
	cleanupContainerImageIdentitySupportFactsLive(t, ctx, db, scopeID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentitySupportFactsLive(t, cleanupCtx, db, scopeID)
	})
	seedContainerImageIdentitySnapshotSetsLive(
		t, ctx, db, scopeID, generationID, digest, listFactsByKindPageSize+1,
	)

	switchingDB := &activeSetSwitchingDB{
		SQLDB:            SQLDB{DB: db},
		scopeID:          scopeID,
		switchAfterQuery: switchAfterQuery,
	}
	loaded, err := load(ctx, NewFactStore(switchingDB), digest)
	if err != nil {
		t.Fatalf("load active support facts: %v", err)
	}
	if switchingDB.switchErr != nil {
		t.Fatalf("switch active support set: %v", switchingDB.switchErr)
	}
	if !switchingDB.switched {
		t.Fatal("active support set was not switched during the load")
	}
	if len(loaded) != listFactsByKindPageSize+1 {
		t.Fatalf("support rows = %d, want %d", len(loaded), listFactsByKindPageSize+1)
	}
	for _, row := range loaded {
		if imageRef := stringPayloadValue(row.Payload, "image_ref"); !strings.Contains(imageRef, "/set-a-") {
			t.Fatalf("support load mixed active sets: image_ref = %q, want set-a snapshot", imageRef)
		}
	}
}

type activeSetSwitchingDB struct {
	SQLDB
	scopeID          string
	switchOnce       sync.Once
	switched         bool
	switchErr        error
	queryNumber      int
	switchAfterQuery func(string, int) bool
	switchAction     func(context.Context) error
}

func (db *activeSetSwitchingDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	rows, err := db.SQLDB.QueryContext(ctx, query, args...)
	if err != nil || !db.shouldSwitchAfter(query) {
		return rows, err
	}
	return &afterCloseRows{Rows: rows, afterClose: func() { db.switchActiveSet(ctx) }}, nil
}

func (db *activeSetSwitchingDB) BeginReadOnlyRepeatableRead(
	ctx context.Context,
) (Transaction, error) {
	tx, err := db.DB.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return nil, err
	}
	return &activeSetSwitchingTx{SQLTx: SQLTx{Tx: tx}, parent: db}, nil
}

func (db *activeSetSwitchingDB) switchActiveSet(ctx context.Context) {
	db.switchOnce.Do(func() {
		if db.switchAction != nil {
			db.switchErr = db.switchAction(ctx)
			db.switched = db.switchErr == nil
			return
		}
		_, db.switchErr = db.DB.ExecContext(ctx, `
UPDATE container_image_identity_scope_state
SET active_set_id = decode(repeat('b2', 32), 'hex'),
    last_set_id = decode(repeat('b2', 32), 'hex'),
    last_set_hash = decode(repeat('c2', 32), 'hex'),
    updated_at = clock_timestamp()
WHERE scope_id = $1
`, db.scopeID)
		db.switched = db.switchErr == nil
	})
}

func (db *activeSetSwitchingDB) shouldSwitchAfter(query string) bool {
	db.queryNumber++
	if db.switchAfterQuery != nil {
		return db.switchAfterQuery(query, db.queryNumber)
	}
	return strings.Contains(query, "container_image_identity_current_support_facts_for")
}

type activeSetSwitchingTx struct {
	SQLTx
	parent *activeSetSwitchingDB
}

func (tx *activeSetSwitchingTx) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	rows, err := tx.SQLTx.QueryContext(ctx, query, args...)
	if err != nil || !tx.parent.shouldSwitchAfter(query) {
		return rows, err
	}
	return &afterCloseRows{Rows: rows, afterClose: func() { tx.parent.switchActiveSet(ctx) }}, nil
}

type afterCloseRows struct {
	Rows
	afterClose func()
	close      sync.Once
}

func (rows *afterCloseRows) Close() error {
	err := rows.Rows.Close()
	rows.close.Do(rows.afterClose)
	return err
}

func seedContainerImageIdentitySnapshotSetsLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	digest string,
	supportCount int,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin active-set snapshot seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{
		{query: `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1,
          clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)
`, args: []any{scopeID}},
		{query: `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($2, $1, 'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)
`, args: []any{scopeID, generationID}},
		{query: `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, args: []any{scopeID, generationID}},
		{query: `
INSERT INTO container_image_identity_support_sets (set_id, scope_id, content_hash, support_count)
VALUES
    (decode(repeat('a1', 32), 'hex'), $1, decode(repeat('c1', 32), 'hex'), $2),
    (decode(repeat('b2', 32), 'hex'), $1, decode(repeat('c2', 32), 'hex'), $2)
`, args: []any{scopeID, supportCount}},
		{query: `
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
)
SELECT set_id, $1::text, sha256(convert_to(format('support-%06s', n), 'UTF8')),
       format('registry.example.com/team/%s-%06s@%s', set_name, n, $1::text),
       'registry.example.com/team/snapshot', 'exact_digest', 'digest', 1,
       ARRAY['repository:snapshot-proof']::text[],
       ARRAY['observed_resource', 'source_declaration']::text[]
FROM (VALUES
    (decode(repeat('a1', 32), 'hex'), 'set-a'),
    (decode(repeat('b2', 32), 'hex'), 'set-b')
) AS sets(set_id, set_name)
CROSS JOIN generate_series(1, $2::integer) AS n
`, args: []any{digest, supportCount}},
		{query: `
UPDATE container_image_identity_scope_state
SET active_set_id = decode(repeat('a1', 32), 'hex'),
    last_set_id = decode(repeat('a1', 32), 'hex'),
    last_set_hash = decode(repeat('c1', 32), 'hex'),
    source_system = 'git', collector_kind = 'git', source_confidence = 'inferred',
    source_fact_key = 'intent:snapshot-consistency-live',
    observed_at = clock_timestamp(), ingested_at = clock_timestamp()
WHERE scope_id = $1 AND active_generation_id = $2
`, args: []any{scopeID, generationID}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed active-set snapshot rows: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit active-set snapshot seed: %v", err)
	}
}

var (
	_ ExecQueryer = (*activeSetSwitchingDB)(nil)
	_ Transaction = (*activeSetSwitchingTx)(nil)
)
