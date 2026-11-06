// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// Bootstrap template clones (#10148 PR-A). A template is one disposable
// database per (proof server, flavour) per test process, built by the real
// apply from template0 exactly once and cloned by every opted-in caller, so
// the gate pays one bootstrap per flavour instead of one per test. The
// template is process-scoped: it is built in the same test process that
// clones it and never reused across processes, so a migration change cannot
// meet a stale template. DiscardBootstrapTemplates drops every template the
// process built; the owning test package calls it from TestMain, after m.Run,
// because no per-test cleanup may drop a template other tests still clone.
//
// A flavour pins one apply shape (for example the plain production bootstrap
// versus the content-search-index-deferred one). Distinct apply shapes need
// distinct flavours: the cache cannot tell two closures apart, and sharing
// one template across shapes would clone the wrong content. Flavours are
// validated to a short safe charset because they become part of the template
// database name.

// bootstrapTemplateNamePrefix keeps template databases inside the generated
// proof namespace so cleanup only ever touches names this package generated.
const bootstrapTemplateNamePrefix = "eshu_content_index_proof_tpl_"

// Well-known template flavours shared by internal and external test
// packages. A flavour pins one apply shape: FlavourPublicBootstrap is the
// plain production bootstrap, FlavourPublicWithoutContentSearchIndexes
// defers the content search indexes for a later bulk build. The values stay
// inside the template-name budget; the apply shape each value pins is
// documented on the owning callers, not here.
const (
	FlavourPublicBootstrap                   = "public"
	FlavourPublicWithoutContentSearchIndexes = "public_nocsearch"
)

var bootstrapTemplateFlavourPattern = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)

// bootstrapTemplateCache maps template key to template database name. The
// build holds the mutex for its whole duration, so two parallel first uses
// build once and concurrent flavour builds serialize behind one bootstrap
// each; lookups never run while a build mutates the map.
var bootstrapTemplateCache = struct {
	sync.Mutex
	byKey map[string]string
}{byKey: map[string]string{}}

// bootstrapTemplateKey scopes a template to one proof server and one flavour.
// The admin DSN is the server identity: two families on different servers
// must not share a template even under the same flavour name.
func bootstrapTemplateKey(adminDSN, flavour string) string {
	return adminDSN + "\x00" + flavour
}

func validateBootstrapTemplateFlavour(flavour string) error {
	if !bootstrapTemplateFlavourPattern.MatchString(flavour) {
		return fmt.Errorf("bootstrap template flavour must match %s, got %q", bootstrapTemplateFlavourPattern, flavour)
	}
	return nil
}

func bootstrapTemplateName(flavour string) (string, error) {
	if err := validateBootstrapTemplateFlavour(flavour); err != nil {
		return "", err
	}
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate bootstrap template name: %w", err)
	}
	return bootstrapTemplateNamePrefix + flavour + "_" + hex.EncodeToString(random), nil
}

// validateBootstrapTemplateName refuses any template outside the generated
// namespace: clone and discard cleanup only drops names this package
// generated, and the same check keeps a caller from cloning an arbitrary
// database through this helper.
func validateBootstrapTemplateName(template string) error {
	if !strings.HasPrefix(template, bootstrapTemplateNamePrefix) || len(template) > 63 {
		return fmt.Errorf("refuse bootstrap template outside the generated namespace: %q", template)
	}
	rest := strings.TrimPrefix(template, bootstrapTemplateNamePrefix)
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(rest) {
		return fmt.Errorf("refuse bootstrap template with unsafe name: %q", template)
	}
	return nil
}

