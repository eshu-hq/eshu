// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestReadinessIgnoresOrphanPackageKeysLive is #7420 accuracy condition (b)
// for the two package key readers: the bootstrap drops the per-fact foreign
// keys (migrations 189 and 190), so the orphan inserts below succeed, and a
// manifest consumption key or registry identity key with no fact behind it
// must not change readiness. Both readers join each key to its
// live fact by (fact_id, scope_id, generation_id), so orphans placed in the
// active generations, the worst case, add no package.consumption evidence and
// no second registry owner (which would make ownership ambiguous).
func TestReadinessIgnoresOrphanPackageKeysLive(t *testing.T) {
	// The readiness runner provides the package consumption family DSN with
	// its disposable opt-in; a local run may use ESHU_POSTGRES_DSN instead.
	dsn := strings.TrimSpace(os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN"))
	if dsn != "" && os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE") != "1" {
		t.Fatal("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN is set without ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=1")
	}
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN or ESHU_POSTGRES_DSN to run the orphan package key reader proof")
	}
	db := postgresproof.OpenIsolatedSchema(t, dsn, "eshu_7420_orphan_keys", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx := context.Background()
	seedReadinessManifestScope(t, ctx, db, "consuming-one", "repository:consuming-one", "lodash")
	seedReadinessManifestScope(t, ctx, db, "consuming-two", "repository:consuming-two", "lodash")
	seedReadinessRegistryPackage(t, ctx, db, "registry-default", "npm://registry.npmjs.org/lodash")
	if err := storagepostgres.BackfillPackageManifestConsumptionKeys(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	store := NewPostgresReadinessStore(db)
	read := func(query ReadinessQuery) int {
		t.Helper()
		snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, query)
		if err != nil {
			t.Fatalf("ReadSupplyChainImpactReadiness(%+v): %v", query, err)
		}
		return readinessPackageConsumptionFactCount(snapshot)
	}
	packageQuery := ReadinessQuery{PackageID: "npm://registry.npmjs.org/lodash"}
	// A package anchor takes the key arm ($20); a repository-only anchor reads
	// facts directly and never touches the keys, so the orphan check pairs
	// the package with the orphan's repository.
	orphanRepoQuery := ReadinessQuery{PackageID: "npm://registry.npmjs.org/lodash", RepositoryID: "repository:orphan"}
	if got := read(packageQuery); got != 2 {
		t.Fatalf("baseline package.consumption fact count = %d, want 2", got)
	}

	for _, statement := range []string{
		`INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
VALUES ('orphan-manifest', 'readiness-consumption-consuming-one', 'readiness-consumption-consuming-one-gen',
        'repository:orphan', 'npm', 'lodash')`,
		`INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id)
VALUES ('orphan-registry', 'readiness-registry-default', 'readiness-registry-default-gen',
        'npm', 'lodash', 'npm://registry.example/lodash')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed orphan keys: %v", err)
		}
	}
	if got := read(packageQuery); got != 2 {
		t.Fatalf("package.consumption fact count with orphan keys = %d, want 2 (orphans invisible)", got)
	}
	if got := read(orphanRepoQuery); got != 0 {
		t.Fatalf("orphan repository package.consumption fact count = %d, want 0", got)
	}
	// Sensitivity, manifest side: the same manifest key with a live fact
	// behind it is package.consumption evidence for repository:orphan, so the
	// orphan above would have mattered had it been read.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('orphan-manifest', 'readiness-consumption-consuming-one', 'readiness-consumption-consuming-one-gen',
        'content_entity', 'orphan-manifest', 'git', 'orphan-manifest',
        clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object('repo_id', 'repository:orphan', 'entity_type', 'Variable', 'entity_name', 'lodash',
            'entity_metadata', jsonb_build_object('config_kind', 'dependency', 'package_manager', 'npm')))`); err != nil {
		t.Fatalf("give the orphan manifest key a fact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM package_manifest_consumption_key_dirty_scopes`); err != nil {
		t.Fatalf("clear dirty scope mark: %v", err)
	}
	if got := read(orphanRepoQuery); got != 1 {
		t.Fatalf("repository:orphan package.consumption fact count with a live fact = %d, want 1", got)
	}
	if got := read(packageQuery); got != 3 {
		t.Fatalf("package.consumption fact count with a live fact behind the manifest key = %d, want 3", got)
	}
	// Sensitivity, registry side: the same registry key with a live fact
	// behind it is a second owner, so the orphan above would have mattered.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('orphan-registry', 'readiness-registry-default', 'readiness-registry-default-gen',
        'package_registry.package', 'orphan-registry', 'registry', 'orphan-registry',
        clock_timestamp(), clock_timestamp(), FALSE,
        '{"package_id":"npm://registry.example/lodash","ecosystem":"npm","raw_name":"lodash"}'::jsonb)`); err != nil {
		t.Fatalf("give the orphan registry key a fact: %v", err)
	}
	// The fact insert marks its scope dirty; the existing key row already
	// matches it, so clear the mark instead of rebuilding the scope.
	if _, err := db.ExecContext(ctx, `DELETE FROM package_manifest_consumption_key_dirty_scopes`); err != nil {
		t.Fatalf("clear dirty scope mark: %v", err)
	}
	if _, err := store.ReadSupplyChainImpactReadiness(ctx, packageQuery); err == nil ||
		!strings.Contains(err.Error(), "ownership is ambiguous or incomplete") {
		t.Fatalf("ReadSupplyChainImpactReadiness() with a live second owner error = %v, want ambiguous ownership", err)
	}
}
