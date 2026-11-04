// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

const requiredDisposableOptIn = "1"

// OpenDisposableDatabase creates a unique database through an explicitly
// approved connection to PostgreSQL's administrative postgres database. The
// generated database uses pristine template0 and a public search path, and is
// force-dropped during test cleanup.
func OpenDisposableDatabase(
	t testing.TB,
	adminDSN string,
	optIn string,
	timeout time.Duration,
) (context.Context, *sql.DB) {
	t.Helper()
	if strings.TrimSpace(adminDSN) == "" {
		t.Skip(missingAdminDSNSkipMessage())
	}
	config, err := validateAdminDSN(adminDSN, optIn)
	if err != nil {
		t.Fatalf("refuse destructive PostgreSQL proof target: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	adminDB, err := sql.Open("pgx", config.ConnString())
	if err != nil {
		t.Fatalf("open PostgreSQL administrative database: %v", err)
	}
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		t.Fatalf("ping PostgreSQL administrative database: %v", err)
	}

	databaseName := disposableDatabaseName(t)
	// Create from template0 so extensions and relations installed in
	// template1 (for example a pg_trgm left in public) cannot leak into the
	// proof database and shadow the bootstrap's own installs.
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(databaseName)+" TEMPLATE template0"); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create disposable PostgreSQL database: %v", err)
	}
	targetConfig := config.Copy()
	targetConfig.Database = databaseName
	// Pin the search path to public so unqualified operator classes such as
	// gin_trgm_ops resolve to this database's own installs even when the
	// caller's session carries a schema-first search_path.
	targetConfig.RuntimeParams = cloneRuntimeParams(config.RuntimeParams)
	targetConfig.RuntimeParams["search_path"] = "public"
	// Connect the way the service runtimes do (runtime.OpenPostgres): every
	// connection carries the derive-aware writer setting, so a proof that
	// seeds infra-typed content rows leaves no rolling-upgrade fence marks.
	//
	// Register the drop before opening the target: every later exit, including
	// a failed open or ping, runs this cleanup, so neither the target handle
	// nor the database leaks on a failure path.
	var targetDB *sql.DB
	t.Cleanup(func() {
		if targetDB != nil {
			_ = targetDB.Close()
		}
		dropDisposableDatabase(t, adminDB, databaseName)
		_ = adminDB.Close()
	})
	targetDB = stdlib.OpenDB(*targetConfig, inventory.WriterConnectOption())
	if err := targetDB.PingContext(ctx); err != nil {
		t.Fatalf("ping disposable PostgreSQL database: %v", err)
	}
	return ctx, targetDB
}

// OpenDisposableBootstrapDatabase opens a disposable database exactly the
// way OpenDisposableDatabase does (template0, search_path=public, the same
// administrative DSN and explicit opt-in checks) and runs apply on it before
// returning, so a full production bootstrap lands in public instead of an
// isolated schema. The caller passes the administrative DSN and the opt-in
// explicitly, read from the proof family's own environment variables; the
// helper never derives either one, and an absent DSN skips like
// OpenDisposableDatabase does. An apply error fails the test before any
// caller can use a half-built database.
func OpenDisposableBootstrapDatabase(
	t testing.TB,
	adminDSN string,
	optIn string,
	timeout time.Duration,
	apply func(context.Context, *sql.DB) error,
) (context.Context, *sql.DB) {
	t.Helper()
	ctx, database := OpenDisposableDatabase(t, adminDSN, optIn, timeout)
	if err := apply(ctx, database); err != nil {
		t.Fatalf("apply schema to disposable PostgreSQL database: %v", err)
	}
	return ctx, database
}

// WithDatabase returns dsn with its database replaced by database, so a
// proof holding a family DSN can derive the administrative session (database
// "postgres", which OpenDisposableDatabase requires) or a session on a
// generated disposable database (whose name the proof learns from
// current_database()). Credentials, host, port, and every other setting
// carry over byte-identical; only the database changes, in the DSN's own
// form (URL or keyword/value, dispatched the way pgconn parses). An
// unparseable DSN is reported instead of opening the wrong database.
//
// pgx's ConnString cannot do this: it replays the originally parsed string,
// so mutating the parsed config's Database never reaches the emitted DSN.
func WithDatabase(dsn, database string) (string, error) {
	if _, err := pgx.ParseConfig(dsn); err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return withDatabaseURL(dsn, database)
	}
	return withDatabaseKeyword(dsn, database)
}

// withDatabaseURL replaces the path database of a URL-form DSN, keeping
// userinfo, host, port, and query parameters untouched.
func withDatabaseURL(dsn, database string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN URL: %w", err)
	}
	parsed.Path = "/" + database
	return parsed.String(), nil
}

// withDatabaseKeyword replaces (or appends) the dbname setting of a
// keyword/value-form DSN, keeping every other setting and its quoting.
// Values with whitespace or quotes stay single-quoted per libpq rules.
func withDatabaseKeyword(dsn, database string) (string, error) {
	tokens, err := splitKeywordDSN(dsn)
	if err != nil {
		return "", err
	}
	replacement := "dbname=" + quoteKeywordValue(database)
	replaced := false
	for i, token := range tokens {
		if keywordName(token) == "dbname" {
			tokens[i] = replacement
			replaced = true
		}
	}
	if !replaced {
		tokens = append(tokens, replacement)
	}
	return strings.Join(tokens, " "), nil
}

