// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writershape

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Store implements recovery.WriterShapeStore: the Ensure
// contract in recovery owns the upgrade sequence, this store owns the
// marker row it converges on.
var _ recovery.WriterShapeStore = Store{}

// graphWriterShapeSchemaSQL is the durable writer-shape marker table so a
// deployment retires stale generations exactly once per writer-semantics
// upgrade instead of relying on an operator refinalize (issue #6868).
// applied_version advances only after the upgrade refinalize succeeds;
// claimed_at lets a crashed or failed winner's claim expire so the next
// starter retries.
const graphWriterShapeSchemaSQL = `CREATE TABLE IF NOT EXISTS graph_writer_shape (
    shape_key TEXT PRIMARY KEY,
    applied_version INTEGER NOT NULL DEFAULT 0,
    claimed_version INTEGER NULL,
    claimed_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
`

const graphWriterShapeAppliedSQL = `
SELECT applied_version FROM graph_writer_shape WHERE shape_key = $1
`

const graphWriterShapeEnsureRowSQL = `
INSERT INTO graph_writer_shape (shape_key, applied_version, updated_at)
VALUES ($1, 0, now())
ON CONFLICT (shape_key) DO NOTHING
`

const graphWriterShapeClaimSQL = `
UPDATE graph_writer_shape SET claimed_version = $2, claimed_at = now(), updated_at = now()
WHERE shape_key = $1 AND applied_version < $2
  AND (claimed_at IS NULL
    OR claimed_at < now() - ($3 * interval '1 second')
    OR claimed_version IS NULL
    OR claimed_version < $2)
RETURNING shape_key
`

const graphWriterShapeMarkAppliedSQL = `
UPDATE graph_writer_shape SET applied_version = $2, updated_at = now()
WHERE shape_key = $1 AND applied_version < $2
`

const graphWriterShapeReleaseSQL = `
UPDATE graph_writer_shape SET claimed_at = NULL, updated_at = now()
WHERE shape_key = $1
`

// SchemaSQL returns the DDL for the writer-shape marker table.
func SchemaSQL() string {
	return graphWriterShapeSchemaSQL
}

// Store persists the applied graph-writer shape version,
// implementing recovery.WriterShapeStore over Postgres. It references the
// recovery contract through method signatures only -- the interface lives
// in recovery so this package keeps its existing dependency direction.
type Store struct {
	database db.ExecQueryer
}

// NewStore constructs a Postgres-backed writer-shape marker store.
func NewStore(database db.ExecQueryer) Store {
	return Store{database: database}
}

// EnsureSchema applies the writer-shape marker DDL.
func (s Store) EnsureSchema(ctx context.Context) error {
	if s.database == nil {
		return fmt.Errorf("graph writer shape store database is required")
	}
	if _, err := s.database.ExecContext(ctx, graphWriterShapeSchemaSQL); err != nil {
		return fmt.Errorf("ensure graph writer shape schema: %w", err)
	}
	return nil
}

// AppliedVersion reports the last retired writer shape version, or 0 when
// no retirement has ever been recorded.
func (s Store) AppliedVersion(ctx context.Context, key string) (int, error) {
	if s.database == nil {
		return 0, fmt.Errorf("graph writer shape store database is required")
	}
	rows, err := s.database.QueryContext(ctx, graphWriterShapeAppliedSQL, key)
	if err != nil {
		return 0, fmt.Errorf("read applied writer shape version: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var version int
	if err := rows.Scan(&version); err != nil {
		return 0, fmt.Errorf("scan applied writer shape version: %w", err)
	}
	return version, rows.Err()
}

// ClaimVersion takes the upgrade claim for version when no version at
// least this new is applied and no unexpired competing claim exists. The
// second claimant blocks on the winner's row and re-evaluates against it,
// so exactly one starter wins; a crashed winner's claim expires after the
// lease.
func (s Store) ClaimVersion(ctx context.Context, key string, version int, lease time.Duration) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("graph writer shape store database is required")
	}
	if _, err := s.database.ExecContext(ctx, graphWriterShapeEnsureRowSQL, key); err != nil {
		return false, fmt.Errorf("ensure writer shape marker row: %w", err)
	}
	rows, err := s.database.QueryContext(ctx, graphWriterShapeClaimSQL, key, version, int64(lease/time.Second))
	if err != nil {
		return false, fmt.Errorf("claim writer shape version: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	return true, rows.Err()
}

// MarkAppliedVersion records version as retired. Callers invoke it only
// after the upgrade refinalize succeeds.
func (s Store) MarkAppliedVersion(ctx context.Context, key string, version int) error {
	if s.database == nil {
		return fmt.Errorf("graph writer shape store database is required")
	}
	if _, err := s.database.ExecContext(ctx, graphWriterShapeMarkAppliedSQL, key, version); err != nil {
		return fmt.Errorf("mark writer shape version applied: %w", err)
	}
	return nil
}

// ReleaseClaim clears the key's claim so the next starter retries
// immediately instead of waiting out the lease. Callers invoke it when
// the upgrade refinalize fails.
func (s Store) ReleaseClaim(ctx context.Context, key string) error {
	if s.database == nil {
		return fmt.Errorf("graph writer shape store database is required")
	}
	if _, err := s.database.ExecContext(ctx, graphWriterShapeReleaseSQL, key); err != nil {
		return fmt.Errorf("release writer shape claim: %w", err)
	}
	return nil
}
