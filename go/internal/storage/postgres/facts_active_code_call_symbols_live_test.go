// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const activeCodeCallSymbolProofKey = "scip-go gomod github.com/acme/lib Client#Request()."

// TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository proves
// the production loader resolves an active definition from a repository other
// than the caller. Its prefix enrolls it in the blocking real-Postgres reducer
// contention job; the hermetic workflow guard pins that enrollment.
func TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository(t *testing.T) {
	// The content schema carries content_files, which the loader's go.mod
	// manifest read needs; the library repository declares the module the Go
	// proof key belongs to.
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedActiveCodeCallSymbolScope(t, ctx, database, "repository:repo-api", "generation-api", now)
	seedActiveCodeCallSymbolScope(t, ctx, database, "repository:repo-lib", "generation-lib", now)
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:repo-lib", "go.mod", "github.com/acme/lib", now)
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-api-caller", "repository:repo-api", "generation-api", "api.go", "scip-go gomod github.com/acme/api Handler#Serve().", now)
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-lib-active", "repository:repo-lib", "generation-lib", "client.go", activeCodeCallSymbolProofKey, now.Add(time.Second))
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-lib-stale", "repository:repo-lib", "generation-lib-stale", "old_client.go", activeCodeCallSymbolProofKey, now.Add(-time.Second))

	loaded, err := NewFactStore(SQLDB{DB: database}).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{activeCodeCallSymbolProofKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() len = %d, want %d active cross-repository definition", got, want)
	}
	if got, want := loaded[0].FactID, "fact-lib-active"; got != want {
		t.Fatalf("loaded FactID = %q, want %q", got, want)
	}
}

func seedActiveCodeCallSymbolScope(t *testing.T, ctx context.Context, database db.Executor, scopeID, generationID string, observedAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'repository', 'git', $1, 'git', $1, $3, $3, 'active', $2)`, scopeID, generationID, observedAt); err != nil {
		t.Fatalf("insert scope %q: %v", scopeID, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ($1, $2, 'snapshot', $3, $3, 'active', $3)`, generationID, scopeID, observedAt); err != nil {
		t.Fatalf("insert generation %q: %v", generationID, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, $2, 'snapshot', $3, $3, 'superseded')`, generationID+"-stale", scopeID, observedAt.Add(-time.Minute)); err != nil {
		t.Fatalf("insert stale generation for %q: %v", scopeID, err)
	}
}

func seedActiveCodeCallSymbolFact(t *testing.T, ctx context.Context, database db.Executor, factID, scopeID, generationID, relativePath, symbol string, observedAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES (
    $1, $2, $3, 'file', 'file:' || $2 || ':' || $4,
    'git', $4, $6, $6,
    jsonb_build_object(
        'repo_id', $2,
        'relative_path', $4,
        'parsed_file_data', jsonb_build_object(
            'functions', jsonb_build_array(jsonb_build_object('uid', 'uid:' || $1, 'scip_symbol', $5::text))
        )
    )
)`, factID, scopeID, generationID, relativePath, symbol, observedAt); err != nil {
		t.Fatalf("insert fact %q: %v", factID, err)
	}
}

// recordingCodeCallSymbolQueryer records each query the loader issues, with
// its arguments, so a live proof can assert which statements ran against real
// Postgres and which producer scopes the anchored scan was given.
type recordingCodeCallSymbolQueryer struct {
	SQLDB
	queries []string
	args    [][]any
}

func (r *recordingCodeCallSymbolQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	r.queries = append(r.queries, query)
	r.args = append(r.args, args)
	return r.SQLDB.QueryContext(ctx, query, args...)
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderAnchorsPackageKeys proves
// on real Postgres that a package:<package_id>#<export_name> key loads only the
// active file facts of the scopes whose stored manifests publish that package
// (#7601): a duplicate-name pair returns both producers, while a stale
// generation and a non-producer scope carrying the same derived key are
// excluded. Scopes whose manifest names the package but which have no active
// generation are kept out of the producer set passed to the anchored scan.
func TestReducerContentionGateActiveCodeCallSymbolLoaderAnchorsPackageKeys(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()

	// Producer of @acme/logging: a root manifest plus a nested workspace one.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:logging", "repository:r_logging", "generation-logging", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_logging", "package.json", `{"name":"@acme/logging","version":"1.0.0"}`, now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_logging", "packages/format/package.json", `{"name":"@acme/format"}`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-logging-active", "scope:logging", "generation-logging", "src/logger.js", "@acme/logging", "Logger", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-logging-stale", "scope:logging", "generation-logging-stale", "src/old_logger.js", "@acme/logging", "Logger", now.Add(-time.Second))
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-format-active", "scope:logging", "generation-logging", "packages/format/index.js", "@acme/format", "format", now.Add(time.Second))

	// Two repositories publish @acme/shared: both producers must load.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:shared-a", "repository:r_shared_a", "generation-shared-a", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_shared_a", "package.json", `{"name":"@acme/shared"}`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-shared-a", "scope:shared-a", "generation-shared-a", "index.js", "@acme/shared", "Thing", now.Add(2*time.Second))
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:shared-b", "repository:r_shared_b", "generation-shared-b", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_shared_b", "package.json", `{"name":"@acme/shared"}`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-shared-b", "scope:shared-b", "generation-shared-b", "index.js", "@acme/shared", "Thing", now.Add(3*time.Second))

	// Not a producer: the same derived key, but no manifest names the package.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:vendored", "repository:r_vendored", "generation-vendored", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_vendored", "package.json", `{"name":"@acme/app"}`, now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_vendored", "broken/package.json", `{"name": "@acme/logging",`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-vendored", "scope:vendored", "generation-vendored", "vendor/logger.js", "@acme/logging", "Logger", now.Add(4*time.Second))

	// Manifests whose scope has no active generation are not producers: one
	// scope was never activated, the other points at a generation that is
	// still pending. Both carry a pending-generation fact with the same key.
	for _, pending := range []struct{ scopeID, repoID, activeGenerationID string }{
		{"scope:never-active", "repository:r_never_active", ""},
		{"scope:pending-generation", "repository:r_pending_generation", "generation-pending-scope:pending-generation"},
	} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'repository', 'git', $2, 'git', $1, $3, $3, 'pending', NULLIF($4, ''))`,
			pending.scopeID, pending.repoID, now, pending.activeGenerationID); err != nil {
			t.Fatalf("insert pending scope %q: %v", pending.scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('generation-pending-' || $1, $1, 'snapshot', $2, $2, 'pending')`, pending.scopeID, now); err != nil {
			t.Fatalf("insert pending generation for %q: %v", pending.scopeID, err)
		}
		seedActiveCodeCallSymbolManifest(t, ctx, database, pending.repoID, "package.json", `{"name":"@acme/logging"}`, now)
		seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-"+pending.scopeID, pending.scopeID, "generation-pending-"+pending.scopeID, "src/logger.js", "@acme/logging", "Logger", now.Add(5*time.Second))
	}

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{
		"package:@acme/logging#Logger",
		"package:@acme/format#format",
		"package:@acme/shared#Thing",
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	want := []string{"fact-logging-active", "fact-format-active", "fact-shared-a", "fact-shared-b"}
	if got := factIDs(loaded); !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := queryer.queries, []string{
		producerstore.PackageManifestsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d queries, want the manifest read then the anchored scan only", len(got))
	}
	wantScopes := []string{"scope:logging", "scope:shared-a", "scope:shared-b"}
	if got := queryer.args[1][4]; !reflect.DeepEqual(got, wantScopes) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, wantScopes)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanWithoutProducer
// proves a package key that no stored manifest publishes runs only the
// manifest read and never the definition scan, while a key no manifest anchors
// (a scip-java symbol) in the same request still loads through the corpus-wide
// scan.
func TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanWithoutProducer(t *testing.T) {
	const unanchoredKey = "scip-java maven org.acme/lib org.acme/Client#request()."
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedActiveCodeCallSymbolScope(t, ctx, database, "repository:repo-lib", "generation-lib", now)
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-lib-active", "repository:repo-lib", "generation-lib", "client.java", unanchoredKey, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-unpublished", "repository:repo-lib", "generation-lib", "index.js", "@acme/unpublished", "run", now.Add(time.Second))

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{
		unanchoredKey,
		"package:@acme/unpublished#run",
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-lib-active"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := queryer.queries, []string{
		listActiveCodeCallSymbolDefinitionFactsQuery,
		producerstore.PackageManifestsQuery,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d queries, want the corpus-wide scan then the package manifest read only", len(got))
	}
}

