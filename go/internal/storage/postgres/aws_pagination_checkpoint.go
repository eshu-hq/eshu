package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/checkpoint"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const awsPaginationCheckpointSchemaSQL = `
CREATE TABLE IF NOT EXISTS aws_scan_pagination_checkpoints (
    collector_instance_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    region TEXT NOT NULL,
    service_kind TEXT NOT NULL,
    resource_parent TEXT NOT NULL DEFAULT '',
    operation TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    page_token TEXT NOT NULL DEFAULT '',
    page_number INTEGER NOT NULL DEFAULT 0,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (collector_instance_id, account_id, region, service_kind, resource_parent, operation)
);

CREATE INDEX IF NOT EXISTS aws_scan_pagination_checkpoints_scope_idx
    ON aws_scan_pagination_checkpoints (
        collector_instance_id,
        account_id,
        region,
        service_kind,
        generation_id,
        updated_at DESC
    );
`

const loadAWSPaginationCheckpointQuery = `
SELECT page_token, page_number, payload, updated_at
FROM aws_scan_pagination_checkpoints
WHERE collector_instance_id = $1
  AND account_id = $2
  AND region = $3
  AND service_kind = $4
  AND resource_parent = $5
  AND operation = $6
  AND generation_id = $7
  AND fencing_token <= $8
`

const saveAWSPaginationCheckpointQuery = `
INSERT INTO aws_scan_pagination_checkpoints (
    collector_instance_id,
    account_id,
    region,
    service_kind,
    resource_parent,
    operation,
    generation_id,
    fencing_token,
    page_token,
    page_number,
    payload,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12
)
ON CONFLICT (collector_instance_id, account_id, region, service_kind, resource_parent, operation) DO UPDATE SET
    generation_id = EXCLUDED.generation_id,
    fencing_token = EXCLUDED.fencing_token,
    page_token = EXCLUDED.page_token,
    page_number = EXCLUDED.page_number,
    payload = EXCLUDED.payload,
    updated_at = EXCLUDED.updated_at
WHERE aws_scan_pagination_checkpoints.fencing_token <= EXCLUDED.fencing_token
`

const completeAWSPaginationCheckpointQuery = `
DELETE FROM aws_scan_pagination_checkpoints
WHERE collector_instance_id = $1
  AND account_id = $2
  AND region = $3
  AND service_kind = $4
  AND resource_parent = $5
  AND operation = $6
  AND generation_id = $7
  AND fencing_token <= $8
`

const expireStaleAWSPaginationCheckpointsQuery = `
DELETE FROM aws_scan_pagination_checkpoints
WHERE collector_instance_id = $1
  AND account_id = $2
  AND region = $3
  AND service_kind = $4
  AND generation_id <> $5
  AND fencing_token <= $6