// OpenBootstrapTemplate returns the process template for (adminDSN, flavour),
// building it once with apply when the first caller arrives. An empty DSN
// skips like OpenDisposableDatabase does; any other validation failure fails
// the test before any database is created.
func OpenBootstrapTemplate(
	t testing.TB,
	adminDSN, optIn string,
	timeout time.Duration,
	flavour string,
	apply func(context.Context, *sql.DB) error,
) string {
	t.Helper()
	if strings.TrimSpace(adminDSN) == "" {
		t.Skip(missingAdminDSNSkipMessage())
	}
	if err := validateBootstrapTemplateFlavour(flavour); err != nil {
		t.Fatalf("refuse bootstrap template: %v", err)
	}
	config, err := validateAdminDSN(adminDSN, optIn)
	if err != nil {
		t.Fatalf("refuse destructive PostgreSQL proof target: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	bootstrapTemplateCache.Lock()
	defer bootstrapTemplateCache.Unlock()
	key := bootstrapTemplateKey(config.ConnString(), flavour)
	if name, ok := bootstrapTemplateCache.byKey[key]; ok {
		return name
	}
	name, err := createBootstrapTemplate(ctx, config, flavour, apply)
	if err != nil {
		t.Fatalf("build bootstrap template: %v", err)
	}
	bootstrapTemplateCache.byKey[key] = name
	return name
}

// createBootstrapTemplate creates one template database from template0,
// applies the caller's apply on a public-search_path pool like
// OpenDisposableBootstrapDatabase, then closes the pool and waits until no
// session stays on the template: CREATE DATABASE ... TEMPLATE refuses a
// template with connected sessions (SQLSTATE 55006). An apply failure drops
// the half-built template before returning, so a later caller rebuilds
// instead of cloning a broken template.
func createBootstrapTemplate(
	ctx context.Context,
	config *pgx.ConnConfig,
	flavour string,
	apply func(context.Context, *sql.DB) error,
) (string, error) {
	name, err := bootstrapTemplateName(flavour)
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
		return "", fmt.Errorf("create bootstrap template database: %w", err)
	}
	drop := func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = terminateAndDropDisposableDatabase(dropCtx, adminDB, name)
	}
	targetConfig := config.Copy()
	targetConfig.Database = name
	targetConfig.RuntimeParams = cloneRuntimeParams(config.RuntimeParams)
	targetConfig.RuntimeParams["search_path"] = "public"
	targetDB := stdlib.OpenDB(*targetConfig, inventory.WriterConnectOption())
	if err := targetDB.PingContext(ctx); err != nil {
		_ = targetDB.Close()
		drop()
		return "", fmt.Errorf("ping bootstrap template database: %w", err)
	}
	if err := apply(ctx, targetDB); err != nil {
		_ = targetDB.Close()
		drop()
		return "", fmt.Errorf("apply schema to bootstrap template database: %w", err)
	}
	if err := targetDB.Close(); err != nil {
		drop()
		return "", fmt.Errorf("close bootstrap template build pool: %w", err)
	}
	if err := waitForTemplateIdle(ctx, adminDB, name); err != nil {
		drop()
		return "", err
	}
	return name, nil
}

