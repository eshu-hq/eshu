// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	if _, err := adminDB.ExecContext(
		cleanupCtx,
		"DROP DATABASE IF EXISTS "+quoteIdentifier(databaseName)+" WITH (FORCE)",
	); err != nil {
		t.Errorf("drop disposable PostgreSQL database: %v", err)
	}
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
