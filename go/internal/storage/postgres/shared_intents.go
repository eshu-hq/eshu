package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	// sharedIntentBatchSize is the number of rows per multi-row INSERT batch.
	// 500 rows * 11 columns = 5500 parameters per query, well under the
	// Postgres limit of 65535.
	sharedIntentBatchSize = 500

	// columnsPerSharedIntent is the number of columns in the
	// shared_projection_intents INSERT.
	columnsPerSharedIntent = 11
)

// preparedRow holds marshaled data for one shared intent row before batching.
type preparedRow struct {
	intentID         string
	projectionDomain string
	partitionKey     string
	scopeID          string
	acceptanceUnitID string
	repositoryID     string
	sourceRunID      string
	generationID     string
	payloadBytes     []byte
	createdAt        time.Time
	completedAt      any
}

const sharedIntentSchemaSQL = `
CREATE TABLE IF NOT EXISTS shared_projection_intents (
    intent_id TEXT PRIMARY KEY,
    projection_domain TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    scope_id TEXT NOT NULL DEFAULT '',
    acceptance_unit_id TEXT NOT NULL DEFAULT '',
    repository_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL
);
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS scope_id TEXT NOT NULL DEFAULT '';
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS acceptance_unit_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS shared_projection_intents_repo_run_idx
    ON shared_projection_intents (repository_id, source_run_id, projection_domain, created_at);
CREATE INDEX IF NOT EXISTS shared_projection_intents_acceptance_lookup_idx
    ON shared_projection_intents (scope_id, acceptance_unit_id, source_run_id, projection_domain, created_at);
CREATE INDEX IF NOT EXISTS shared_projection_intents_pending_idx
    ON shared_projection_intents (projection_domain, completed_at, created_at);

CREATE TABLE IF NOT EXISTS shared_projection_partition_leases (
    projection_domain TEXT NOT NULL,
    partition_id INTEGER NOT NULL,
    partition_count INTEGER NOT NULL,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (projection_domain, partition_id, partition_count)
);
`

// SharedIntentSchemaSQL returns the DDL for shared projection intent tables.
func SharedIntentSchemaSQL() string {
	return sharedIntentSchemaSQL
}

const upsertSharedIntentBatchPrefix = `
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, payload, created_at, completed_at
) VALUES `

const upsertSharedIntentBatchSuffix = `
ON CONFLICT (intent_id) DO UPDATE
SET projection_domain = EXCLUDED.projection_domain,
    partition_key = EXCLUDED.partition_key,
    scope_id = EXCLUDED.scope_id,
    acceptance_unit_id = EXCLUDED.acceptance_unit_id,
    repository_id = EXCLUDED.repository_id,
    source_run_id = EXCLUDED.source_run_id,
    generation_id = EXCLUDED.generation_id,
    payload = EXCLUDED.payload,
    created_at = EXCLUDED.created_at,
    completed_at = COALESCE(
        shared_projection_intents.completed_at,
        EXCLUDED.completed_at
    )
`

const listSharedIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE repository_id = $1
  AND source_run_id = $2
  AND ($3 = '' OR projection_domain = $3)
ORDER BY created_at ASC, intent_id ASC
LIMIT $4
`

const listPendingDomainIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE projection_domain = $1
  AND completed_at IS NULL
ORDER BY created_at ASC, intent_id ASC
LIMIT $2
`

const markIntentsCompletedSQL = `
UPDATE shared_projection_intents
SET completed_at = $1
WHERE intent_id = ANY($2)
`

const listPendingAcceptanceUnitIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE scope_id = $1
  AND acceptance_unit_id = $2
  AND source_run_id = $3
  AND projection_domain = $4
  AND completed_at IS NULL
ORDER BY created_at ASC, intent_id ASC
LIMIT $5
`

const listAcceptanceUnitDomainIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE acceptance_unit_id = $1
  AND projection_domain = $2
ORDER BY created_at ASC, intent_id ASC
LIMIT $3
`

const claimPartitionLeaseSQL = `
INSERT INTO shared_projection_partition_leases (
    projection_domain, partition_id, partition_count,
    lease_owner, lease_expires_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (projection_domain, partition_id, partition_count) DO UPDATE
SET lease_owner = EXCLUDED.lease_owner,
    lease_expires_at = EXCLUDED.lease_expires_at,
    updated_at = EXCLUDED.updated_at
WHERE shared_projection_partition_leases.lease_expires_at IS NULL
   OR shared_projection_partition_leases.lease_expires_at <= $6
   OR shared_projection_partition_leases.lease_owner = $4
RETURNING projection_domain
`

const releasePartitionLeaseSQL = `
UPDATE shared_projection_partition_leases
SET lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = $5
WHERE projection_domain = $1
  AND partition_id = $2
  AND partition_count = $3
  AND lease_owner = $4
`