// splitKeywordDSN splits a keyword/value DSN on unquoted whitespace,
// following libpq quoting (single quotes with backslash and ” escapes).
func splitKeywordDSN(dsn string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quoted bool
	for i := 0; i < len(dsn); i++ {
		char := dsn[i]
		switch {
		case quoted && char == '\\' && i+1 < len(dsn):
			current.WriteByte(char)
			i++
			current.WriteByte(dsn[i])
		case char == '\'':
			quoted = !quoted
			current.WriteByte(char)
		case !quoted && (char == ' ' || char == '\t' || char == '\n'):
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(char)
		}
	}
	if quoted {
		return nil, fmt.Errorf("parse DSN: unterminated quoted value")
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// keywordName returns the setting name before the first '='.
func keywordName(token string) string {
	if i := strings.IndexByte(token, '='); i >= 0 {
		return token[:i]
	}
	return ""
}

// quoteKeywordValue single-quotes a keyword value when it carries
// whitespace or quotes, escaping embedded quotes per libpq rules.
func quoteKeywordValue(value string) string {
	if strings.ContainsAny(value, " \t\n'\\") {
		return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
	}
	return value
}

// cloneRuntimeParams copies a pgx runtime-parameter map so the disposable
// target can pin its own search_path without rewriting the shared
// administrative config's session defaults.
func cloneRuntimeParams(params map[string]string) map[string]string {
	out := make(map[string]string, len(params)+1)
	for key, value := range params {
		out[key] = value
	}
	return out
}

func missingAdminDSNSkipMessage() string {
	return "disposable PostgreSQL proof DSN is not set; see the calling test for its environment variable"
}

func validateAdminDSN(dsn, optIn string) (*pgx.ConnConfig, error) {
	if optIn != requiredDisposableOptIn {
		return nil, fmt.Errorf("explicit disposable-database opt-in must equal %q", requiredDisposableOptIn)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse administrative DSN: %w", err)
	}
	if config.Database != "postgres" {
		return nil, fmt.Errorf("administrative DSN database must be %q", "postgres")
	}
	return config, nil
}

func disposableDatabaseName(t testing.TB) string {
	t.Helper()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate disposable PostgreSQL database name: %v", err)
	}
	return "eshu_content_index_proof_" + hex.EncodeToString(random)
}

func dropDisposableDatabase(t testing.TB, adminDB *sql.DB, databaseName string) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := terminateAndDropDisposableDatabase(cleanupCtx, adminDB, databaseName); err != nil {
		t.Errorf("drop disposable PostgreSQL database: %v", err)
	}
}

const (
	// disposableDropMaxAttempts bounds DROP DATABASE retries on SQLSTATE
	// 55006 (#9985). A database whose backends never drain fails loudly
	// after this many attempts instead of stalling test cleanup.
	disposableDropMaxAttempts = 5
	// disposableDropRetryDelay spaces 55006 retries so backends that were
	// slow to die on a loaded runner finish exiting before the next
	// recount. It runs only in test teardown, after every product
	// assertion, and never serializes the load under test.
	disposableDropRetryDelay = 500 * time.Millisecond
)

// terminateDisposableBackendsQuery asks every other backend on one database
// to exit. The datname parameter scopes the terminate to the generated proof
// database; the pid guard keeps the issuing administrative session out.
// Termination is asynchronous: a backend scheduled out on a loaded runner may
// still appear in pg_stat_activity when this returns, which is why the drop
// below retries.
const terminateDisposableBackendsQuery = `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`

// disposableDropAdmin is the administrative surface the disposable-database
// teardown needs: parameterized ExecContext. *sql.DB satisfies it; tests
// script it.
type disposableDropAdmin interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// terminateAndDropDisposableDatabase terminates the named database's other
// backends and force-drops it, retrying a bounded number of times on
// SQLSTATE 55006 (#9985). DROP DATABASE ... WITH (FORCE) signals other
// backends and recounts immediately: on a loaded runner a backend deep in a
// lock wait may not exit before the recount, so the drop fails with "database
// is being accessed by other users" even though every backend is already
// dying. An explicit terminate before every attempt plus a bounded wait
// closes that signal-recount-reap window without touching the load that
// produced the backends. Any other DROP failure returns immediately.
func terminateAndDropDisposableDatabase(ctx context.Context, admin disposableDropAdmin, databaseName string) error {
	var lastErr error
	var termErr error
	for attempt := 1; attempt <= disposableDropMaxAttempts; attempt++ {
		if _, err := admin.ExecContext(ctx, terminateDisposableBackendsQuery, databaseName); err != nil && termErr == nil {
			termErr = err
		}
		if _, dropErr := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(databaseName)+" WITH (FORCE)"); dropErr != nil {
			lastErr = dropErr
			if disposableDropSQLState(dropErr) != "55006" {
				return fmt.Errorf("drop disposable PostgreSQL database %q: %w", databaseName, dropErr)
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
	if termErr != nil {
		return fmt.Errorf("drop disposable PostgreSQL database after %d attempts (last terminate: %v): %w",
			disposableDropMaxAttempts, termErr, lastErr)
	}
	return fmt.Errorf("drop disposable PostgreSQL database after %d attempts: %w",
		disposableDropMaxAttempts, lastErr)
}

// disposableDropSQLState reports the SQLSTATE of err, or "" when err carries
// none. DROP DATABASE cannot take a parameter, so the generated name in the
// force-drop above stays identifier-quoted.
func disposableDropSQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
