// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCICDSupportPaginationKeepsLegacySnapshotAcrossV3CutoverPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the legacy-to-v3 snapshot proof")
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
		scopeID      = "repository:snapshot-legacy-cutover-live"
		generationID = "generation:snapshot-legacy-cutover-live"
		supportCount = listFactsByKindPageSize + 1
	)
	digest := "sha256:" + strings.Repeat("72", 32)
	cleanupContainerImageIdentitySupportFactsLive(t, ctx, db, scopeID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentitySupportFactsLive(t, cleanupCtx, db, scopeID)
	})
	seedContainerImageIdentityLegacyCutoverSnapshotLive(
		t, ctx, db, scopeID, generationID, digest, supportCount,
	)

	switchingDB := &activeSetSwitchingDB{SQLDB: SQLDB{DB: db}, scopeID: scopeID}
	loaded, err := NewFactStore(switchingDB).ListActiveCICDRunCorrelationFacts(
		ctx, []string{digest}, nil,
	)
	if err != nil {
		t.Fatalf("list CI/CD legacy support facts: %v", err)
	}
	if switchingDB.switchErr != nil || !switchingDB.switched {
		t.Fatalf("legacy-to-v3 switch = switched:%t error:%v, want true/nil", switchingDB.switched, switchingDB.switchErr)
	}
	if len(loaded) != supportCount {
		t.Fatalf("legacy support rows = %d, want %d", len(loaded), supportCount)
	}
	for _, row := range loaded {
		if format := stringPayloadValue(row.Payload, "identity_format"); format != "digest_v2" {
			t.Fatalf("support load mixed cutover snapshots: identity_format = %q, want digest_v2", format)
		}
	}
}

func TestSBOMSupportLoadKeepsEmptyGenerationSnapshotPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the empty generation snapshot proof")
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
		scopeID       = "repository:snapshot-empty-generation-live"
		generationAID = "generation:snapshot-empty-a-live"
		generationBID = "generation:snapshot-populated-b-live"
	)
	digest := "sha256:" + strings.Repeat("70", 32)
	cleanupContainerImageIdentitySupportFactsLive(t, ctx, db, scopeID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentitySupportFactsLive(t, cleanupCtx, db, scopeID)
	})
	seedContainerImageIdentitySnapshotSetsLive(t, ctx, db, scopeID, generationAID, digest, 1)
	if _, err := db.ExecContext(ctx, `
DELETE FROM container_image_identity_supports
WHERE set_id = decode(repeat('a1', 32), 'hex');
UPDATE container_image_identity_support_sets
SET support_count = 0
WHERE set_id = decode(repeat('a1', 32), 'hex');
`); err != nil {
		t.Fatalf("make active support set empty: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($2, $1, 'test', clock_timestamp(), clock_timestamp(), 'pending', '{}'::jsonb);
`, scopeID, generationBID); err != nil {
		t.Fatalf("seed populated generation B: %v", err)
	}

	switchAction := func(switchCtx context.Context) error {
		return activateContainerImageIdentitySnapshotGenerationLive(
			switchCtx, db, scopeID, generationBID, "b2", "c2",
		)
	}
	autocommitSwitch := &activeSetSwitchingDB{
		SQLDB:            SQLDB{DB: db},
		scopeID:          scopeID,
		switchAfterQuery: func(_ string, queryNumber int) bool { return queryNumber == 1 },
		switchAction:     switchAction,
	}
	autocommitLoaded, err := NewFactStore(autocommitReadSnapshotDB{
		activeSetSwitchingDB: autocommitSwitch,
	}).ListActiveSBOMAttestationAttachmentFacts(ctx, []string{digest})
	if err != nil {
		t.Fatalf("run autocommit control: %v", err)
	}
	if len(autocommitLoaded) != 1 {
		t.Fatalf("autocommit control rows = %d, want 1 from generation B", len(autocommitLoaded))
	}
	if err := activateContainerImageIdentitySnapshotGenerationLive(
		ctx, db, scopeID, generationAID, "a1", "c1",
	); err != nil {
		t.Fatalf("restore empty generation A: %v", err)
	}

	switchingDB := &activeSetSwitchingDB{
		SQLDB:            SQLDB{DB: db},
		scopeID:          scopeID,
		switchAfterQuery: func(_ string, queryNumber int) bool { return queryNumber == 1 },
		switchAction:     switchAction,
	}
	loaded, err := NewFactStore(switchingDB).ListActiveSBOMAttestationAttachmentFacts(
		ctx, []string{digest},
	)
	if err != nil {
		t.Fatalf("list SBOM empty-generation support facts: %v", err)
	}
	if switchingDB.switchErr != nil || !switchingDB.switched {
		t.Fatalf("generation A-to-B switch = switched:%t error:%v, want true/nil", switchingDB.switched, switchingDB.switchErr)
	}
	if len(loaded) != 0 {
		t.Fatalf("empty generation-A snapshot returned %d rows, want 0", len(loaded))
	}
}

func activateContainerImageIdentitySnapshotGenerationLive(
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	setByte string,
	hashByte string,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
UPDATE scope_generations
SET status = 'superseded', superseded_at = clock_timestamp()
WHERE scope_id = $1 AND generation_id <> $2 AND status = 'active'
`, scopeID, generationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE scope_generations
SET status = 'active', activated_at = clock_timestamp(), superseded_at = NULL
WHERE scope_id = $1 AND generation_id = $2
`, scopeID, generationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID, generationID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE container_image_identity_scope_state
SET active_set_id = decode(repeat($3, 32), 'hex'),
    last_set_id = decode(repeat($3, 32), 'hex'),
    last_set_hash = decode(repeat($4, 32), 'hex'),
    updated_at = clock_timestamp()
WHERE scope_id = $1 AND active_generation_id = $2;
`, scopeID, generationID, setByte, hashByte); err != nil {
		return err
	}
	return tx.Commit()
}