// SharedIntentFilter specifies query parameters for listing shared intents.
type SharedIntentFilter struct {
	RepositoryID     string
	SourceRunID      string
	ProjectionDomain *string
	Limit            int
}

// SharedIntentStore persists shared projection intents in PostgreSQL.
type SharedIntentStore struct {
	db ExecQueryer
}

// NewSharedIntentStore creates a shared intent store backed by the given
// database.
func NewSharedIntentStore(db ExecQueryer) *SharedIntentStore {
	return &SharedIntentStore{db: db}
}

// EnsureSchema applies the shared projection intent DDL.
func (s *SharedIntentStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, sharedIntentSchemaSQL)
	return err
}

// UpsertIntents inserts or updates shared projection intents using batched
// multi-row INSERT statements. Each batch inserts up to sharedIntentBatchSize
// rows in a single query, reducing round trips to the database.
func (s *SharedIntentStore) UpsertIntents(ctx context.Context, rows []reducer.SharedProjectionIntentRow) error {
	if len(rows) == 0 {
		return nil
	}

	rows = deduplicateSharedIntentRows(rows)

	// Marshal all payloads upfront
	prepared := make([]preparedRow, 0, len(rows))
	for _, r := range rows {
		payloadBytes, err := json.Marshal(r.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		var completedAt any
		if r.CompletedAt != nil {
			completedAt = *r.CompletedAt
		}

		prepared = append(prepared, preparedRow{
			intentID:         r.IntentID,
			projectionDomain: r.ProjectionDomain,
			partitionKey:     r.PartitionKey,
			scopeID:          sharedIntentScopeID(r),
			acceptanceUnitID: sharedIntentAcceptanceUnitID(r),
			repositoryID:     r.RepositoryID,
			sourceRunID:      r.SourceRunID,
			generationID:     r.GenerationID,
			payloadBytes:     payloadBytes,
			createdAt:        r.CreatedAt,
			completedAt:      completedAt,
		})
	}

	// Process in batches
	for i := 0; i < len(prepared); i += sharedIntentBatchSize {
		end := i + sharedIntentBatchSize
		if end > len(prepared) {
			end = len(prepared)
		}
		if err := upsertSharedIntentBatch(ctx, s.db, prepared[i:end]); err != nil {
			return err
		}
	}

	return nil
}

func deduplicateSharedIntentRows(rows []reducer.SharedProjectionIntentRow) []reducer.SharedProjectionIntentRow {
	if len(rows) < 2 {
		return rows
	}

	seen := make(map[string]struct{}, len(rows))
	deduplicated := make([]reducer.SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.IntentID]; exists {
			continue
		}
		seen[row.IntentID] = struct{}{}
		deduplicated = append(deduplicated, row)
	}

	return deduplicated
}

// upsertSharedIntentBatch inserts one batch of shared intents using a multi-row INSERT query.
func upsertSharedIntentBatch(ctx context.Context, db ExecQueryer, batch []preparedRow) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*columnsPerSharedIntent)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerSharedIntent
		fmt.Fprintf(&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10, offset+11,
		)

		args = append(args,
			row.intentID,
			row.projectionDomain,
			row.partitionKey,
			row.scopeID,
			row.acceptanceUnitID,
			row.repositoryID,
			row.sourceRunID,
			row.generationID,
			row.payloadBytes,
			row.createdAt,
			row.completedAt,
		)
	}

	query := upsertSharedIntentBatchPrefix + values.String() + upsertSharedIntentBatchSuffix

	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert shared intent batch (%d intents): %w", len(batch), err)
	}

	return nil
}

// ListIntents returns persisted intents for one repository/run pair.
func (s *SharedIntentStore) ListIntents(ctx context.Context, f SharedIntentFilter) ([]reducer.SharedProjectionIntentRow, error) {
	limit := max(f.Limit, 1)

	projDomain := ""
	if f.ProjectionDomain != nil {
		projDomain = *f.ProjectionDomain
	}

	sqlRows, err := s.db.QueryContext(ctx, listSharedIntentsSQL,
		f.RepositoryID,
		f.SourceRunID,
		projDomain,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanSharedIntentRows(sqlRows)
}

// ListPendingDomainIntents returns uncompleted intents for one projection
// domain.
func (s *SharedIntentStore) ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]reducer.SharedProjectionIntentRow, error) {
	l := max(limit, 1)

	sqlRows, err := s.db.QueryContext(ctx, listPendingDomainIntentsSQL, domain, l)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanSharedIntentRows(sqlRows)
}

// MarkIntentsCompleted marks intents as completed by setting completed_at.
func (s *SharedIntentStore) MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) error {
	if len(intentIDs) == 0 {
		return nil
	}

	_, err := s.db.ExecContext(ctx, markIntentsCompletedSQL, completedAt, intentIDs)
	if err != nil {
		return err
	}

	return nil
}