// waitForTemplateIdle polls pg_stat_activity until no session stays on the
// template. The build pool is already closed; the wait covers backends that
// are slow to exit on a loaded runner, the same window the disposable drop
// retry closes on teardown.
func waitForTemplateIdle(ctx context.Context, adminDB *sql.DB, template string) error {
	for {
		var sessions int
		if err := adminDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = $1`, template).Scan(&sessions); err != nil {
			return fmt.Errorf("poll template sessions: %w", err)
		}
		if sessions == 0 {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("template %q still has %d sessions: %w", template, sessions, ctx.Err())
		case <-timer.C:
		}
	}
}

// CloneBootstrapTemplate clones template into a fresh disposable database
// and returns it with its cleanup: closing the pool and force-dropping only
// the generated clone. It reports errors instead of failing the test so
// parallel clone workers can report through their own channel. The opt-in,
// administrative-DSN, and template-namespace checks match the TB wrapper:
// destructive DDL never runs on an unvalidated target. The returned cleanup
// has no error channel, so a failing clone drop is silent there by
// construction; OpenBootstrapTemplateClone wraps this helper with a
// pg_database probe that reports a leaked clone loudly, and callers that
// need a loud clone teardown otherwise should drop through
// DiscardBootstrapTemplates.
func CloneBootstrapTemplate(
	ctx context.Context,
	adminDSN, optIn, template string,
) (*sql.DB, func(), error) {
	db, _, cleanup, err := cloneBootstrapTemplate(ctx, adminDSN, optIn, template)
	return db, cleanup, err
}

// cloneBootstrapTemplate is CloneBootstrapTemplate plus the generated clone
// name, so the TB wrapper can probe pg_database for the clone after the
// cleanup ran. The exported signature stays error-returning for goroutine
// use; only the TB path verifies the drop.
func cloneBootstrapTemplate(
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
		return nil, "", nil, fmt.Errorf("clone bootstrap template database: %w", err)
	}
	targetConfig := config.Copy()
	targetConfig.Database = clone
	targetConfig.RuntimeParams = cloneRuntimeParams(config.RuntimeParams)
	targetConfig.RuntimeParams["search_path"] = "public"
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

// cloneDropTB is the minimal testing surface the clone-drop verification
// needs: Errorf reports a leaked clone without stopping teardown. Every
// testing.TB satisfies it; unit tests script it.
type cloneDropTB interface {
	Helper()
	Errorf(format string, args ...any)
}

// cloneExistenceProber answers whether a database still exists on the proof
// server. Tests script it; the live path probes pg_database.
type cloneExistenceProber interface {
	CloneExists(ctx context.Context, clone string) (bool, error)
}

// sqlCloneExistenceProber answers CloneExists from pg_database on a live
// administrative handle.
type sqlCloneExistenceProber struct {
	db *sql.DB
}

// newCloneExistenceProber builds the live pg_database prober for a fresh
// administrative handle. It is a variable so hermetic tests can script the
// probe without a server; production always uses the live probe.
var newCloneExistenceProber = func(adminDB *sql.DB) cloneExistenceProber {
	return sqlCloneExistenceProber{db: adminDB}
}

func (p sqlCloneExistenceProber) CloneExists(ctx context.Context, clone string) (bool, error) {
	var exists bool
	if err := p.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, clone).Scan(&exists); err != nil {
		return false, fmt.Errorf("probe pg_database for clone %q: %w", clone, err)
	}
	return exists, nil
}

// assertCloneDropped probes for the clone after the clone cleanup ran and
// reports loudly when the clone remains: a failing clone drop must fail the
// test like DiscardBootstrapTemplates does instead of leaking the clone on
// the shared proof server silently. A probe failure reports too: an
// unverified drop is not a verified one.
func assertCloneDropped(t cloneDropTB, ctx context.Context, prober cloneExistenceProber, clone string) {
	t.Helper()
	exists, err := prober.CloneExists(ctx, clone)
	if err != nil {
		t.Errorf("probe pg_database for dropped clone %q: %v", clone, err)
		return
	}
	if exists {
		t.Errorf("clone database %q still exists after cleanup: the clone drop failed", clone)
	}
}

// verifyCloneDropped opens a fresh administrative handle and fails the test
// when the clone cleanup left the database behind. The handle is fresh
// because the cleanup already closed its own.
func verifyCloneDropped(t testing.TB, adminDSN, optIn, clone string) {
	t.Helper()
	config, err := validateAdminDSN(adminDSN, optIn)
	if err != nil {
		t.Errorf("reopen PostgreSQL administrative database to verify clone drop: %v", err)
		return
	}
	adminDB, err := sql.Open("pgx", config.ConnString())
	if err != nil {
		t.Errorf("open PostgreSQL administrative database to verify clone drop: %v", err)
		return
	}
	defer func() { _ = adminDB.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	assertCloneDropped(t, ctx, newCloneExistenceProber(adminDB), clone)
}

// registerCloneTeardown registers the clone drop verification before the
// clone cleanup so t.Cleanup LIFO runs the verification after the drop,
// reporting a leaked clone loudly like DiscardBootstrapTemplates does.
func registerCloneTeardown(t testing.TB, verify, cleanup func()) {
	t.Helper()
	t.Cleanup(verify)
	t.Cleanup(cleanup)
}

// cloneBootstrapTemplateDatabase issues one CREATE DATABASE ... TEMPLATE,
// retrying a bounded number of times on SQLSTATE 55006. waitForTemplateIdle
// proves zero sessions at build time, but a later transient backend on the
// template (an autovacuum worker on a just-bootstrapped database is the
// realistic one) can still refuse the clone with "database is being accessed
// by other users". That is the mirror image of the disposable drop race, so
// the clone reuses its bound, its delay, and its SQLSTATE classifier: a
// genuinely parked session still fails loudly after the bound, and any other
// CREATE failure returns immediately.
func cloneBootstrapTemplateDatabase(ctx context.Context, admin disposableDropAdmin, clone, template string) error {
	var lastErr error
	for attempt := 1; attempt <= disposableDropMaxAttempts; attempt++ {
		if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(clone)+" TEMPLATE "+quoteIdentifier(template)); err != nil {
			lastErr = err
			if disposableDropSQLState(err) != "55006" {
				return fmt.Errorf("clone bootstrap template database %q from %q: %w", clone, template, err)
			}
		} else {
			return nil
		}
		if attempt < disposableDropMaxAttempts {
			timer := time.NewTimer(disposableDropRetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return lastErr
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("clone bootstrap template database %q from %q after %d attempts: %w", clone, template, disposableDropMaxAttempts, lastErr)
}

// OpenBootstrapTemplateClone clones template and returns the open handle,
// registering the close and the force-drop on the test's cleanup. An absent
// DSN skips like the other disposable openers.
func OpenBootstrapTemplateClone(
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
	db, clone, cleanup, err := cloneBootstrapTemplate(ctx, adminDSN, optIn, template)
	if err != nil {
		t.Fatalf("clone bootstrap template database: %v", err)
	}
	// t.Cleanup runs LIFO: registerCloneTeardown registers the drop
	// verification first so it runs after the clone cleanup and reports a
	// leaked clone loudly, matching the DiscardBootstrapTemplates contract.
	registerCloneTeardown(t, func() { verifyCloneDropped(t, adminDSN, optIn, clone) }, cleanup)
	return ctx, db
}

// dropDisposableDatabaseName drops one generated database without a *testing.T,
// for error-returning paths that cannot fail the test. The error returns to
// the caller: DiscardBootstrapTemplates appends it, wrapping the template
// name, so TestMain can fail the process instead of leaking the template
// silently.
func dropDisposableDatabaseName(ctx context.Context, adminDB disposableDropAdmin, databaseName string) error {
	dropCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return terminateAndDropDisposableDatabase(dropCtx, adminDB, databaseName)
}

// DiscardBootstrapTemplates force-drops every template this process built
// and clears the cache. It has no testing.TB: the owning test package calls
// it from TestMain after m.Run and maps a non-empty return to a failing exit
// code. Templates are keyed by server DSN, so each entry revalidates its own
// administrative session before dropping.
func DiscardBootstrapTemplates() []error {
	bootstrapTemplateCache.Lock()
	names := make([]string, 0, len(bootstrapTemplateCache.byKey))
	dsns := map[string]string{}
	for key, name := range bootstrapTemplateCache.byKey {
		names = append(names, name)
		if dsn, _, found := strings.Cut(key, "\x00"); found {
			dsns[name] = dsn
		}
	}
	bootstrapTemplateCache.byKey = map[string]string{}
	bootstrapTemplateCache.Unlock()
	var errs []error
	for _, name := range names {
		dsn := dsns[name]
		config, err := pgx.ParseConfig(dsn)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse administrative DSN for template %q: %w", name, err))
			continue
		}
		adminDB, err := sql.Open("pgx", config.ConnString())
		if err != nil {
			errs = append(errs, fmt.Errorf("open administrative database for template %q: %w", name, err))
			continue
		}
		if err := dropDisposableDatabaseName(context.Background(), adminDB, name); err != nil {
			errs = append(errs, fmt.Errorf("drop bootstrap template %q: %w", name, err))
		}
		_ = adminDB.Close()
	}
	return errs
}
