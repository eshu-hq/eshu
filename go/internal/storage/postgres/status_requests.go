package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/runtime"
)

const controlSchemaSQL = `
CREATE TABLE IF NOT EXISTS runtime_ingester_control (
    ingester TEXT PRIMARY KEY,
    scan_request_status TEXT NOT NULL DEFAULT 'idle',
    scan_request_requested_at TIMESTAMPTZ,
    scan_request_claimed_at TIMESTAMPTZ,
    scan_request_completed_at TIMESTAMPTZ,
    scan_request_error TEXT,
    reindex_request_status TEXT NOT NULL DEFAULT 'idle',
    reindex_request_requested_at TIMESTAMPTZ,
    reindex_request_claimed_at TIMESTAMPTZ,
    reindex_request_completed_at TIMESTAMPTZ,
    reindex_request_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

const requestScanQuery = `
INSERT INTO runtime_ingester_control (ingester, scan_request_status, scan_request_requested_at, updated_at)
VALUES ($1, 'pending', $2, $2)
ON CONFLICT (ingester) DO UPDATE
SET scan_request_status = 'pending',
    scan_request_requested_at = $2,
    scan_request_claimed_at = NULL,
    scan_request_completed_at = NULL,
    scan_request_error = NULL,
    updated_at = $2
`

const claimScanQuery = `
UPDATE runtime_ingester_control
SET scan_request_status = 'running',
    scan_request_claimed_at = $2,
    updated_at = $2
WHERE ingester = $1
  AND scan_request_status = 'pending'
RETURNING ingester, scan_request_status, scan_request_requested_at, scan_request_claimed_at
`

const completeScanQuery = `
UPDATE runtime_ingester_control
SET scan_request_status = CASE WHEN $3 = '' THEN 'completed' ELSE 'failed' END,
    scan_request_completed_at = $2,
    scan_request_error = NULLIF($3, ''),
    updated_at = $2
WHERE ingester = $1
  AND scan_request_status = 'running'
`

const requestReindexQuery = `
INSERT INTO runtime_ingester_control (ingester, reindex_request_status, reindex_request_requested_at, updated_at)
VALUES ($1, 'pending', $2, $2)
ON CONFLICT (ingester) DO UPDATE
SET reindex_request_status = 'pending',
    reindex_request_requested_at = $2,
    reindex_request_claimed_at = NULL,
    reindex_request_completed_at = NULL,
    reindex_request_error = NULL,
    updated_at = $2
`

const claimReindexQuery = `
UPDATE runtime_ingester_control
SET reindex_request_status = 'running',
    reindex_request_claimed_at = $2,
    updated_at = $2
WHERE ingester = $1
  AND reindex_request_status = 'pending'
RETURNING ingester, reindex_request_status, reindex_request_requested_at, reindex_request_claimed_at
`

const completeReindexQuery = `
UPDATE runtime_ingester_control
SET reindex_request_status = CASE WHEN $3 = '' THEN 'completed' ELSE 'failed' END,
    reindex_request_completed_at = $2,
    reindex_request_error = NULLIF($3, ''),
    updated_at = $2
WHERE ingester = $1
  AND reindex_request_status = 'running'
`

const getScanStateQuery = `
SELECT ingester, scan_request_status,
       COALESCE(scan_request_requested_at, '0001-01-01'::timestamptz),
       COALESCE(scan_request_claimed_at, '0001-01-01'::timestamptz),
       COALESCE(scan_request_completed_at, '0001-01-01'::timestamptz),
       COALESCE(scan_request_error, '')
FROM runtime_ingester_control
WHERE ingester = $1
`

const getReindexStateQuery = `
SELECT ingester, reindex_request_status,
       COALESCE(reindex_request_requested_at, '0001-01-01'::timestamptz),
       COALESCE(reindex_request_claimed_at, '0001-01-01'::timestamptz),
       COALESCE(reindex_request_completed_at, '0001-01-01'::timestamptz),
       COALESCE(reindex_request_error, '')
