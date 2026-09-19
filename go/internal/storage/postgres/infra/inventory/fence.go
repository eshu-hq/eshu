// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// WriterSessionSQL marks a connection as derive-aware. Migration 109's
// content_entities triggers skip rows written by such a connection; every
// other connection that writes an infra-typed content row (a binary from
// before the read model, during a rolling upgrade, or manual SQL) marks the
// repository in infra_resource_entity_dirty_repos in the same statement.
// Only binaries whose content_entities writes keep the table in step (the
// ContentWriter's derive, generation retention's orphan delete) may run it;
// runtime.OpenPostgres does, through WriterConnectOption.
//
// A pooler that drops session state loses the setting. That fails safe: the
// writes are marked dirty, reads stay on the graph, and the reconcile
// repairs the repositories.
const WriterSessionSQL = "SET eshu.infra_inventory_writer = 'derive'"

// WriterConnectOption runs WriterSessionSQL on every new pool connection.
func WriterConnectOption() stdlib.OptionOpenDB {
	return stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, WriterSessionSQL); err != nil {
			return fmt.Errorf("mark connection as infra inventory writer: %w", err)
		}
		return nil
	})
}

// OpenWriterDB opens a database/sql pool over the pgx driver whose
// connections are marked derive-aware (WriterConnectOption). It does not
// connect; the first use or a ping does.
func OpenWriterDB(dsn string) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	return stdlib.OpenDB(*config, WriterConnectOption()), nil
}

// readModelReadySQL is the readers' gate: the backfill marker exists and no
// repository waits for a fence repair.
const readModelReadySQL = `
SELECT EXISTS (
    SELECT 1 FROM infra_resource_entity_backfill_markers WHERE marker_name = $1
) AND NOT EXISTS (
    SELECT 1 FROM infra_resource_entity_dirty_repos
)`

// dirtyReposSQL lists the oldest fence marks first, up to $1.
const dirtyReposSQL = `
SELECT repo_id FROM infra_resource_entity_dirty_repos
ORDER BY marked_at, repo_id
LIMIT $1`

// clearDirtySQL drops a repository's fence mark inside the repair
// transaction, after the repository lock and before the re-derive. An
// unaware write whose statement is still open holds the mark's row lock
// (the trigger upserts it), so this DELETE waits for that write to commit
// and the re-derive that follows reads its rows. An unaware write that
// starts after this DELETE inserts a new mark that survives the repair.
const clearDirtySQL = `DELETE FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`

// ReadModelReady reports whether readers may trust the table: the backfill
// marker exists and no repository is marked dirty by an unaware writer. A
// missing table (migration 109 not applied) reports false with no error.
func ReadModelReady(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, readModelReadySQL, BackfillMarker)
	if err != nil {
		if IsNotInstalled(err) {
			return false, nil
		}
		return false, fmt.Errorf("check infra inventory read model readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ready bool
	if rows.Next() {
		if err := rows.Scan(&ready); err != nil {
			return false, fmt.Errorf("scan infra inventory read model readiness: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check infra inventory read model readiness: %w", err)
	}
	return ready, nil
}

func dirtyRepositories(ctx context.Context, queryer db.Queryer, limit int) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, dirtyReposSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("list infra inventory dirty repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var repos []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, fmt.Errorf("scan infra inventory dirty repository: %w", err)
		}
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list infra inventory dirty repositories: %w", err)
	}
	return repos, nil
}

// repairDirty re-derives a fence-marked repository in one transaction: lock,
// clear the mark, delete, insert. The mark proves an unaware writer changed
// content without deriving, so the repair needs no second check.
func repairDirty(ctx context.Context, database db.ExecQueryer, repoID string) (result RepoReconcile) {
	start := time.Now()
	result = RepoReconcile{RepoID: repoID, Outcome: ReconcileFenced}
	defer func() { result.Duration = time.Since(start) }()
	stats, err := repairDirtyTx(ctx, database, repoID)
	if err != nil {
		result.Outcome = ReconcileError
		result.Err = fmt.Errorf("infra inventory fence repair repo %q: %w", repoID, err)
		return result
	}
	result.Repair = stats
	return result
}

func repairDirtyTx(ctx context.Context, database db.ExecQueryer, repoID string) (stats Stats, err error) {
	beginner, ok := database.(db.Beginner)
	if !ok {
		return Stats{}, errors.New("database must support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, repoLockSQL, repoID); err != nil {
		return Stats{}, fmt.Errorf("lock repo: %w", err)
	}
	if _, err = tx.ExecContext(ctx, clearDirtySQL, repoID); err != nil {
		return Stats{}, fmt.Errorf("clear dirty mark: %w", err)
	}
	deleted, err := tx.ExecContext(ctx, mirrorRepoDeleteSQL, repoID)
	if err != nil {
		return Stats{}, fmt.Errorf("delete: %w", err)
	}
	inserted, err := tx.ExecContext(ctx, mirrorRepoInsertSQL, repoID, pgarray.StringArray(Labels), "", "")
	if err != nil {
		return Stats{}, fmt.Errorf("insert: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("commit: %w", err)
	}
	stats.Deleted, _ = deleted.RowsAffected()
	stats.Inserted, _ = inserted.RowsAffected()
	return stats, nil
}
