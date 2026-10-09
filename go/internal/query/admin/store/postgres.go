// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/decisions"
)

// NewStore constructs an admin.Store backed by Postgres.
func NewStore(database *sql.DB) admin.Store {
	if database == nil {
		return nil
	}

	sqlDB := pgstatus.SQLDB{DB: database}
	return &postgresStore{
		database:  sqlDB,
		beginner:  sqlDB,
		decisions: decisionsstore.NewDecisionStore(sqlDB),
		now:       func() time.Time { return time.Now().UTC() },
	}
}

type postgresStore struct {
	database db.ExecQueryer
	// beginner opens the lock_timeout transaction the reopen path needs.
	// It is always set by NewStore; tests that build the struct directly
	// leave it nil and cannot run a reopen.
	beginner  db.Beginner
	decisions *decisionsstore.DecisionStore
	now       func() time.Time
}

// deadLetterNoteDetailsSQL and skipNoteDetailsSQL are the failure_details
// assignment of the two operator-note statements, over the note parameter each
// one binds ($2 and $3). An empty note leaves the row's details as they are; a
// note becomes the details and the row's current failure evidence rides under
// prior_failure, the same fold the supersede writers use (#7388). The row only
// holds the details it failed with: dead-letter acts on failed and dead_letter
// rows, skip on unleased pending, retrying and failed rows. Dead-lettering a row
// that is already dead_letter with a second note folds the first note's row under
// prior_failure again, so each repeat nests one level deeper; nothing is lost,
// and unlike the reclaim UPDATEs this path has no keep-as-is rule for a row it
// already rewrote. Skip cannot reach a dead_letter row.
const (
	deadLetterNoteDetailsSQL = `CASE
        WHEN NULLIF($2, '') IS NULL THEN work.failure_details
        ELSE (jsonb_build_object('operator_note', $2::text) || ` + pgstatus.PriorFailureWorkSQL + `)::text
    END`
	skipNoteDetailsSQL = `CASE
            WHEN NULLIF($3, '') IS NULL THEN work.failure_details
            ELSE (jsonb_build_object('operator_note', $3::text) || ` + pgstatus.PriorFailureWorkSQL + `)::text
        END`
)

func (s *postgresStore) ListWorkItems(ctx context.Context, f admin.WorkItemFilter) ([]admin.WorkItem, error) {
	return s.reader().ListWorkItems(ctx, f)
}

// DeadLetterWorkItems moves terminal failures to dead letter while preserving
// required identity status authorizations and newer worker claims.
func (s *postgresStore) DeadLetterWorkItems(ctx context.Context, f admin.DeadLetterFilter) ([]admin.WorkItem, error) {
	now := s.time()
	query, args := buildMutatingWorkItemsQuery(f.WorkItemIDs, f.ScopeID, f.Stage, f.FailureClass, f.Limit, 2, false, false, `
SET status = 'dead_letter',
    container_image_identity_v2_authorized_status = CASE
        WHEN work.container_image_identity_v2_required THEN 'dead_letter' ELSE ''
    END,
    container_image_identity_v3_authorized_status = CASE
        WHEN work.container_image_identity_v3_required THEN 'dead_letter' ELSE ''
    END,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    failure_class = COALESCE(NULLIF(work.failure_class, ''), 'operator_dead_letter'),
    failure_message = COALESCE(NULLIF(work.failure_message, ''), 'dead-lettered by operator'),
    failure_details = `+deadLetterNoteDetailsSQL+`,
    updated_at = $1
`)
	args = append([]any{now, strings.TrimSpace(f.OperatorNote)}, args...)
	return scanWorkItems(ctx, s.database, query, args...)
}

// SkipRepositoryWorkItems dead-letters only unclaimed, actionable rows for one
// repository or scope and rechecks their status after the target row is locked.
// The selector resolves through the shared skip/reopen resolver first: an
// unknown selector skips nothing, and an ambiguous one fails closed (#7732),
// so the UPDATE below filters on the exact resolved scope id and can never
// reach a second scope, even one inserted after the resolve.
func (s *postgresStore) SkipRepositoryWorkItems(ctx context.Context, repoID string, note string) ([]admin.WorkItem, error) {
	scopeID, err := s.resolveScopeID(ctx, repoID)
	if err != nil {
		if errors.Is(err, admin.ErrScopeSelectorNotFound) {
			return nil, nil
		}
		return nil, err
	}
	now := s.time()
	const query = `
WITH selected AS (
    SELECT work.work_item_id, work.status AS selected_status
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id
    WHERE scope.scope_id = $1
      AND work.status IN ('pending', 'retrying', 'failed')
    ORDER BY work.updated_at DESC, work.work_item_id ASC
    LIMIT 100
), updated AS (
    UPDATE fact_work_items AS work
    SET status = 'dead_letter',
        container_image_identity_v2_authorized_status = CASE
            WHEN work.container_image_identity_v2_required THEN 'dead_letter' ELSE ''
        END,
        container_image_identity_v3_authorized_status = CASE
            WHEN work.container_image_identity_v3_required THEN 'dead_letter' ELSE ''
        END,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $2,
        failure_class = COALESCE(NULLIF(work.failure_class, ''), 'operator_skipped'),
        failure_message = COALESCE(NULLIF(work.failure_message, ''), 'skipped by operator'),
        failure_details = ` + skipNoteDetailsSQL + `,
        updated_at = $2
    FROM selected
    WHERE work.work_item_id = selected.work_item_id
      -- Recheck the locked row against both the selected state and eligibility.
      AND work.status = selected.selected_status
      AND work.status IN ('pending', 'retrying', 'failed')
    RETURNING
        work.work_item_id,
        work.scope_id,
        work.generation_id,
        work.stage,
        work.domain,
        work.status,
        work.attempt_count,
        work.lease_owner,
        work.failure_class,
        work.failure_message,
        work.created_at,
        work.updated_at,
        work.visible_at,
        work.failure_details
)
SELECT * FROM updated ORDER BY updated_at DESC, work_item_id ASC
`
	return scanWorkItems(ctx, s.database, query, scopeID, now, strings.TrimSpace(note))
}

