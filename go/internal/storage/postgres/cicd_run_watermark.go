// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
)

const cicdRunWatermarkSchemaSQL = `
CREATE TABLE IF NOT EXISTS cicd_run_watermarks (
    scope_id TEXT NOT NULL,
    repository TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    last_run_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, repository)
);
`

const loadCICDRunWatermarkQuery = `
SELECT last_run_id, generation_id, fencing_token, updated_at
FROM cicd_run_watermarks
WHERE scope_id = $1
  AND repository = $2
`

// saveCICDRunWatermarkQuery intentionally has NO generation_id/fencing_token
// predicate on the SELECT side of the guard (unlike
// aws_pagination_checkpoint.go's Load): a watermark must be readable ACROSS
// generations, because gap detection compares this cycle's window against a
// PRIOR cycle's (necessarily different generation_id) watermark. Only Save
// is fenced.
const saveCICDRunWatermarkQuery = `
INSERT INTO cicd_run_watermarks (
    scope_id,
    repository,
    generation_id,
    fencing_token,
    last_run_id,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (scope_id, repository) DO UPDATE SET
    generation_id = EXCLUDED.generation_id,
    fencing_token = EXCLUDED.fencing_token,
    last_run_id = EXCLUDED.last_run_id,
    updated_at = EXCLUDED.updated_at
WHERE cicd_run_watermarks.fencing_token <= EXCLUDED.fencing_token
`

// CICDRunWatermarkStore persists claim-fenced CI/CD run watermarks in
// Postgres, closing the #5429 cross-cycle gap-detection watermark
// (go/internal/collector/cicdrun/runwatermark) across process restarts and
// collector replicas. It mirrors AWSPaginationCheckpointStore's fencing
// pattern (a Save carrying a fencing token older than the stored row is
// rejected) but Load has no generation/fencing predicate: unlike an AWS
// pagination checkpoint (resume state scoped to ONE generation's scan), a
// run watermark must be readable by a LATER generation to detect a gap
// against an EARLIER generation's progress.
type CICDRunWatermarkStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewCICDRunWatermarkStore constructs a watermark store over the shared
// data-plane database.
func NewCICDRunWatermarkStore(db ExecQueryer) CICDRunWatermarkStore {
	return CICDRunWatermarkStore{db: db}
}

// CICDRunWatermarkSchemaSQL returns the DDL for CI/CD run watermark rows.
func CICDRunWatermarkSchemaSQL() string {
	return cicdRunWatermarkSchemaSQL
}

// EnsureSchema applies the CI/CD run watermark DDL.
func (s CICDRunWatermarkStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("ci/cd run watermark database is required")
	}
	if _, err := s.db.ExecContext(ctx, cicdRunWatermarkSchemaSQL); err != nil {
		return fmt.Errorf("ensure ci/cd run watermark schema: %w", err)
	}
	return nil
}

// Load returns the stored watermark for key, regardless of which generation
// wrote it.
func (s CICDRunWatermarkStore) Load(
	ctx context.Context,
	key runwatermark.Key,
) (runwatermark.Watermark, bool, error) {
	if s.db == nil {
		return runwatermark.Watermark{}, false, fmt.Errorf("ci/cd run watermark database is required")
	}
	if err := key.Validate(); err != nil {
		return runwatermark.Watermark{}, false, err
	}
	rows, err := s.db.QueryContext(ctx, loadCICDRunWatermarkQuery, key.ScopeID, key.Repository)
	if err != nil {
		return runwatermark.Watermark{}, false, fmt.Errorf("load ci/cd run watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return runwatermark.Watermark{}, false, fmt.Errorf("load ci/cd run watermark: %w", err)
		}
		return runwatermark.Watermark{}, false, nil
	}
	var lastRunID, generationID string
	var fencingToken int64
	var updatedAt time.Time
	if err := rows.Scan(&lastRunID, &generationID, &fencingToken, &updatedAt); err != nil {
		return runwatermark.Watermark{}, false, fmt.Errorf("scan ci/cd run watermark: %w", err)
	}
	if err := rows.Err(); err != nil {
		return runwatermark.Watermark{}, false, fmt.Errorf("load ci/cd run watermark: %w", err)
	}
	return runwatermark.Watermark{
		Key:          key,
		LastRunID:    lastRunID,
		GenerationID: generationID,
		FencingToken: fencingToken,
		UpdatedAt:    updatedAt.UTC(),
	}, true, nil
}

// Save upserts a watermark. A fencing token strictly older than the stored
// row's is rejected with runwatermark.ErrStaleFence; a fencing token equal
// to the stored row's succeeds (idempotent redelivery).
func (s CICDRunWatermarkStore) Save(ctx context.Context, value runwatermark.Watermark) error {
	if s.db == nil {
		return fmt.Errorf("ci/cd run watermark database is required")
	}
	if err := value.Validate(); err != nil {
		return err
	}
	updatedAt := value.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = s.now()
	}
	result, err := s.db.ExecContext(
		ctx,
		saveCICDRunWatermarkQuery,
		value.Key.ScopeID,
		value.Key.Repository,
		value.GenerationID,
		value.FencingToken,
		value.LastRunID,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("save ci/cd run watermark: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read ci/cd run watermark mutation result: %w", err)
	}
	if rowsAffected == 0 {
		return runwatermark.ErrStaleFence
	}
	return nil
}

func (s CICDRunWatermarkStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

var _ runwatermark.Store = CICDRunWatermarkStore{}
