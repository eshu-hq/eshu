// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// BackfillMarker names the completion marker row in
// config_artifact_backfill_markers. The reducer walker records it when every
// repository has a current state row: the first full walk is done and the
// stored rows cover the walk universe. Changing what the stored rows hold,
// such as a new column or row shape, needs a new marker name so a populated
// install re-runs the backfill before readers trust the new shape; an
// extractor change does not, because the dirty join re-derives every
// repository under the same marker. Renaming this constant orphans installs
// that already recorded it, so TestBackfillMarkerName pins it.
const BackfillMarker = "config_artifact_paths_v1"

const backfillCompleteSQL = `
SELECT EXISTS (
    SELECT 1 FROM config_artifact_backfill_markers WHERE marker_name = $1
)`

const backfillMarkSQL = `
INSERT INTO config_artifact_backfill_markers (marker_name, completed_at)
VALUES ($1, now())
ON CONFLICT (marker_name) DO NOTHING`

// undefinedTableSQLState is Postgres' undefined_table error code.
const undefinedTableSQLState = "42P01"

// isNotInstalled reports whether err means the read model tables do not exist
// yet (SQLSTATE 42P01): the migrations have not been applied to this database,
// for example while a new reducer binary starts before the bootstrap
// migrations of the same release. It mirrors the infra read model's check;
// the two leaves stay independent so neither imports the other.
func isNotInstalled(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == undefinedTableSQLState
}

// BackfillComplete reports whether the backfill marker exists. It is a
// primary-key lookup, cheap enough for the walker to check once per cycle.
//
// A missing marker table means the migrations have not been applied yet. That
// is "read model not installed", so it reports false with no error and the
// walker derives nothing until the tables exist. Every other error is
// returned.
func BackfillComplete(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, backfillCompleteSQL, BackfillMarker)
	if err != nil {
		if isNotInstalled(err) {
			return false, nil
		}
		return false, fmt.Errorf("check config artifact backfill marker: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var complete bool
	if rows.Next() {
		if err := rows.Scan(&complete); err != nil {
			return false, fmt.Errorf("scan config artifact backfill marker: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check config artifact backfill marker: %w", err)
	}
	return complete, nil
}

// RecordBackfillMarker records the backfill completion marker. It runs after
// a walk cycle that left no repository dirty, so every repository has a
// current state row. The insert is ON CONFLICT DO NOTHING: two replicas that
// finish the walk together record once.
func RecordBackfillMarker(ctx context.Context, database db.Executor) error {
	if _, err := database.ExecContext(ctx, backfillMarkSQL, BackfillMarker); err != nil {
		return fmt.Errorf("record config artifact backfill marker: %w", err)
	}
	return nil
}
