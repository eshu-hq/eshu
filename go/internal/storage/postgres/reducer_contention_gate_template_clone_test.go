// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestMain runs the package's tests, then drops every bootstrap template the
// template-clone helper built. Templates are process-scoped (#10148 PR-A): a
// test that clones never owns the template, so no per-test cleanup can drop
// it, and a leaked template would outlive the test process on the shared
// proof server. Discarding here keeps the drop off the measured clone path.
func TestMain(m *testing.M) {
	code := m.Run()
	if errs := postgresproof.DiscardBootstrapTemplates(); len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "discard bootstrap template: %v\n", err)
		}
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// Public-bootstrap flavours that may clone instead of bootstrapping (#10148
// PR-A). A flavour pins one apply shape: flavour A is the plain production
// bootstrap, flavour B defers the content search indexes. A caller whose
// apply differs (historical definitions, subset DDL, bespoke seeds) must not
// use either flavour; it stays on the direct bootstrap path.
// Pinned flavours under test, shared with converted callers through
// postgresproof so internal and external test packages use one key each.
const (
	bootstrapTemplateFlavourA = postgresproof.FlavourPublicBootstrap
	bootstrapTemplateFlavourB = postgresproof.FlavourPublicWithoutContentSearchIndexes
)

// openDisposablePublicBootstrapCloneDB is openDisposablePublicBootstrapDB for
// callers whose apply is exactly one of the pinned flavours: it clones the
// process template for that flavour instead of applying the bootstrap again.
// Skip semantics match the wrapped helper: an empty dsn skips with
// skipMessage, and an opt-in other than "1" skips without failing.
//
// The returned context carries timeout like OpenDisposableBootstrapDatabase's:
// callers keep their deadline behavior unchanged when they switch.
func openDisposablePublicBootstrapCloneDB(
	t *testing.T,
	dsn, optIn, skipMessage string,
	flavour string,
	timeout time.Duration,
	apply func(context.Context, *sql.DB) error,
) (context.Context, *sql.DB) {
	t.Helper()
	if dsn == "" {
		t.Skip(skipMessage)
	}
	if optIn != "1" {
		t.Skip("set the family disposable opt-in to 1 to run this proof on a disposable PostgreSQL database (absent opt-in skips, never fails)")
	}
	template := postgresproof.OpenBootstrapTemplate(t, dsn, optIn, timeout, flavour, apply)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	db, cleanup, err := postgresproof.CloneBootstrapTemplate(ctx, dsn, optIn, template)
	if err != nil {
		t.Fatalf("clone bootstrap template database: %v", err)
	}
	t.Cleanup(cleanup)
	return ctx, db
}

// templateCloneProofDSN resolves the administrative session for the
// template-clone differential proofs from ESHU_POSTGRES_TEST_DSN (the gate
// step's main pair), skipping when the DSN is unset and skipping (never
// failing) when the opt-in is absent.
func templateCloneProofDSN(t *testing.T) (adminDSN, optIn string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the template-clone differential proof")
	}
	optIn = os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if optIn != "1" {
		t.Skip("set ESHU_POSTGRES_DISPOSABLE=1 to run the template-clone differential proof (absent opt-in skips, never fails)")
	}
	return disposableAdminDSN(t, dsn), optIn
}

// busyProofSuffix generates the random suffix for the 55006 busy-proof
// database name, mirroring disposableDatabaseName: every other database this
// proof creates uses a crypto-random suffix so reruns and shared proof
// servers cannot collide.
func busyProofSuffix(t *testing.T) string {
	t.Helper()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate busy-proof database name: %v", err)
	}
	return hex.EncodeToString(random)
}

