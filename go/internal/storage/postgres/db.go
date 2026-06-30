// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ExecQueryer combines read and write access for storage adapters.
type ExecQueryer interface {
	Queryer
	Executor
}

// Transaction is the narrow transactional surface required by durable commit
// boundaries in storage adapters.
type Transaction interface {
	ExecQueryer
	Commit() error
	Rollback() error
}

// Beginner constructs transactions for storage adapters that need atomic writes.
type Beginner interface {
	Begin(context.Context) (Transaction, error)
}

// SQLDB adapts a *sql.DB into the combined storage interface surface.
type SQLDB struct {
	DB *sql.DB
}

var concurrentIndexNamePattern = regexp.MustCompile(`(?is)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:"(?:[^"]|"")+")|[A-Za-z_][A-Za-z0-9_$]*)`)

// QueryContext implements Queryer against a sql.DB.
func (db SQLDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return db.DB.QueryContext(ctx, query, args...)
}

// ExecContext implements Executor against a sql.DB.
func (db SQLDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.DB.ExecContext(ctx, query, args...)
}

func (db SQLDB) execContextWithLockTimeout(
	ctx context.Context,
	query string,
	lockTimeout time.Duration,
) (sql.Result, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("postgres SQLDB requires a database handle")
	}
	if lockTimeout <= 0 {
		return db.ExecContext(ctx, query)
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open schema connection: %w", err)
	}
	closeConn := true
	defer func() {
		if closeConn {
			_ = conn.Close()
		}
	}()

	if _, err := conn.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, false)", lockTimeout.String()); err != nil {
		return nil, fmt.Errorf("set schema lock timeout: %w", err)
	}
	if err := db.dropInvalidConcurrentIndexes(ctx, conn, concurrentIndexNamesForInvalidCleanup(query)); err != nil {
		resetErr := resetSchemaLockTimeout(conn)
		closeErr := conn.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("close schema connection: %w", closeErr)
		}
		closeConn = false
		return nil, errors.Join(err, resetErr, closeErr)
	}
	result, execErr := conn.ExecContext(ctx, query)
	resetErr := resetSchemaLockTimeout(conn)
	closeErr := conn.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close schema connection: %w", closeErr)
	}
	closeConn = false
	return result, errors.Join(execErr, resetErr, closeErr)
}

func resetSchemaLockTimeout(conn *sql.Conn) error {
	resetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(resetCtx, "SELECT set_config('lock_timeout', '0', false)"); err != nil {
		return fmt.Errorf("reset schema lock timeout: %w", err)
	}
	return nil
}

// dropInvalidConcurrentIndexes removes invalid indexes left by failed
// concurrent index builds so IF NOT EXISTS cannot silently skip a broken index.
func (db SQLDB) dropInvalidConcurrentIndexes(ctx context.Context, conn *sql.Conn, indexNames []string) error {
	for _, indexName := range indexNames {
		rows, err := conn.QueryContext(ctx, `
SELECT n.nspname, c.relname
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname = $1
  AND i.indisvalid = FALSE
`, indexName)
		if err != nil {
			return fmt.Errorf("query invalid concurrent index %s: %w", indexName, err)
		}

		type invalidIndex struct {
			schema string
			name   string
		}
		var invalidIndexes []invalidIndex
		for rows.Next() {
			var idx invalidIndex
			if err := rows.Scan(&idx.schema, &idx.name); err != nil {
				closeErr := rows.Close()
				return errors.Join(fmt.Errorf("scan invalid concurrent index %s: %w", indexName, err), closeErr)
			}
			invalidIndexes = append(invalidIndexes, idx)
		}
		if err := rows.Err(); err != nil {
			closeErr := rows.Close()
			return errors.Join(fmt.Errorf("iterate invalid concurrent index %s: %w", indexName, err), closeErr)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close invalid concurrent index rows %s: %w", indexName, err)
		}

		for _, idx := range invalidIndexes {
			qualifiedName := quoteSQLIdentifier(idx.schema) + "." + quoteSQLIdentifier(idx.name)
			if _, err := conn.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+qualifiedName); err != nil {
				return fmt.Errorf("drop invalid concurrent index %s: %w", qualifiedName, err)
			}
		}
	}
	return nil
}

// concurrentIndexNamesForInvalidCleanup extracts the index names from
// CREATE INDEX CONCURRENTLY statements whose retry path needs invalid cleanup.
func concurrentIndexNamesForInvalidCleanup(query string) []string {
	matches := concurrentIndexNamePattern.FindAllStringSubmatch(query, -1)
	names := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := unquoteSQLIdentifier(match[1])
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// unquoteSQLIdentifier decodes the quoted identifier form used in schema SQL.
func unquoteSQLIdentifier(identifier string) string {
	if len(identifier) >= 2 && identifier[0] == '"' && identifier[len(identifier)-1] == '"' {
		return strings.ReplaceAll(identifier[1:len(identifier)-1], `""`, `"`)
	}
	return identifier
}

// quoteSQLIdentifier quotes catalog-derived identifiers before DDL execution.
func quoteSQLIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// Begin opens a transaction against the wrapped database.
func (db SQLDB) Begin(ctx context.Context) (Transaction, error) {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	return SQLTx{Tx: tx}, nil
}

// SQLTx adapts a *sql.Tx into the storage transaction surface.
type SQLTx struct {
	Tx *sql.Tx
}

// QueryContext implements Queryer against a sql.Tx.
func (tx SQLTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.Tx.QueryContext(ctx, query, args...)
}

// ExecContext implements Executor against a sql.Tx.
func (tx SQLTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, query, args...)
}

// Commit commits the wrapped transaction.
func (tx SQLTx) Commit() error {
	return tx.Tx.Commit()
}

// Rollback rolls back the wrapped transaction.
func (tx SQLTx) Rollback() error {
	return tx.Tx.Rollback()
}