// ClaimPartitionLease attempts to claim a partition lease. Returns true if the
// lease was successfully claimed, false if it is held by another worker.
func (s *SharedIntentStore) ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string, leaseTTL time.Duration) (bool, error) {
	now := time.Now().UTC()
	leaseExpiresAt := now.Add(leaseTTL)

	rows, err := s.db.QueryContext(ctx, claimPartitionLeaseSQL,
		domain,
		partitionID,
		partitionCount,
		leaseOwner,
		leaseExpiresAt,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("claim partition lease: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, nil
	}

	var projectionDomain string
	if err := rows.Scan(&projectionDomain); err != nil {
		return false, fmt.Errorf("scan lease claim result: %w", err)
	}

	return true, rows.Err()
}

// ReleasePartitionLease releases a partition lease owned by the given worker.
func (s *SharedIntentStore) ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string) error {
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, releasePartitionLeaseSQL,
		domain,
		partitionID,
		partitionCount,
		leaseOwner,
		now,
	)
	if err != nil {
		return fmt.Errorf("release partition lease: %w", err)
	}

	return nil
}

// ListPendingAcceptanceUnitIntents lists uncompleted intents for one bounded
// freshness key and projection domain.
func (s *SharedIntentStore) ListPendingAcceptanceUnitIntents(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	domain string,
	limit int,
) ([]reducer.SharedProjectionIntentRow, error) {
	l := max(limit, 1)

	sqlRows, err := s.db.QueryContext(
		ctx,
		listPendingAcceptanceUnitIntentsSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		domain,
		l,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanSharedIntentRows(sqlRows)
}

// ListAcceptanceUnitDomainIntents lists all intents, including already
// completed ones, for one acceptance unit and projection domain. Repo-owned
// execution uses this full slice to reconstruct the authoritative snapshot
// across multiple contributing scopes before retracting repo-wide edges.
func (s *SharedIntentStore) ListAcceptanceUnitDomainIntents(
	ctx context.Context,
	acceptanceUnitID string,
	domain string,
	limit int,
) ([]reducer.SharedProjectionIntentRow, error) {
	l := max(limit, 1)

	sqlRows, err := s.db.QueryContext(
		ctx,
		listAcceptanceUnitDomainIntentsSQL,
		acceptanceUnitID,
		domain,
		l,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanSharedIntentRows(sqlRows)
}

func scanSharedIntentRows(rows Rows) ([]reducer.SharedProjectionIntentRow, error) {
	var result []reducer.SharedProjectionIntentRow
	for rows.Next() {
		var r reducer.SharedProjectionIntentRow
		var payloadBytes []byte
		var completedAt sql.NullTime
		if err := rows.Scan(
			&r.IntentID,
			&r.ProjectionDomain,
			&r.PartitionKey,
			&r.ScopeID,
			&r.AcceptanceUnitID,
			&r.RepositoryID,
			&r.SourceRunID,
			&r.GenerationID,
			&payloadBytes,
			&r.CreatedAt,
			&completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan shared intent: %w", err)
		}
		if len(payloadBytes) > 0 {
			if err := json.Unmarshal(payloadBytes, &r.Payload); err != nil {
				return nil, fmt.Errorf("unmarshal payload: %w", err)
			}
		}
		if completedAt.Valid {
			r.CompletedAt = &completedAt.Time
		}
		result = append(result, r)
	}

	return result, rows.Err()
}

// sharedIntentScopeID keeps the storage layer compatible while reducer callers
// migrate to explicit Option B identity fields.
func sharedIntentScopeID(row reducer.SharedProjectionIntentRow) string {
	if value := strings.TrimSpace(row.ScopeID); value != "" {
		return value
	}
	return sharedIntentPayloadString(row.Payload, "scope_id")
}

// sharedIntentAcceptanceUnitID falls back to repository identity so the current
// Git collector path remains stable while the broader bounded-unit contract
// lands across the reducer.
func sharedIntentAcceptanceUnitID(row reducer.SharedProjectionIntentRow) string {
	if value := strings.TrimSpace(row.AcceptanceUnitID); value != "" {
		return value
	}
	if value := sharedIntentPayloadString(row.Payload, "acceptance_unit_id"); value != "" {
		return value
	}
	return row.RepositoryID
}

func sharedIntentPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

// sharedIntentBootstrapDefinition returns the schema definition for the
// bootstrap registry.
func sharedIntentBootstrapDefinition() Definition {
	return Definition{
		Name: "shared_projection_intents",
		Path: "schema/data-plane/postgres/008_shared_projection_intents.sql",
		SQL:  sharedIntentSchemaSQL,
	}
}

// init registers the shared intent schema in the bootstrap definitions.
func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, sharedIntentBootstrapDefinition())
}
