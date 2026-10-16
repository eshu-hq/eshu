// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestPackageManifestConsumptionScopeRebuildKeepsOldWriterDirtyLive is the
// #7663 dirty-fence proof. An old writer (no sidecar-writer GUC) whose
// transaction began before the repair commits a manifest fact behind the
// repair's page cursor while the repair is mid-scope. The repair must not
// clear the scope's dirty row: the old writer's trigger stamps marked_at with
// its own, older transaction start, so only a row-version compare can tell
// the write landed after the repair captured the row. A later rebuild must
// repair the scope before readers see it as ready.
func TestPackageManifestConsumptionScopeRebuildKeepsOldWriterDirtyLive(t *testing.T) {
	ctx, database := openPackageManifestConsumptionPageProofDatabase(t)
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	seedPackageManifestConsumptionPageFacts(t, ctx, database, 0, 6*packageManifestConsumptionKeyBackfillBatchSize)
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("initial BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "old-writer-before", "old-before")
	assertPackageManifestConsumptionReady(t, ctx, database, false)

	oldWriter, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin old writer: %v", err)
	}
	defer func() { _ = oldWriter.Rollback() }()
	if _, err := oldWriter.ExecContext(ctx, `SELECT now()`); err != nil {
		t.Fatalf("start old writer transaction: %v", err)
	}

	blocker := holdPackageManifestConsumptionPageKey(t, ctx, database, 3*packageManifestConsumptionKeyBackfillBatchSize+5)
	done := make(chan error, 1)
	go func() { done <- BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}) }()
	waitForPackageManifestConsumptionKeyInsertWait(t, ctx, database)

	// "old-writer-during" sorts before every page-fact, so it lands behind
	// the repair's cursor and only a later rebuild can derive its keys.
	written := make(chan error, 1)
	go func() {
		seedErr := insertOldWriterManifestFact(ctx, oldWriter, "old-writer-during", "old-during")
		if seedErr == nil {
			seedErr = oldWriter.Commit()
		}
		written <- seedErr
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("old writer insert during repair: %v", err)
		}
		written <- nil
	case <-time.After(2 * time.Second):
		t.Errorf("old writer manifest insert did not commit within 2s while the repair was mid-scope (want the backfill not to block fact writers)")
	}

	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release page blocker: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("repair BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	if err := <-written; err != nil {
		t.Fatalf("old writer insert: %v", err)
	}
	runPackageManifestConsumptionBackfillUntilReady(t, ctx, database)
	assertPackageManifestConsumptionSidecarsMatchFacts(t, ctx, database)
	assertPackageManifestConsumptionKeyPresent(t, ctx, database, "old-writer-during", "old-during")
}

// TestPackageManifestConsumptionScopeRebuildKeepsConcurrentWriterKeysLive
// proves a current-version writer that updates manifest facts on both sides
// of the repair's page cursor keeps its keys: no page writes stale keys over
// the writer's, and the final sidecar equals the facts.
func TestPackageManifestConsumptionScopeRebuildKeepsConcurrentWriterKeysLive(t *testing.T) {
	ctx, database := openPackageManifestConsumptionPageProofDatabase(t)
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	seedPackageManifestConsumptionPageFacts(t, ctx, database, 0, 8*packageManifestConsumptionKeyBackfillBatchSize)
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("initial BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "old-writer-dirty", "dirty")
	assertPackageManifestConsumptionReady(t, ctx, database, false)

	blocker := holdPackageManifestConsumptionPageKey(t, ctx, database, 4*packageManifestConsumptionKeyBackfillBatchSize+9)
	done := make(chan error, 1)
	go func() { done <- BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}) }()
	waitForPackageManifestConsumptionKeyInsertWait(t, ctx, database)

	behind, ahead := packageManifestConsumptionPageFactID(600), packageManifestConsumptionPageFactID(3300)
	written := make(chan error, 1)
	go func() {
		written <- renameManifestFactsLikeIngester(ctx, database, map[string]string{behind: "renamed-behind", ahead: "renamed-ahead"})
	}()
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release page blocker: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("repair BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	if err := <-written; err != nil {
		t.Fatalf("current-version writer: %v", err)
	}
	runPackageManifestConsumptionBackfillUntilReady(t, ctx, database)
	assertPackageManifestConsumptionSidecarsMatchFacts(t, ctx, database)
	assertPackageManifestConsumptionKeyPresent(t, ctx, database, behind, "renamed-behind")
	assertPackageManifestConsumptionKeyPresent(t, ctx, database, ahead, "renamed-ahead")
}

