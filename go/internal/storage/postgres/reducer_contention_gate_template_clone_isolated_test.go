// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Isolated-flavour template-clone proofs (#10148 PR-B). The differential
// below is the committed clone-equals-bootstrap proof for the isolated
// flavour, and the concurrency proof beside it covers parallel clones from
// one isolated template plus the SQLSTATE 55006 path. Both ride the gate
// through the TestReducerContentionGate prefix.
const bootstrapTemplateFlavourC = postgresproof.FlavourIsolatedBootstrap

// TestReducerContentionGateIsolatedTemplateCloneMatchesBootstrap is the
// committed differential for the isolated flavour: a clone of the process
// isolated template must be indistinguishable from a fresh real bootstrap
// on every catalog line. The fresh arm builds the flavour the way the
// per-test schemas did — disposable database, fixed schema, pg_trgm in
// public, real ApplyBootstrap with that schema first — using only public
// helpers plus direct SQL, so it stays independent of the template builder
// it is compared against. The disposable bootstrap helper pins
// search_path=public, which would install the full production layout
// instead of the 276 shape, so the isolated flavour needs this own fresh
// arm.
func TestReducerContentionGateIsolatedTemplateCloneMatchesBootstrap(t *testing.T) {
	adminDSN, optIn := templateCloneProofDSN(t)
	ctx := context.Background()
	_, fresh := openIsolatedFreshBootstrapDB(t, adminDSN, optIn, 5*time.Minute)
	template := postgresproof.OpenIsolatedBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	_, clone := postgresproof.OpenIsolatedBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)

	freshFP := mustFingerprint(ctx, t, fresh)
	cloneFP := mustFingerprint(ctx, t, clone)
	equal, detail := bootstrapTemplateCompare(freshFP, cloneFP)
	t.Logf("flavour=%s bootstrap_vs_clone=%t %s", bootstrapTemplateFlavourC, equal, detail)
	if !equal {
		t.Fatalf("flavour %s: clone differs from fresh bootstrap: %s", bootstrapTemplateFlavourC, detail)
	}
}

// openIsolatedFreshBootstrapDB builds the isolated flavour without the
// template: a disposable database, the fixed isolated schema, pg_trgm in
// public, and the real ApplyBootstrap on a pool whose search_path holds
// that schema first, so the public-only skip fires. The fixed name (not a
// per-test name) keeps the fresh arm byte-comparable with the clone.
func openIsolatedFreshBootstrapDB(t *testing.T, adminDSN, optIn string, timeout time.Duration) (context.Context, *sql.DB) {
	t.Helper()
	ctx, public := postgresproof.OpenDisposableDatabase(t, adminDSN, optIn, timeout)
	postgresproof.InstallTrigramExtension(ctx, t, public)
	if _, err := public.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(postgresproof.IsolatedBootstrapSchema)); err != nil {
		t.Fatalf("create isolated fresh schema: %v", err)
	}
	var name string
	if err := public.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("read fresh database name: %v", err)
	}
	dsn, err := postgresproof.WithDatabase(adminDSN, name)
	if err != nil {
		t.Fatalf("derive fresh database DSN: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse fresh database DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", postgresproof.IsolatedBootstrapSchema+",public")
	parsed.RawQuery = query.Encode()
	isolated, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated fresh schema: %v", err)
	}
	t.Cleanup(func() { _ = isolated.Close() })
	if err := isolated.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated fresh schema: %v", err)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: isolated}); err != nil {
		t.Fatalf("apply bootstrap to isolated fresh schema: %v", err)
	}
	return ctx, isolated
}

// TestReducerContentionGateBootstrapTemplateCloneConcurrentIsolated is the
// PR-B concurrency proof for the isolated flavour: parallel clones from one
// isolated template all match the template fingerprint, and cloning while a
// session stays on the template fails with SQLSTATE 55006 after the shared
// bounded retry instead of a half-built database.
func TestReducerContentionGateBootstrapTemplateCloneConcurrentIsolated(t *testing.T) {
	adminDSN, optIn := templateCloneProofDSN(t)
	ctx := context.Background()
	template := postgresproof.OpenIsolatedBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	_, wantDB := postgresproof.OpenIsolatedBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)
	want := mustFingerprint(ctx, t, wantDB)

	const clones = 4
	var wg sync.WaitGroup
	errs := make([]error, clones)
	cleanups := make([]func(), clones)
	for i := range clones {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// The error-returning clone keeps t.Fatalf off the worker
			// goroutine; the main goroutine reports and drops the clones.
			db, cleanup, err := postgresproof.CloneIsolatedBootstrapTemplate(ctx, adminDSN, optIn, template)
			if err != nil {
				errs[i] = fmt.Errorf("parallel isolated clone %d: %w", i, err)
				return
			}
			cleanups[i] = cleanup
			got, ferr := bootstrapTemplateFingerprint(ctx, db)
			if ferr != nil {
				errs[i] = fmt.Errorf("parallel isolated clone %d fingerprint: %w", i, ferr)
				return
			}
			if equal, detail := bootstrapTemplateCompare(want, got); !equal {
				errs[i] = fmt.Errorf("parallel isolated clone %d differs from template: %s", i, detail)
			}
		}(i)
	}
	wg.Wait()
	for _, cleanup := range cleanups {
		if cleanup != nil {
			defer cleanup()
		}
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("isolated clone %d: %v", i, err)
		}
	}

	// A session parked on the isolated template must refuse the clone with
	// 55006 after the bounded retry.
	holderDSN, err := postgresproof.WithDatabase(adminDSN, template)
	if err != nil {
		t.Fatalf("derive isolated template session DSN: %v", err)
	}
	holder, err := sql.Open("pgx", holderDSN)
	if err != nil {
		t.Fatalf("open isolated template session: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if err := holder.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated template session: %v", err)
	}
	_, _, err = postgresproof.CloneIsolatedBootstrapTemplate(ctx, adminDSN, optIn, template)
	if err == nil {
		t.Fatal("isolated clone with a session on the template succeeded, want SQLSTATE 55006")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55006" {
		t.Fatalf("isolated clone with a session on the template: error = %v, want SQLSTATE 55006", err)
	}
}