// seedVendoredManifestScopes seeds a repository that committed a backup of
// node_modules (manifests under a segment that BEGINS with node_modules, one in
// a case variant) plus two control repositories whose manifests sit under paths
// that only resemble it. The control manifests must stay producers.
func seedVendoredManifestScopes(t *testing.T, ctx context.Context, database db.Executor, now time.Time) {
	t.Helper()
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:backup", "repository:r_backup", "generation-backup", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_backup", "tools/node_modules.bak/lodash/package.json", `{"name":"lodash"}`, now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_backup", "Node_Modules-old/async/package.json", `{"name":"async"}`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-backup-lodash", "scope:backup", "generation-backup", "tools/node_modules.bak/lodash/index.js", "lodash", "map", now.Add(time.Second))
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-backup-async", "scope:backup", "generation-backup", "Node_Modules-old/async/index.js", "async", "each", now.Add(2*time.Second))

	// Controls: a real nested workspace package, and a segment that only
	// resembles node_modules. Each lives in its OWN scope, because the manifest
	// read decides which scopes are scanned: sharing a scope would keep it a
	// producer through the other manifest and hide a predicate that wrongly
	// excludes one of them.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:control", "repository:r_control", "generation-control", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_control", "packages/format/package.json", `{"name":"ctl-format"}`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-control-format", "scope:control", "generation-control", "packages/format/index.js", "ctl-format", "format", now.Add(3*time.Second))
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:control-prefixed", "repository:r_control_prefixed", "generation-control-prefixed", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_control_prefixed", "my_node_modules/x/package.json", `{"name":"ctl-prefixed"}`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-control-prefixed", "scope:control-prefixed", "generation-control-prefixed", "my_node_modules/x/index.js", "ctl-prefixed", "run", now.Add(4*time.Second))
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderIgnoresVendoredManifests
// proves on real Postgres that a package.json under a path segment that begins
// with node_modules (case-insensitive) is not a package producer (#7601): a
// backup copy of node_modules must not anchor a scan or mint a candidate
// scope, while lookalike segments such as my_node_modules and a real nested
// workspace manifest stay producers.
func TestReducerContentionGateActiveCodeCallSymbolLoaderIgnoresVendoredManifests(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedVendoredManifestScopes(t, ctx, database, now)

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{
		"package:lodash#map",
		"package:async#each",
		"package:ctl-prefixed#run",
		"package:ctl-format#format",
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-control-format", "fact-control-prefixed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v (vendored copies must not load, lookalike controls must)", got, want)
	}
	if len(queryer.args) != 2 {
		t.Fatalf("issued %d queries, want the manifest read then the anchored scan", len(queryer.args))
	}
	if got, want := queryer.args[1][4], []string{"scope:control", "scope:control-prefixed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanForVendoredOnlyPackage
// proves a request whose only package is published by a vendored manifest runs
// the manifest read and never the definition scan.
func TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanForVendoredOnlyPackage(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedVendoredManifestScopes(t, ctx, database, now)

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{"package:lodash#map"})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded fact ids = %#v, want none", factIDs(loaded))
	}
	if got, want := queryer.queries, []string{producerstore.PackageManifestsQuery}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d queries, want the manifest read only", len(got))
	}
}

func openActiveCodeCallSymbolContentSchema(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres loader proof")
	}
	ctx := context.Background()
	database, schemaName := openFactCrossBatchFencingSchema(t, ctx, dsn)
	// content_store creates pg_trgm, which lives in public when another schema
	// installed it first, so keep public on the path for its operator classes.
	if _, err := database.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := database.ExecContext(ctx, MigrationSQL("content_store")); err != nil {
		t.Fatalf("apply content_store schema: %v", err)
	}
	return ctx, database
}

func seedActiveCodeCallSymbolRepositoryScope(t *testing.T, ctx context.Context, database db.Executor, scopeID, sourceKey, generationID string, observedAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'repository', 'git', $2, 'git', $1, $4, $4, 'active', $3)`, scopeID, sourceKey, generationID, observedAt); err != nil {
		t.Fatalf("insert scope %q: %v", scopeID, err)
	}
	for _, generation := range []struct {
		id     string
		status string
	}{{generationID, "active"}, {generationID + "-stale", "superseded"}} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, $2, 'snapshot', $3, $3, $4)`, generation.id, scopeID, observedAt, generation.status); err != nil {
			t.Fatalf("insert generation %q: %v", generation.id, err)
		}
	}
}

func seedActiveCodeCallSymbolManifest(t *testing.T, ctx context.Context, database db.Executor, repoID, relativePath, content string, indexedAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (
    repo_id, relative_path, content, content_hash, line_count, language, indexed_at
) VALUES ($1, $2, $3, md5($3), 1, 'json', $4)`, repoID, relativePath, content, indexedAt); err != nil {
		t.Fatalf("insert manifest %s/%s: %v", repoID, relativePath, err)
	}
}

func seedActiveCodeCallSymbolGoMod(t *testing.T, ctx context.Context, database db.Executor, repoID, relativePath, module string, indexedAt time.Time) {
	t.Helper()
	content := "module " + module + "\n\ngo 1.24\n"
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (
    repo_id, relative_path, content, content_hash, line_count, language, indexed_at
) VALUES ($1, $2, $3, md5($3), 3, 'gomod', $4)`, repoID, relativePath, content, indexedAt); err != nil {
		t.Fatalf("insert go.mod %s/%s: %v", repoID, relativePath, err)
	}
}

func seedActiveCodeCallSymbolPackageFact(t *testing.T, ctx context.Context, database db.Executor, factID, scopeID, generationID, relativePath, packageID, exportName string, observedAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES (
    $1, $2, $3, 'file', 'file:' || $2 || ':' || $4,
    'git', $4, $7, $7,
    jsonb_build_object(
        'repo_id', $2,
        'relative_path', $4,
        'parsed_file_data', jsonb_build_object(
            'functions', jsonb_build_array(jsonb_build_object(
                'uid', 'uid:' || $1, 'name', $6::text,
                'package_id', $5::text, 'export_name', $6::text
            ))
        )
    )
)`, factID, scopeID, generationID, relativePath, packageID, exportName, observedAt); err != nil {
		t.Fatalf("insert fact %q: %v", factID, err)
	}
}
