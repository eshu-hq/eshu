// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// Isolated bootstrap template flavour (#10148 PR-B). The isolated flavour
// holds the 276-shape layout: the production bootstrap applied with a
// fixed-name schema first on the search_path and pg_trgm installed in
// public, so the public-only skip fires and every table lands in the fixed
// schema. Per-test clones of this template replace the hand-built per-test
// schemas the isolated openers used to bootstrap in the shared database:
// CREATE DATABASE ... TEMPLATE cannot copy one schema into an existing
// database, so the isolation unit becomes the cloned database and the fixed
// schema name stays identical across clones. The apply shape is pinned like
// the public flavours: a caller whose apply differs must not use this
// flavour. It lives in this file so the PR-A flavour block in
// template_clone.go stays untouched.
const FlavourIsolatedBootstrap = "isolated"

// IsolatedBootstrapSchema is the fixed schema holding the isolated flavour
// in the template and in every clone. One name for every test keeps the
// template build, every clone, and the fresh-bootstrap arm of the committed
// differential identical without normalizing a per-test name.
const IsolatedBootstrapSchema = "eshu_isolated_proof"

// isolatedBootstrapSearchPath resolves unqualified names in the fixed
// isolated schema first with public behind it, so pg_trgm operator classes
// installed in public keep resolving for definitions the template creates
// in the fixed schema.
func isolatedBootstrapSearchPath() string {
	return IsolatedBootstrapSchema + ",public"
}