FROM runtime_ingester_control
WHERE ingester = $1
`

// StatusRequestStore implements runtime.StatusRequestStore over Postgres.
type StatusRequestStore struct {
	db ExecQueryer
}

// NewStatusRequestStore constructs a Postgres-backed status request store.
func NewStatusRequestStore(db ExecQueryer) StatusRequestStore {
	return StatusRequestStore{db: db}
}

// RequestScan transitions a scan request from idle to pending.
func (s StatusRequestStore) RequestScan(ctx context.Context, ingester string, now time.Time) error {
	if s.db == nil {
		return fmt.Errorf("status request store database is required")
	}
	_, err := s.db.ExecContext(ctx, requestScanQuery, ingester, now.UTC())
	if err != nil {
		return fmt.Errorf("request scan: %w", err)
	}
	return nil
}

// ClaimScanRequest transitions a pending scan to running.
func (s StatusRequestStore) ClaimScanRequest(ctx context.Context, ingester string, now time.Time) (runtime.ScanRequest, error) {
	if s.db == nil {
		return runtime.ScanRequest{}, fmt.Errorf("status request store database is required")
	}
	rows, err := s.db.QueryContext(ctx, claimScanQuery, ingester, now.UTC())
	if err != nil {
		return runtime.ScanRequest{}, fmt.Errorf("claim scan request: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return runtime.ScanRequest{}, fmt.Errorf("claim scan request: %w", err)
		}
		return runtime.ScanRequest{}, fmt.Errorf("no pending scan request for ingester %q", ingester)
	}

	var req runtime.ScanRequest
	var state string
	if err := rows.Scan(&req.Ingester, &state, &req.RequestedAt, &req.ClaimedAt); err != nil {
		return runtime.ScanRequest{}, fmt.Errorf("claim scan request: %w", err)
	}
	req.State = runtime.RequestState(state)
	return req, nil
}

// CompleteScanRequest transitions a running scan to completed or failed.
func (s StatusRequestStore) CompleteScanRequest(ctx context.Context, ingester string, now time.Time, scanErr string) error {
	if s.db == nil {
		return fmt.Errorf("status request store database is required")
	}
	_, err := s.db.ExecContext(ctx, completeScanQuery, ingester, now.UTC(), scanErr)
	if err != nil {
		return fmt.Errorf("complete scan request: %w", err)
	}
	return nil
}

// RequestReindex transitions a reindex request from idle to pending.
func (s StatusRequestStore) RequestReindex(ctx context.Context, ingester string, now time.Time) error {
	if s.db == nil {
		return fmt.Errorf("status request store database is required")
	}
	_, err := s.db.ExecContext(ctx, requestReindexQuery, ingester, now.UTC())
	if err != nil {
		return fmt.Errorf("request reindex: %w", err)
	}
	return nil
}

// ClaimReindexRequest transitions a pending reindex to running.
func (s StatusRequestStore) ClaimReindexRequest(ctx context.Context, ingester string, now time.Time) (runtime.ReindexRequest, error) {
	if s.db == nil {
		return runtime.ReindexRequest{}, fmt.Errorf("status request store database is required")
	}
	rows, err := s.db.QueryContext(ctx, claimReindexQuery, ingester, now.UTC())
	if err != nil {
		return runtime.ReindexRequest{}, fmt.Errorf("claim reindex request: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return runtime.ReindexRequest{}, fmt.Errorf("claim reindex request: %w", err)
		}
		return runtime.ReindexRequest{}, fmt.Errorf("no pending reindex request for ingester %q", ingester)
	}

	var req runtime.ReindexRequest
	var state string
	if err := rows.Scan(&req.Ingester, &state, &req.RequestedAt, &req.ClaimedAt); err != nil {
		return runtime.ReindexRequest{}, fmt.Errorf("claim reindex request: %w", err)
	}
	req.State = runtime.RequestState(state)
	return req, nil
}

// CompleteReindexRequest transitions a running reindex to completed or failed.
func (s StatusRequestStore) CompleteReindexRequest(ctx context.Context, ingester string, now time.Time, reindexErr string) error {
	if s.db == nil {
		return fmt.Errorf("status request store database is required")
	}
	_, err := s.db.ExecContext(ctx, completeReindexQuery, ingester, now.UTC(), reindexErr)
	if err != nil {
		return fmt.Errorf("complete reindex request: %w", err)
	}
	return nil
}

// GetScanState returns the current scan request state for one ingester.
func (s StatusRequestStore) GetScanState(ctx context.Context, ingester string) (runtime.ScanRequest, error) {
	if s.db == nil {
		return runtime.ScanRequest{}, fmt.Errorf("status request store database is required")
	}
	rows, err := s.db.QueryContext(ctx, getScanStateQuery, ingester)
	if err != nil {
		return runtime.ScanRequest{}, fmt.Errorf("get scan state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return runtime.ScanRequest{}, fmt.Errorf("get scan state: %w", err)
		}
		return runtime.ScanRequest{Ingester: ingester, State: runtime.RequestStateIdle}, nil
	}

	var req runtime.ScanRequest
	var state string
	if err := rows.Scan(&req.Ingester, &state, &req.RequestedAt, &req.ClaimedAt, &req.CompletedAt, &req.Error); err != nil {
		return runtime.ScanRequest{}, fmt.Errorf("get scan state: %w", err)
	}
	req.State = runtime.RequestState(state)
	return req, nil
}

// GetReindexState returns the current reindex request state for one ingester.
func (s StatusRequestStore) GetReindexState(ctx context.Context, ingester string) (runtime.ReindexRequest, error) {
	if s.db == nil {
		return runtime.ReindexRequest{}, fmt.Errorf("status request store database is required")
	}
	rows, err := s.db.QueryContext(ctx, getReindexStateQuery, ingester)
	if err != nil {
		return runtime.ReindexRequest{}, fmt.Errorf("get reindex state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return runtime.ReindexRequest{}, fmt.Errorf("get reindex state: %w", err)
		}
		return runtime.ReindexRequest{Ingester: ingester, State: runtime.RequestStateIdle}, nil
	}

	var req runtime.ReindexRequest
	var state string
	if err := rows.Scan(&req.Ingester, &state, &req.RequestedAt, &req.ClaimedAt, &req.CompletedAt, &req.Error); err != nil {
		return runtime.ReindexRequest{}, fmt.Errorf("get reindex state: %w", err)
	}
	req.State = runtime.RequestState(state)
	return req, nil
}

// Ensure StatusRequestStore satisfies the interface at compile time.
var _ runtime.StatusRequestStore = StatusRequestStore{}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, Definition{
		Name: "runtime_ingester_control",
		Path: "schema/data-plane/postgres/009_runtime_ingester_control.sql",
		SQL:  controlSchemaSQL,
	})
}