type autocommitReadSnapshotDB struct {
	*activeSetSwitchingDB
}

func (db autocommitReadSnapshotDB) BeginReadOnlyRepeatableRead(
	context.Context,
) (Transaction, error) {
	return autocommitReadSnapshotTransaction{db: db.activeSetSwitchingDB}, nil
}

type autocommitReadSnapshotTransaction struct {
	db *activeSetSwitchingDB
}

func (tx autocommitReadSnapshotTransaction) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	return tx.db.QueryContext(ctx, query, args...)
}

func (tx autocommitReadSnapshotTransaction) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return tx.db.ExecContext(ctx, query, args...)
}

func (autocommitReadSnapshotTransaction) Commit() error   { return nil }
func (autocommitReadSnapshotTransaction) Rollback() error { return nil }

func seedContainerImageIdentityLegacyCutoverSnapshotLive(
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
		t.Fatalf("begin legacy-cutover snapshot seed: %v", err)
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
VALUES (decode(repeat('b2', 32), 'hex'), $1, decode(repeat('c2', 32), 'hex'), $2)
`, args: []any{scopeID, supportCount}},
		{query: `
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
)
SELECT decode(repeat('b2', 32), 'hex'), $1::text,
       sha256(convert_to(format('typed-support-%06s', n), 'UTF8')),
       format('registry.example.com/team/typed-%06s@%s', n, $1::text),
       'registry.example.com/team/cutover', 'exact_digest', 'digest', 1,
       ARRAY['repository:cutover-proof']::text[],
       ARRAY['observed_resource', 'source_declaration']::text[]
FROM generate_series(1, $2::integer) AS n
`, args: []any{digest, supportCount}},
		{query: `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT format('legacy:snapshot:%06s', n), $1, $2,
       'reducer_container_image_identity', format('stable:legacy:%06s', n),
       'git', 'intent:legacy-snapshot', clock_timestamp(), clock_timestamp(),
       jsonb_build_object(
           'digest', $3::text,
           'image_ref', format('registry.example.com/team/legacy-%06s@%s', n, $3::text),
           'repository_id', 'registry.example.com/team/cutover',
           'outcome', 'exact_digest', 'identity_strength', 'digest',
           'canonical_writes', 1,
           'source_repository_ids', jsonb_build_array('repository:cutover-proof')
       )
FROM generate_series(1, $4::integer) AS n
`, args: []any{scopeID, generationID, digest, supportCount}},
	}
	for index, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed legacy-cutover snapshot statement %d: %v", index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit legacy-cutover snapshot seed: %v", err)
	}
}