func (s *postgresStore) RequestBackfill(ctx context.Context, input admin.BackfillInput) (*admin.BackfillRequest, error) {
	now := s.time()
	id, err := newStoreID("backfill")
	if err != nil {
		return nil, err
	}

	var scopeID any
	if value := strings.TrimSpace(input.ScopeID); value != "" {
		scopeID = value
	}
	var generationID any
	if value := strings.TrimSpace(input.GenerationID); value != "" {
		generationID = value
	}
	var operatorNote any
	if value := strings.TrimSpace(input.OperatorNote); value != "" {
		operatorNote = value
	}

	const query = `
INSERT INTO fact_backfill_requests (
    backfill_request_id,
    scope_id,
    generation_id,
    operator_note,
    created_at
) VALUES ($1, $2, $3, $4, $5)
`
	if _, err := s.database.ExecContext(ctx, query, id, scopeID, generationID, operatorNote, now); err != nil {
		return nil, fmt.Errorf("insert backfill request: %w", err)
	}

	row := &admin.BackfillRequest{
		BackfillRequestID: id,
		CreatedAt:         now,
	}
	if value, ok := scopeID.(string); ok {
		row.ScopeID = &value
	}
	if value, ok := generationID.(string); ok {
		row.GenerationID = &value
	}
	if value, ok := operatorNote.(string); ok {
		row.OperatorNote = &value
	}
	return row, nil
}

func (s *postgresStore) ListReplayEvents(ctx context.Context, f admin.ReplayEventFilter) ([]admin.ReplayEvent, error) {
	return s.reader().ListReplayEvents(ctx, f)
}

func (s *postgresStore) ListDecisions(ctx context.Context, f admin.DecisionQueryFilter) ([]admin.DecisionRow, error) {
	return s.reader().ListDecisions(ctx, f)
}

func (s *postgresStore) ListEvidence(ctx context.Context, decisionID string) ([]admin.EvidenceRow, error) {
	return s.reader().ListEvidence(ctx, decisionID)
}

func buildListWorkItemsQuery(f admin.WorkItemFilter) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
SELECT
    work_item_id,
    scope_id,
    generation_id,
    stage,
    domain,
    status,
    attempt_count,
    lease_owner,
    failure_class,
    failure_message,
    created_at,
    updated_at,
    visible_at,
    failure_details
FROM fact_work_items
WHERE 1=1
`)
	args := make([]any, 0, 5)
	if len(f.Statuses) > 0 {
		args = append(args, f.Statuses)
		_, _ = fmt.Fprintf(&builder, " AND status = ANY($%d)\n", len(args))
	}
	if value := strings.TrimSpace(f.ScopeID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND scope_id = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.Stage); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND stage = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.FailureClass); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND failure_class = $%d\n", len(args))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	_, _ = fmt.Fprintf(&builder, " ORDER BY updated_at DESC, work_item_id ASC LIMIT $%d", len(args))
	return builder.String(), args
}

func scanWorkItems(ctx context.Context, database db.Queryer, query string, args ...any) ([]admin.WorkItem, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []admin.WorkItem
	for rows.Next() {
		var item admin.WorkItem
		var leaseOwner sql.NullString
		var failureClass sql.NullString
		var failureMessage sql.NullString
		var visibleAt sql.NullTime
		var failureDetails sql.NullString
		if err := rows.Scan(
			&item.WorkItemID,
			&item.ScopeID,
			&item.GenerationID,
			&item.Stage,
			&item.Domain,
			&item.Status,
			&item.AttemptCount,
			&leaseOwner,
			&failureClass,
			&failureMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
			&visibleAt,
			&failureDetails,
		); err != nil {
			return nil, fmt.Errorf("scan work item: %w", err)
		}
		applyWorkItemDetails(&item, failureDetails)
		if leaseOwner.Valid {
			item.LeaseOwner = &leaseOwner.String
		}
		if failureClass.Valid {
			item.FailureClass = &failureClass.String
		}
		if failureMessage.Valid {
			item.FailureMessage = &failureMessage.String
		}
		if visibleAt.Valid {
			item.VisibleAt = &visibleAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func newStoreID(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixNano(), hex.EncodeToString(raw[:])), nil
}

func (s *postgresStore) time() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *postgresStore) reader() *postgresReadStore {
	return &postgresReadStore{database: s.database, decisions: decisionsstore.NewDecisionReader(s.database)}
}