// bootstrapTemplateCatalogSQL lists one text line per catalog fact, tagged by
// category: relations, columns, indexes, constraints, functions, triggers,
// policies, schemas, extensions, types, sequences, default ACLs, database
// settings, the migration ledger, and exact row counts of every table. It is
// the committed differential for #10148 PR-A: a clone must be
// indistinguishable from a fresh real bootstrap on every line.
const bootstrapTemplateCatalogSQL = `SELECT cat, line FROM (
SELECT 'relation' AS cat, format('%s.%s kind=%s persist=%s rls=%s/%s opts=%s acl=%s owner=%s', n.nspname, c.relname, c.relkind, c.relpersistence, c.relrowsecurity, c.relforcerowsecurity, coalesce(c.reloptions::text,''), coalesce(c.relacl::text,''), pg_get_userbyid(c.relowner)) AS line
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'column', format('%s.%s.%s #%s %s notnull=%s default=%s ident=%s gen=%s coll=%s acl=%s opts=%s storage=%s', n.nspname, c.relname, a.attname, a.attnum, format_type(a.atttypid,a.atttypmod), a.attnotnull, coalesce(pg_get_expr(d.adbin,d.adrelid),''), a.attidentity, a.attgenerated, a.attcollation::regcollation, coalesce(a.attacl::text,''), coalesce(a.attoptions::text,''), a.attstorage)
  FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
  WHERE a.attnum>0 AND NOT a.attisdropped AND n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'index', format('%s valid=%s ready=%s live=%s opts=%s', pg_get_indexdef(i.indexrelid), i.indisvalid, i.indisready, i.indislive, coalesce(ic.reloptions::text,''))
  FROM pg_index i JOIN pg_class ic ON ic.oid=i.indexrelid JOIN pg_namespace n ON n.oid=ic.relnamespace WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'constraint', format('%s %s %s %s validated=%s', n.nspname, c.conrelid::regclass::text, c.conname, pg_get_constraintdef(c.oid), c.convalidated)
  FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'function', format('%s.%s(%s) md5=%s secdef=%s lang=%s config=%s acl=%s owner=%s vol=%s', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid), md5(p.prosrc), p.prosecdef, l.lanname, coalesce(p.proconfig::text,''), coalesce(p.proacl::text,''), pg_get_userbyid(p.proowner), p.provolatile)
  FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'trigger', format('%s enabled=%s', pg_get_triggerdef(t.oid), t.tgenabled) FROM pg_trigger t WHERE NOT t.tgisinternal
UNION ALL SELECT 'policy', format('%s.%s %s cmd=%s permissive=%s roles=%s qual=%s check=%s', schemaname, tablename, policyname, cmd, permissive, roles::text, coalesce(qual,''), coalesce(with_check,'')) FROM pg_policies WHERE schemaname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'schema', format('%s acl=%s owner=%s', nspname, coalesce(nspacl::text,''), pg_get_userbyid(nspowner)) FROM pg_namespace WHERE nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'extension', format('%s %s schema=%s', extname, extversion, extnamespace::regnamespace) FROM pg_extension
UNION ALL SELECT 'type', format('%s.%s %s %s', n.nspname, t.typname, t.typtype, coalesce((SELECT string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder) FROM pg_enum e WHERE e.enumtypid=t.oid),''))
  FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE t.typtype IN ('e','d') AND n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'sequence', format('%s.%s %s start=%s inc=%s min=%s max=%s cycle=%s cache=%s', schemaname, sequencename, data_type, start_value, increment_by, min_value, max_value, cycle, cache_size) FROM pg_sequences WHERE schemaname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'default_acl', format('%s %s %s %s', pg_get_userbyid(defaclrole), defaclnamespace::regnamespace, defaclobjtype, defaclacl) FROM pg_default_acl
UNION ALL SELECT 'database', format('enc=%s collate=%s ctype=%s connlimit=%s allowconn=%s acl=%s owner=%s template=%s', pg_encoding_to_char(encoding), datcollate, datctype, datconnlimit, datallowconn, coalesce(datacl::text,''), pg_get_userbyid(datdba), datistemplate) FROM pg_database WHERE datname=current_database()
UNION ALL SELECT 'db_settings', format('%s %s', setrole, setconfig) FROM pg_db_role_setting WHERE setdatabase=(SELECT oid FROM pg_database WHERE datname=current_database())
UNION ALL SELECT 'ledger', format('%s %s %s', path, checksum_sha256, variant) FROM eshu_schema_migrations
UNION ALL SELECT 'rowcount', format('%s.%s=%s', n.nspname, c.relname, (xpath('/row/c/text()', query_to_xml(format('select count(*) as c from %I.%I', n.nspname, c.relname), false, true, '')))[1]::text)
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname !~ '^(pg_|information_schema)'
) q ORDER BY 1, 2`