func insertOldWriterManifestFact(ctx context.Context, tx *sql.Tx, factID, name string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'sidecar-live-scope', 'sidecar-live-generation', 'content_entity', $1,
        'git', $1, clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', 'repository:sidecar-live', 'entity_type', 'Variable', 'entity_name', $2::text,
            'entity_metadata', jsonb_build_object('config_kind', 'dependency', 'package_manager', 'npm')))`, factID, name)
	return err
}

// renameManifestFactsLikeIngester follows the ingester's commit order: set the
// sidecar-writer GUC, take the scope row lock, write facts, then refresh both
// sidecars for exactly those facts in the same transaction.
func renameManifestFactsLikeIngester(ctx context.Context, database *sql.DB, renames map[string]string) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, setPackageManifestConsumptionKeysWriterSQL); err != nil {
		return err
	}
	if err := upsertSidecarLiveScopeLikeIngester(ctx, tx); err != nil {
		return err
	}
	factIDs := make([]string, 0, len(renames))
	for factID, name := range renames {
		if _, err := tx.ExecContext(ctx, `UPDATE fact_records SET payload = jsonb_set(payload, '{entity_name}', to_jsonb($2::text)) WHERE fact_id = $1`, factID, name); err != nil {
			return err
		}
		factIDs = append(factIDs, factID)
	}
	envelopes, err := loadFactEnvelopes(ctx, SQLTx{Tx: tx}, `WHERE fact_id = ANY($1::text[])`, array.StringArray(factIDs))
	if err != nil {
		return err
	}
	if err := refreshPackageManifestConsumptionKeys(ctx, SQLTx{Tx: tx}, envelopes); err != nil {
		return err
	}
	if err := refreshPackageRegistryIdentityKeys(ctx, SQLTx{Tx: tx}, envelopes); err != nil {
		return err
	}
	return tx.Commit()
}

func runPackageManifestConsumptionBackfillUntilReady(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for pass := 0; pass < 5; pass++ {
		if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
			t.Fatalf("BackfillPackageManifestConsumptionKeys() pass %d: %v", pass, err)
		}
		ready, err := PackageManifestConsumptionKeysReady(ctx, SQLDB{DB: database})
		if err != nil {
			t.Fatalf("PackageManifestConsumptionKeysReady(): %v", err)
		}
		if ready {
			return
		}
	}
	t.Fatal("package manifest consumption keys did not become ready within 5 passes")
}

// packageManifestConsumptionPageEnvelopes builds the envelope that
// seedPackageManifestConsumptionPageFacts writes for index, so a test can
// derive the exact sidecar key the rebuild will insert for it.
func packageManifestConsumptionPageEnvelopes(index int) []facts.Envelope {
	return []facts.Envelope{{
		FactID:       packageManifestConsumptionPageFactID(index),
		ScopeID:      "sidecar-live-scope",
		GenerationID: "sidecar-live-generation",
		FactKind:     "content_entity",
		SourceRef:    facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"repo_id":     "repository:sidecar-live",
			"entity_type": "Variable",
			"entity_name": packageManifestConsumptionPagePackageName(index),
			"entity_metadata": map[string]any{
				"config_kind": "dependency", "package_manager": "npm",
			},
		},
	}}
}

func loadFactEnvelopes(ctx context.Context, queryer db.Queryer, where string, args ...any) ([]facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
       collector_kind, fencing_token, source_confidence, source_system,
       source_fact_key, COALESCE(source_uri, ''), COALESCE(source_record_id, ''),
       observed_at, is_tombstone, payload
FROM fact_records `+where+` ORDER BY fact_id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var loaded []facts.Envelope
	for rows.Next() {
		envelope, err := backfill.ScanFactEnvelope(rows)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, envelope)
	}
	return loaded, rows.Err()
}

// assertPackageManifestConsumptionSidecarsMatchFacts derives both sidecars
// from every fact in one unpaged read, independent of the backfill's paging
// and transactions, and requires the stored rows to equal that derivation.
// The pre-#7663 single-transaction rebuild was the same pure function of the
// facts, so equality here is also equality with its result.
func assertPackageManifestConsumptionSidecarsMatchFacts(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	envelopes, err := loadFactEnvelopes(ctx, SQLDB{DB: database}, "")
	if err != nil {
		t.Fatalf("load facts for independent derivation: %v", err)
	}
	var wantManifest, wantRegistry []string
	for _, row := range packageManifestConsumptionKeyRows(envelopes) {
		wantManifest = append(wantManifest, fmt.Sprintf("%s|%s|%s|%s|%s|%s", row.FactID, row.ScopeID, row.GenerationID, row.RepositoryID, row.Ecosystem, row.PackageName))
	}
	for _, row := range packageRegistryIdentityKeyRows(envelopes) {
		wantRegistry = append(wantRegistry, fmt.Sprintf("%s|%s|%s|%s|%s|%s", row.FactID, row.ScopeID, row.GenerationID, row.Ecosystem, row.PackageName, row.PackageID))
	}
	gotManifest := queryJoinedRows(t, ctx, database, `SELECT concat_ws('|', fact_id, scope_id, generation_id, repository_id, ecosystem, package_name) FROM package_manifest_consumption_keys`)
	gotRegistry := queryJoinedRows(t, ctx, database, `SELECT concat_ws('|', fact_id, scope_id, generation_id, ecosystem, package_name, package_id) FROM package_registry_identity_keys`)
	assertSameRowSet(t, "package_manifest_consumption_keys", gotManifest, wantManifest)
	assertSameRowSet(t, "package_registry_identity_keys", gotRegistry, wantRegistry)
}

func queryJoinedRows(t *testing.T, ctx context.Context, database *sql.DB, query string) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("read sidecar rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatalf("scan sidecar row: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read sidecar rows: %v", err)
	}
	return out
}

func assertSameRowSet(t *testing.T, table string, got, want []string) {
	t.Helper()
	got, want = slices.Compact(slices.Sorted(slices.Values(got))), slices.Compact(slices.Sorted(slices.Values(want)))
	if slices.Equal(got, want) {
		return
	}
	missing, extra := 0, 0
	for _, row := range want {
		if _, found := slices.BinarySearch(got, row); !found {
			missing++
		}
	}
	for _, row := range got {
		if _, found := slices.BinarySearch(want, row); !found {
			extra++
		}
	}
	t.Fatalf("%s rows = %d, derived from facts = %d (missing %d, stale or extra %d)", table, len(got), len(want), missing, extra)
}

func assertPackageManifestConsumptionKeyPresent(t *testing.T, ctx context.Context, database *sql.DB, factID, packageName string) {
	t.Helper()
	var found bool
	if err := database.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM package_manifest_consumption_keys WHERE fact_id = $1 AND package_name = $2)`, factID, packageName).Scan(&found); err != nil {
		t.Fatalf("check sidecar key: %v", err)
	}
	if !found {
		t.Fatalf("sidecar key (%s, %s) is missing", factID, packageName)
	}
}
