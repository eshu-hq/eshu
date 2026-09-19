// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// BackfillMarker names the completion marker row. Readers trust the table
// only once this row exists. Changing what the table holds, such as a new
// label or column, needs a new marker name so a populated install re-runs
// the backfill before readers use the new shape.
//
// v2 added TerraformModule and TerraformOutput to Labels.
const BackfillMarker = "infra_resource_entities_v2"

const backfillCompleteSQL = `
SELECT EXISTS (
    SELECT 1 FROM infra_resource_entity_backfill_markers WHERE marker_name = $1
)`

// backfillRepositoriesSQL lists every repository that has entity-derived
// content rows, plus every repository that still has table rows. The second
// set lets the backfill clear rows whose content disappeared while no derive
// was running (for example before this binary shipped).
const backfillRepositoriesSQL = `
SELECT repo_id FROM content_entities WHERE entity_type = ANY($1::text[])
UNION
SELECT repo_id FROM infra_resource_entities
ORDER BY 1`

const backfillMarkSQL = `
INSERT INTO infra_resource_entity_backfill_markers (marker_name, completed_at)
VALUES ($1, $2)
ON CONFLICT (marker_name) DO NOTHING`

// backfillProgressEvery bounds how often a running backfill logs progress.
const backfillProgressEvery = 100

// undefinedTableSQLState is Postgres' undefined_table error code.
const undefinedTableSQLState = "42P01"

// IsNotInstalled reports whether err means the read model tables do not exist
// yet (SQLSTATE 42P01): migration 109 has not been applied to this database.
func IsNotInstalled(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == undefinedTableSQLState
}

// BackfillComplete reports whether the backfill marker exists. It is a
// primary-key lookup, cheap enough for readers to check on every request.
//
// A missing marker table means migration 109 has not been applied yet, for
// example while a new API binary starts before the bootstrap migrations of the
// same release. That is "read model not installed", so it reports false with
// no error and readers stay on the graph. Every other error is returned.
func BackfillComplete(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, backfillCompleteSQL, BackfillMarker)
	if err != nil {
		if IsNotInstalled(err) {
			return false, nil
		}
		return false, fmt.Errorf("check infra inventory backfill marker: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var complete bool
	if rows.Next() {
		if err := rows.Scan(&complete); err != nil {
			return false, fmt.Errorf("scan infra inventory backfill marker: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check infra inventory backfill marker: %w", err)
	}
	return complete, nil
}

// Backfiller populates infra_resource_entities for every existing repository
// and then records BackfillMarker.
type Backfiller struct {
	DB     db.ExecQueryer
	Logger *slog.Logger
	Now    func() time.Time
}

// BackfillResult reports what one Run did.
type BackfillResult struct {
	AlreadyComplete bool
	Repositories    int
	RowsInserted    int64
	Duration        time.Duration
}

// Run re-derives every repository with MirrorRepo, one transaction per
// repository under the same lock the live derive takes, and records the marker
// only after every repository succeeded. A failure leaves no marker, so the
// next Run starts over; every step is idempotent. Several processes may run it
// at once: they serialize per repository on the lock and the marker insert is
// ON CONFLICT DO NOTHING.
func (b Backfiller) Run(ctx context.Context) (BackfillResult, error) {
	if b.DB == nil {
		return BackfillResult{}, errors.New("infra inventory backfill: database is required")
	}
	start := time.Now()
	complete, err := BackfillComplete(ctx, b.DB)
	if err != nil {
		return BackfillResult{}, err
	}
	if complete {
		return BackfillResult{AlreadyComplete: true}, nil
	}

	repos, err := b.repositories(ctx)
	if err != nil {
		return BackfillResult{}, err
	}
	b.log(ctx, "infra inventory backfill started", "repository_count", len(repos))

	result := BackfillResult{Repositories: len(repos)}
	for i, repo := range repos {
		stats, err := MirrorRepo(ctx, b.DB, repo)
		if err != nil {
			return result, fmt.Errorf("infra inventory backfill after %d of %d repositories: %w", i, len(repos), err)
		}
		result.RowsInserted += stats.Inserted
		if (i+1)%backfillProgressEvery == 0 {
			b.log(ctx, "infra inventory backfill progress",
				"repositories_done", i+1, "repository_count", len(repos), "rows_inserted", result.RowsInserted)
		}
	}

	now := time.Now().UTC()
	if b.Now != nil {
		now = b.Now().UTC()
	}
	if _, err := b.DB.ExecContext(ctx, backfillMarkSQL, BackfillMarker, now); err != nil {
		return result, fmt.Errorf("record infra inventory backfill marker: %w", err)
	}
	result.Duration = time.Since(start)
	b.log(ctx, "infra inventory backfill completed",
		"repository_count", len(repos), "rows_inserted", result.RowsInserted,
		"duration_seconds", result.Duration.Seconds(), "marker", BackfillMarker)
	return result, nil
}

func (b Backfiller) repositories(ctx context.Context) ([]string, error) {
	rows, err := b.DB.QueryContext(ctx, backfillRepositoriesSQL, pgarray.StringArray(Labels))
	if err != nil {
		return nil, fmt.Errorf("list infra inventory backfill repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var repos []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, fmt.Errorf("scan infra inventory backfill repository: %w", err)
		}
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list infra inventory backfill repositories: %w", err)
	}
	return repos, nil
}

func (b Backfiller) log(ctx context.Context, msg string, attrs ...any) {
	if b.Logger == nil {
		return
	}
	b.Logger.InfoContext(ctx, msg, append([]any{"event_name", "infra_inventory.backfill"}, attrs...)...)
}