`

// AWSPaginationCheckpointStore persists claim-fenced AWS pagination tokens in
// Postgres.
type AWSPaginationCheckpointStore struct {
	db          ExecQueryer
	Now         func() time.Time
	Instruments *telemetry.Instruments
}

// NewAWSPaginationCheckpointStore constructs a checkpoint store over the
// shared data-plane database.
func NewAWSPaginationCheckpointStore(db ExecQueryer) AWSPaginationCheckpointStore {
	return AWSPaginationCheckpointStore{db: db}
}

// AWSPaginationCheckpointSchemaSQL returns the DDL for AWS pagination
// checkpoint rows.
func AWSPaginationCheckpointSchemaSQL() string {
	return awsPaginationCheckpointSchemaSQL
}

func awsPaginationCheckpointBootstrapDefinition() Definition {
	return Definition{
		Name: "aws_pagination_checkpoints",
		Path: "schema/data-plane/postgres/018_aws_pagination_checkpoints.sql",
		SQL:  awsPaginationCheckpointSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, awsPaginationCheckpointBootstrapDefinition())
}

// EnsureSchema applies the AWS pagination checkpoint DDL.
func (s AWSPaginationCheckpointStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("aws pagination checkpoint database is required")
	}
	_, err := s.db.ExecContext(ctx, awsPaginationCheckpointSchemaSQL)
	if err != nil {
		s.recordEvent(ctx, checkpoint.Scope{}, "", "failure", "error")
		return fmt.Errorf("ensure AWS pagination checkpoint schema: %w", err)
	}
	return nil
}

// Load returns the retry-safe page marker for one operation and generation.
func (s AWSPaginationCheckpointStore) Load(ctx context.Context, key checkpoint.Key) (checkpoint.Checkpoint, bool, error) {
	if s.db == nil {
		return checkpoint.Checkpoint{}, false, fmt.Errorf("aws pagination checkpoint database is required")
	}
	if err := key.Validate(); err != nil {
		return checkpoint.Checkpoint{}, false, err
	}
	rows, err := s.db.QueryContext(ctx, loadAWSPaginationCheckpointQuery, checkpointKeyArgs(key)...)
	if err != nil {
		s.recordEvent(ctx, key.Scope, key.Operation, "failure", "error")
		return checkpoint.Checkpoint{}, false, fmt.Errorf("load AWS pagination checkpoint: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			s.recordEvent(ctx, key.Scope, key.Operation, "failure", "error")
			return checkpoint.Checkpoint{}, false, fmt.Errorf("load AWS pagination checkpoint: %w", err)
		}
		s.recordEvent(ctx, key.Scope, key.Operation, "load", "miss")
		return checkpoint.Checkpoint{}, false, nil
	}
	var pageToken string
	var pageNumber int
	var rawPayload []byte
	var updatedAt time.Time
	if err := rows.Scan(&pageToken, &pageNumber, &rawPayload, &updatedAt); err != nil {
		s.recordEvent(ctx, key.Scope, key.Operation, "failure", "error")
		return checkpoint.Checkpoint{}, false, fmt.Errorf("scan AWS pagination checkpoint: %w", err)
	}
	if err := rows.Err(); err != nil {
		s.recordEvent(ctx, key.Scope, key.Operation, "failure", "error")
		return checkpoint.Checkpoint{}, false, fmt.Errorf("load AWS pagination checkpoint: %w", err)
	}
	payload, err := decodeCheckpointPayload(rawPayload)
	if err != nil {
		s.recordEvent(ctx, key.Scope, key.Operation, "failure", "error")
		return checkpoint.Checkpoint{}, false, err
	}
	s.recordEvent(ctx, key.Scope, key.Operation, "load", "hit")
	s.recordEvent(ctx, key.Scope, key.Operation, "resume", "success")
	return checkpoint.Checkpoint{
		Key:        key,
		PageToken:  pageToken,
		PageNumber: pageNumber,
		Payload:    payload,
		UpdatedAt:  updatedAt.UTC(),
	}, true, nil
}

// Save upserts a retry-safe page marker. Older fencing tokens cannot overwrite
// newer claim-owned checkpoint rows.
func (s AWSPaginationCheckpointStore) Save(ctx context.Context, value checkpoint.Checkpoint) error {
	if s.db == nil {
		return fmt.Errorf("aws pagination checkpoint database is required")
	}
	if err := value.Validate(); err != nil {
		return err
	}
	updatedAt := value.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = s.now()
	}
	rawPayload, err := encodeCheckpointPayload(value.Payload)
	if err != nil {
		return err
	}
	args := checkpointKeyArgs(value.Key)
	args = append(args, strings.TrimSpace(value.PageToken), value.PageNumber, rawPayload, updatedAt)
	result, err := s.db.ExecContext(ctx, saveAWSPaginationCheckpointQuery, args...)
	if err != nil {
		s.recordEvent(ctx, value.Key.Scope, value.Key.Operation, "failure", "error")
		return fmt.Errorf("save AWS pagination checkpoint: %w", err)
	}
	if err := validateCheckpointMutation(result); err != nil {
		s.recordEvent(ctx, value.Key.Scope, value.Key.Operation, "failure", "stale_fence")
		return err
	}
	s.recordEvent(ctx, value.Key.Scope, value.Key.Operation, "save", "success")
	return nil
}

// Complete removes the checkpoint for a completed paginated operation.
func (s AWSPaginationCheckpointStore) Complete(ctx context.Context, key checkpoint.Key) error {
	if s.db == nil {
		return fmt.Errorf("aws pagination checkpoint database is required")
	}
	if err := key.Validate(); err != nil {
		return err
	}
	args := checkpointKeyArgs(key)
	result, err := s.db.ExecContext(ctx, completeAWSPaginationCheckpointQuery, args...)
	if err != nil {
		s.recordEvent(ctx, key.Scope, key.Operation, "failure", "error")
		return fmt.Errorf("complete AWS pagination checkpoint: %w", err)
	}
	if err := validateCheckpointMutation(result); err != nil {
		s.recordEvent(ctx, key.Scope, key.Operation, "failure", "stale_fence")
		return err
	}
	s.recordEvent(ctx, key.Scope, key.Operation, "save", "complete")
	return nil
}

// ExpireStale removes prior-generation checkpoints for the same AWS claim
// boundary. The fencing guard prevents an expired worker from deleting newer
// claim state.
func (s AWSPaginationCheckpointStore) ExpireStale(ctx context.Context, scope checkpoint.Scope) (int64, error) {
	if s.db == nil {
		return 0, fmt.Errorf("aws pagination checkpoint database is required")
	}
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	result, err := s.db.ExecContext(
		ctx,
		expireStaleAWSPaginationCheckpointsQuery,
		scope.CollectorInstanceID,
		scope.AccountID,
		scope.Region,
		scope.ServiceKind,
		scope.GenerationID,
		scope.FencingToken,
	)
	if err != nil {
		s.recordEvent(ctx, scope, "all", "failure", "error")
		return 0, fmt.Errorf("expire stale AWS pagination checkpoints: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.recordEvent(ctx, scope, "all", "failure", "error")
		return 0, fmt.Errorf("expire stale AWS pagination checkpoints: %w", err)
	}
	if rowsAffected > 0 {
		s.recordEvent(ctx, scope, "all", "expire", "success")
	}
	return rowsAffected, nil
}

func checkpointKeyArgs(key checkpoint.Key) []any {
	return []any{
		key.Scope.CollectorInstanceID,
		key.Scope.AccountID,
		key.Scope.Region,
		key.Scope.ServiceKind,
		strings.TrimSpace(key.ResourceParent),
		strings.TrimSpace(key.Operation),
		key.Scope.GenerationID,
		key.Scope.FencingToken,
	}
}

func validateCheckpointMutation(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read AWS pagination checkpoint mutation result: %w", err)
	}
	if rowsAffected == 0 {
		return checkpoint.ErrStaleFence
	}
	return nil
}

func encodeCheckpointPayload(payload map[string]any) ([]byte, error) {
	if len(payload) == 0 {
		return []byte(`{}`), nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode AWS pagination checkpoint payload: %w", err)
	}
	return raw, nil
}

func decodeCheckpointPayload(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode AWS pagination checkpoint payload: %w", err)
	}
	return payload, nil
}

func (s AWSPaginationCheckpointStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s AWSPaginationCheckpointStore) recordEvent(
	ctx context.Context,
	scope checkpoint.Scope,
	operation string,
	eventKind string,
	result string,
) {
	if s.Instruments == nil {
		return
	}
	if strings.TrimSpace(scope.ServiceKind) == "" {
		scope.ServiceKind = "unknown"
	}
	if strings.TrimSpace(scope.AccountID) == "" {
		scope.AccountID = "unknown"
	}
	if strings.TrimSpace(scope.Region) == "" {
		scope.Region = "unknown"
	}
	if strings.TrimSpace(operation) == "" {
		operation = "unknown"
	}
	if strings.TrimSpace(result) == "" {
		result = "unknown"
	}
	if strings.TrimSpace(eventKind) == "" {
		eventKind = "unknown"
	}
	s.Instruments.AWSCheckpointEvents.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrService(scope.ServiceKind),
		telemetry.AttrAccount(scope.AccountID),
		telemetry.AttrRegion(scope.Region),
		telemetry.AttrOperation(operation),
		telemetry.AttrEventKind(eventKind),
		telemetry.AttrResult(result),
	))
}

var _ checkpoint.Store = AWSPaginationCheckpointStore{}