// bootstrapTemplateFingerprint returns the catalog lines grouped by
// category. It reports errors instead of failing the test so parallel clone
// workers can fingerprint off the test goroutine.
func bootstrapTemplateFingerprint(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	rows, err := db.QueryContext(ctx, bootstrapTemplateCatalogSQL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var cat, line string
		if err := rows.Scan(&cat, &line); err != nil {
			return nil, err
		}
		out[cat] = append(out[cat], line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func mustFingerprint(ctx context.Context, t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	out, err := bootstrapTemplateFingerprint(ctx, db)
	if err != nil {
		t.Fatalf("fingerprint catalog: %v", err)
	}
	return out
}

func bootstrapTemplateDigest(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:4])
}

// bootstrapTemplateCompare reports whether two fingerprints are identical
// and, per category, a count and short digest plus the first differing lines.
func bootstrapTemplateCompare(a, b map[string][]string) (bool, string) {
	cats := map[string]bool{}
	for c := range a {
		cats[c] = true
	}
	for c := range b {
		cats[c] = true
	}
	names := make([]string, 0, len(cats))
	for c := range cats {
		names = append(names, c)
	}
	sort.Strings(names)
	equal := true
	var parts []string
	for _, c := range names {
		parts = append(parts, fmt.Sprintf("%s=%d:%s", c, len(a[c]), bootstrapTemplateDigest(a[c])))
		if bootstrapTemplateDigest(a[c]) == bootstrapTemplateDigest(b[c]) {
			continue
		}
		equal = false
		parts = append(parts, fmt.Sprintf("DIFF_%s(a=%d,b=%d)", c, len(a[c]), len(b[c])))
		for _, l := range bootstrapTemplateOnlyIn(a[c], b[c], 3) {
			parts = append(parts, "only_a["+strings.ReplaceAll(l, " ", "_")+"]")
		}
		for _, l := range bootstrapTemplateOnlyIn(b[c], a[c], 3) {
			parts = append(parts, "only_b["+strings.ReplaceAll(l, " ", "_")+"]")
		}
	}
	return equal, strings.Join(parts, " ")
}

func bootstrapTemplateOnlyIn(a, b []string, limit int) []string {
	seen := map[string]bool{}
	for _, l := range b {
		seen[l] = true
	}
	var out []string
	for _, l := range a {
		if !seen[l] && len(out) < limit {
			out = append(out, l[:min(len(l), 240)])
		}
	}
	return out
}

// templateCloneFlavourApplies maps each pinned flavour to its apply shape for
// the differential proof. The map is the test-side pin: production callers
// pass equivalent closures through openDisposablePublicBootstrapCloneDB.
func templateCloneFlavourApplies() map[string]func(context.Context, SQLDB) error {
	return map[string]func(context.Context, SQLDB) error{
		bootstrapTemplateFlavourA: func(ctx context.Context, exec SQLDB) error {
			return ApplyBootstrap(ctx, exec)
		},
		bootstrapTemplateFlavourB: func(ctx context.Context, exec SQLDB) error {
			return ApplyBootstrapWithoutContentSearchIndexes(ctx, exec)
		},
	}
}

// TestReducerContentionGateBootstrapTemplateCloneMatchesBootstrap is the
// committed differential for #10148 PR-A: for each pinned flavour, a clone of
// the process template must be indistinguishable from a fresh real bootstrap
// on every catalog line. The gate's -run filter selects it by the
// TestReducerContentionGate prefix, so the equality rides the gate itself.
func TestReducerContentionGateBootstrapTemplateCloneMatchesBootstrap(t *testing.T) {
	adminDSN, optIn := templateCloneProofDSN(t)
	ctx := context.Background()
	for flavour, apply := range templateCloneFlavourApplies() {
		apply := apply
		t.Run(flavour, func(t *testing.T) {
			_, fresh := postgresproof.OpenDisposableBootstrapDatabase(t, adminDSN, optIn, 5*time.Minute,
				func(ctx context.Context, database *sql.DB) error {
					return apply(ctx, SQLDB{DB: database})
				})
			template := postgresproof.OpenBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute, flavour,
				func(ctx context.Context, database *sql.DB) error {
					return apply(ctx, SQLDB{DB: database})
				})
			_, clone := postgresproof.OpenBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)

			freshFP := mustFingerprint(ctx, t, fresh)
			cloneFP := mustFingerprint(ctx, t, clone)
			equal, detail := bootstrapTemplateCompare(freshFP, cloneFP)
			t.Logf("flavour=%s bootstrap_vs_clone=%t %s", flavour, equal, detail)
			if !equal {
				t.Fatalf("flavour %s: clone differs from fresh bootstrap: %s", flavour, detail)
			}
		})
	}
}