// OpenIsolatedBootstrapTemplate returns the process template for
// (adminDSN, FlavourIsolatedBootstrap), building it once with apply when the
// first caller arrives. The build installs pg_trgm into public, creates the
// fixed isolated schema, and runs apply with that schema first on the
// search_path, so the public-only skip fires exactly like the per-test
// schemas this template replaces. Skip and validation semantics match
// OpenBootstrapTemplate.
func OpenIsolatedBootstrapTemplate(
	t testing.TB,
	adminDSN, optIn string,
	timeout time.Duration,
	apply func(context.Context, *sql.DB) error,
) string {
	t.Helper()
	if strings.TrimSpace(adminDSN) == "" {
		t.Skip(missingAdminDSNSkipMessage())
	}
	config, err := validateAdminDSN(adminDSN, optIn)
	if err != nil {
		t.Fatalf("refuse destructive PostgreSQL proof target: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	bootstrapTemplateCache.Lock()
	defer bootstrapTemplateCache.Unlock()
	key := bootstrapTemplateKey(config.ConnString(), FlavourIsolatedBootstrap)
	if name, ok := bootstrapTemplateCache.byKey[key]; ok {
		return name
	}
	name, err := createIsolatedBootstrapTemplate(ctx, t, config, apply)
	if err != nil {
		t.Fatalf("build isolated bootstrap template: %v", err)
	}
	bootstrapTemplateCache.byKey[key] = name
	return name
}

// createIsolatedBootstrapTemplate creates one isolated-flavour template
// database from template0, installs pg_trgm into public, creates the fixed
// isolated schema, and applies the caller's apply on a pool whose
// search_path holds that schema first. An apply failure drops the half-built
// template before returning, so a later caller rebuilds instead of cloning
// a broken template.
func createIsolatedBootstrapTemplate(
	ctx context.Context,
	t testing.TB,
	config *pgx.ConnConfig,
	apply func(context.Context, *sql.DB) error,
) (string, error) {
	t.Helper()
	name, err := bootstrapTemplateName(FlavourIsolatedBootstrap)
	if err != nil {
		return "", err
	}
	adminDB, err := sql.Open("pgx", config.ConnString())
	if err != nil {
		return "", fmt.Errorf("open PostgreSQL administrative database: %w", err)
	}
	defer func() { _ = adminDB.Close() }()
	if err := adminDB.PingContext(ctx); err != nil {
		return "", fmt.Errorf("ping PostgreSQL administrative database: %w", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(name)+" TEMPLATE template0"); err != nil {
		return "", fmt.Errorf("create isolated bootstrap template database: %w", err)
	}
	drop := func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = terminateAndDropDisposableDatabase(dropCtx, adminDB, name)
	}
	targetConfig := config.Copy()
	targetConfig.Database = name
	targetConfig.RuntimeParams = cloneRuntimeParams(config.RuntimeParams)
	targetConfig.RuntimeParams["search_path"] = isolatedBootstrapSearchPath()
	targetDB := stdlib.OpenDB(*targetConfig, inventory.WriterConnectOption())
	if err := targetDB.PingContext(ctx); err != nil {
		_ = targetDB.Close()
		drop()
		return "", fmt.Errorf("ping isolated bootstrap template database: %w", err)
	}
	if _, err := targetDB.ExecContext(ctx, "CREATE SCHEMA "+quoteIdentifier(IsolatedBootstrapSchema)); err != nil {
		_ = targetDB.Close()
		drop()
		return "", fmt.Errorf("create isolated bootstrap schema: %w", err)
	}
	InstallTrigramExtension(ctx, t, targetDB)
	if err := apply(ctx, targetDB); err != nil {
		_ = targetDB.Close()
		drop()
		return "", fmt.Errorf("apply schema to isolated bootstrap template database: %w", err)
	}
	if err := targetDB.Close(); err != nil {
		drop()
		return "", fmt.Errorf("close isolated bootstrap template build pool: %w", err)
	}
	if err := waitForTemplateIdle(ctx, adminDB, name); err != nil {
		drop()
		return "", err
	}
	return name, nil
}

// CloneIsolatedBootstrapTemplate clones an isolated-flavour template into a
// fresh disposable database and returns it with its cleanup: closing the
// pool and force-dropping only the generated clone. The clone's connections
// resolve unqualified names in the fixed isolated schema first, matching the
// search_path the per-test schemas this template replaces used. It reports
// errors instead of failing the test so parallel clone workers can report
// through their own channel. The opt-in, administrative-DSN, and
// template-namespace checks match CloneBootstrapTemplate: destructive DDL
// never runs on an unvalidated target.
func CloneIsolatedBootstrapTemplate(
	ctx context.Context,
	adminDSN, optIn, template string,
) (*sql.DB, func(), error) {
	db, _, cleanup, err := cloneIsolatedBootstrapTemplate(ctx, adminDSN, optIn, template)
	return db, cleanup, err
}

// cloneIsolatedBootstrapTemplate is CloneIsolatedBootstrapTemplate plus the
// generated clone name, so the TB wrapper can probe pg_database for the
// clone after the cleanup ran. It mirrors cloneBootstrapTemplate with the
// isolated search_path and is kept separate because that function is owned
// by a parallel change; sharing the 55006 retry classifier and the drop
// helpers keeps their behavior identical.
func cloneIsolatedBootstrapTemplate(
	ctx context.Context,
	adminDSN, optIn, template string,
) (*sql.DB, string, func(), error) {
	config, err := validateAdminDSN(adminDSN, optIn)
	if err != nil {
		return nil, "", nil, fmt.Errorf("refuse destructive PostgreSQL proof target: %w", err)
	}
	if err := validateBootstrapTemplateName(template); err != nil {
		return nil, "", nil, err
	}
	adminDB, err := sql.Open("pgx", config.ConnString())
	if err != nil {
		return nil, "", nil, fmt.Errorf("open PostgreSQL administrative database: %w", err)
	}
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		return nil, "", nil, fmt.Errorf("ping PostgreSQL administrative database: %w", err)
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		_ = adminDB.Close()
		return nil, "", nil, fmt.Errorf("generate disposable PostgreSQL database name: %w", err)
	}
	clone := "eshu_content_index_proof_" + hex.EncodeToString(random)
	if err := cloneBootstrapTemplateDatabase(ctx, adminDB, clone, template); err != nil {
		_ = adminDB.Close()
		return nil, "", nil, fmt.Errorf("clone isolated bootstrap template database: %w", err)
	}
	targetConfig := config.Copy()
	targetConfig.Database = clone
	targetConfig.RuntimeParams = cloneRuntimeParams(config.RuntimeParams)
	targetConfig.RuntimeParams["search_path"] = isolatedBootstrapSearchPath()
	targetDB := stdlib.OpenDB(*targetConfig, inventory.WriterConnectOption())
	if err := targetDB.PingContext(ctx); err != nil {
		_ = targetDB.Close()
		_ = dropDisposableDatabaseName(ctx, adminDB, clone)
		_ = adminDB.Close()
		return nil, "", nil, fmt.Errorf("ping cloned PostgreSQL database: %w", err)
	}
	cleanup := func() {
		_ = targetDB.Close()
		_ = dropDisposableDatabaseName(context.Background(), adminDB, clone)
		_ = adminDB.Close()
	}
	return targetDB, clone, cleanup, nil
}

// OpenIsolatedBootstrapTemplateClone clones the isolated-flavour template
// and returns the open handle, registering the close and the force-drop on
// the test's cleanup. An absent DSN skips like the other disposable openers.
func OpenIsolatedBootstrapTemplateClone(
	t testing.TB,
	adminDSN, optIn string,
	timeout time.Duration,
	template string,
) (context.Context, *sql.DB) {
	t.Helper()
	if strings.TrimSpace(adminDSN) == "" {
		t.Skip(missingAdminDSNSkipMessage())
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	db, clone, cleanup, err := cloneIsolatedBootstrapTemplate(ctx, adminDSN, optIn, template)
	if err != nil {
		t.Fatalf("clone isolated bootstrap template database: %v", err)
	}
	// t.Cleanup runs LIFO: register the drop verification first so it runs
	// after the clone cleanup and reports a leaked clone loudly, matching
	// the DiscardBootstrapTemplates contract.
	t.Cleanup(func() { verifyCloneDropped(t, adminDSN, optIn, clone) })
	t.Cleanup(cleanup)
	return ctx, db
}