// TestReducerContentionGateBootstrapTemplateCloneConcurrent is the committed
// concurrency proof for #10148 PR-A: parallel clones from one template all
// match the template fingerprint, and cloning while a session stays on the
// template fails with SQLSTATE 55006 instead of a half-built database.
func TestReducerContentionGateBootstrapTemplateCloneConcurrent(t *testing.T) {
	adminDSN, optIn := templateCloneProofDSN(t)
	ctx := context.Background()
	template := postgresproof.OpenBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
		bootstrapTemplateFlavourA,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	_, wantDB := postgresproof.OpenBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)
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
			db, cleanup, err := postgresproof.CloneBootstrapTemplate(ctx, adminDSN, optIn, template)
			if err != nil {
				errs[i] = fmt.Errorf("parallel clone %d: %w", i, err)
				return
			}
			cleanups[i] = cleanup
			got, ferr := bootstrapTemplateFingerprint(ctx, db)
			if ferr != nil {
				errs[i] = fmt.Errorf("parallel clone %d fingerprint: %w", i, ferr)
				return
			}
			if equal, detail := bootstrapTemplateCompare(want, got); !equal {
				errs[i] = fmt.Errorf("parallel clone %d differs from template: %s", i, detail)
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
			t.Errorf("clone %d: %v", i, err)
		}
	}

	// A session parked on the template must refuse the clone with 55006.
	holderDSN, err := postgresproof.WithDatabase(adminDSN, template)
	if err != nil {
		t.Fatalf("derive template session DSN: %v", err)
	}
	holder, err := sql.Open("pgx", holderDSN)
	if err != nil {
		t.Fatalf("open template session: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if err := holder.PingContext(ctx); err != nil {
		t.Fatalf("ping template session: %v", err)
	}
	adminCfg, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	admin, err := sql.Open("pgx", adminCfg.ConnString())
	if err != nil {
		t.Fatalf("open admin session: %v", err)
	}
	defer func() { _ = admin.Close() }()
	// The busy-proof name carries a random suffix like disposableDatabaseName:
	// on the unexpected-success path the database is created, and a fixed
	// name would leak it and fail a rerun with 42P04 instead of the asserted
	// 55006, misdiagnosing the failure.
	busyProof := "eshu_clone_busy_proof_" + busyProofSuffix(t)
	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+pgx.Identifier{busyProof}.Sanitize()+" TEMPLATE "+pgx.Identifier{template}.Sanitize())
	if err == nil {
		if _, dropErr := admin.ExecContext(ctx, "DROP DATABASE "+pgx.Identifier{busyProof}.Sanitize()); dropErr != nil {
			t.Fatalf("clone with a session on the template unexpectedly succeeded, and dropping %q failed: %v", busyProof, dropErr)
		}
		t.Fatal("clone with a session on the template succeeded, want SQLSTATE 55006")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55006" {
		t.Fatalf("clone with a session on the template: error = %v, want SQLSTATE 55006", err)
	}
}
