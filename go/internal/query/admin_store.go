package query

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// NewPostgresAdminStore constructs an AdminStore backed by Postgres.
func NewPostgresAdminStore(db *sql.DB) AdminStore {
	if db == nil {
		return nil
	}

	sqlDB := pgstatus.SQLDB{DB: db}
	return &postgresAdminStore{
		db:        sqlDB,
		decisions: pgstatus.NewDecisionStore(sqlDB),
		now:       func() time.Time { return time.Now().UTC() },
	}
}

type postgresAdminStore struct {
	db        pgstatus.ExecQueryer
	decisions *pgstatus.DecisionStore
	now       func() time.Time
}

func (s *postgresAdminStore) ListWorkItems(ctx context.Context, f WorkItemFilter) ([]AdminWorkItem, error) {
	query, args := buildListWorkItemsQuery(f)
	return scanAdminWorkItems(ctx, s.db, query, args...)
}

func (s *postgresAdminStore) DeadLetterWorkItems(ctx context.Context, f DeadLetterFilter) ([]AdminWorkItem, error) {
	now := s.time()
	query, args := buildMutatingWorkItemsQuery(f.WorkItemIDs, f.ScopeID, f.Stage, f.FailureClass, f.Limit, 2, `
SET status = 'dead_letter',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    failure_class = COALESCE(NULLIF(work.failure_class, ''), 'operator_dead_letter'),
    failure_message = COALESCE(NULLIF(work.failure_message, ''), 'dead-lettered by operator'),
    failure_details = COALESCE(NULLIF($2, ''), work.failure_details),
    updated_at = $1
`)
	args = append([]any{now, strings.TrimSpace(f.OperatorNote)}, args...)
	return scanAdminWorkItems(ctx, s.db, query, args...)
}

func (s *postgresAdminStore) SkipRepositoryWorkItems(ctx context.Context, repoID string, note string) ([]AdminWorkItem, error) {
	now := s.time()
	const query = `
WITH selected AS (
    SELECT work.work_item_id
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id
    WHERE scope.scope_id = $1 OR scope.source_key = $1
    ORDER BY work.updated_at DESC, work.work_item_id ASC
    LIMIT 100
), updated AS (
    UPDATE fact_work_items AS work
    SET status = 'dead_letter',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $2,
        failure_class = COALESCE(work.failure_class, 'operator_skipped'),
        failure_message = COALESCE(NULLIF(work.failure_message, ''), 'skipped by operator'),
        failure_details = COALESCE(NULLIF($3, ''), work.failure_details),
        updated_at = $2
    FROM selected
    WHERE work.work_item_id = selected.work_item_id
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
        work.visible_at
)
SELECT * FROM updated ORDER BY updated_at DESC, work_item_id ASC
`
	return scanAdminWorkItems(ctx, s.db, query, repoID, now, strings.TrimSpace(note))
}

func (s *postgresAdminStore) ReplayFailedWorkItems(ctx context.Context, f ReplayWorkItemFilter) ([]AdminWorkItem, error) {
	now := s.time()
	query, args := buildMutatingWorkItemsQuery(f.WorkItemIDs, f.ScopeID, f.Stage, f.FailureClass, f.Limit, 1, `
SET status = 'pending',
    attempt_count = GREATEST(work.attempt_count, 1),
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = $1
`)
	args = append([]any{now}, args...)
	items, err := scanAdminWorkItems(ctx, s.db, query, args...)
	if err != nil {
		return nil, err
	}
	if err := s.insertReplayEvents(ctx, items, strings.TrimSpace(f.OperatorNote), now); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *postgresAdminStore) RequestBackfill(ctx context.Context, input BackfillInput) (*AdminBackfillRequest, error) {
	now := s.time()
	id, err := newAdminID("backfill")
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
	if _, err := s.db.ExecContext(ctx, query, id, scopeID, generationID, operatorNote, now); err != nil {
		return nil, fmt.Errorf("insert backfill request: %w", err)
	}

	row := &AdminBackfillRequest{
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

func (s *postgresAdminStore) ListReplayEvents(ctx context.Context, f ReplayEventFilter) ([]AdminReplayEvent, error) {
	var builder strings.Builder
	builder.WriteString(`
SELECT replay_event_id, work_item_id, scope_id, generation_id, failure_class, operator_note, created_at
FROM fact_replay_events
WHERE 1=1
`)
	args := make([]any, 0, 4)
	if value := strings.TrimSpace(f.ScopeID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND scope_id = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.WorkItemID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND work_item_id = $%d\n", len(args))
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
	_, _ = fmt.Fprintf(&builder, " ORDER BY created_at DESC, replay_event_id DESC LIMIT $%d", len(args))

	rows, err := s.db.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list replay events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []AdminReplayEvent
	for rows.Next() {
		var event AdminReplayEvent
		var failureClass sql.NullString
		var operatorNote sql.NullString
		if err := rows.Scan(
			&event.ReplayEventID,
			&event.WorkItemID,
			&event.ScopeID,
			&event.GenerationID,
			&failureClass,
			&operatorNote,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan replay event: %w", err)
		}
		if failureClass.Valid {
			event.FailureClass = &failureClass.String
		}
		if operatorNote.Valid {
			event.OperatorNote = &operatorNote.String
		}
		events = append(events, event)
	}

	return events, rows.Err()
}

func (s *postgresAdminStore) ListDecisions(ctx context.Context, f DecisionQueryFilter) ([]AdminDecisionRow, error) {
	rows, err := s.decisions.ListDecisions(ctx, pgstatus.DecisionFilter{
		RepositoryID: f.RepositoryID,
		SourceRunID:  f.SourceRunID,
		DecisionType: f.DecisionType,
		Limit:        f.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}

	result := make([]AdminDecisionRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, AdminDecisionRow{
			DecisionID:        row.DecisionID,
			DecisionType:      row.DecisionType,
			RepositoryID:      row.RepositoryID,
			SourceRunID:       row.SourceRunID,
			WorkItemID:        row.WorkItemID,
			Subject:           row.Subject,
			ConfidenceScore:   row.ConfidenceScore,
			ConfidenceReason:  row.ConfidenceReason,
			ProvenanceSummary: row.ProvenanceSummary,
			CreatedAt:         row.CreatedAt,
		})
	}
	return result, nil
}

func (s *postgresAdminStore) ListEvidence(ctx context.Context, decisionID string) ([]AdminEvidenceRow, error) {
	rows, err := s.decisions.ListEvidence(ctx, decisionID)
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}

	result := make([]AdminEvidenceRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, AdminEvidenceRow{
			EvidenceID:   row.EvidenceID,
			DecisionID:   row.DecisionID,
			FactID:       row.FactID,
			EvidenceKind: row.EvidenceKind,
			Detail:       row.Detail,
			CreatedAt:    row.CreatedAt,
		})
	}
	return result, nil
}

func (s *postgresAdminStore) insertReplayEvents(ctx context.Context, items []AdminWorkItem, operatorNote string, now time.Time) error {
	if len(items) == 0 {
		return nil
	}

	const query = `
INSERT INTO fact_replay_events (
    replay_event_id,
    work_item_id,
    scope_id,
    generation_id,
    failure_class,
    operator_note,
    created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
`
	for _, item := range items {
		id, err := newAdminID("replay")
		if err != nil {
			return err
		}
		var failureClass any
		if item.FailureClass != nil {
			failureClass = *item.FailureClass
		}
		var note any
		if operatorNote != "" {
			note = operatorNote
		}
		if _, err := s.db.ExecContext(ctx, query, id, item.WorkItemID, item.ScopeID, item.GenerationID, failureClass, note, now); err != nil {
			return fmt.Errorf("insert replay event: %w", err)
		}
	}
	return nil
}

func buildListWorkItemsQuery(f WorkItemFilter) (string, []any) {
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
    visible_at
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

func buildMutatingWorkItemsQuery(
	workItemIDs []string,
	scopeID, stage, failureClass string,
	limit int,
	baseArgCount int,
	updateClause string,
) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
WITH selected AS (
    SELECT work_item_id
    FROM fact_work_items
    WHERE status IN ('dead_letter', 'failed')
`)
	args := make([]any, 0, 5)
	if len(workItemIDs) > 0 {
		args = append(args, workItemIDs)
		_, _ = fmt.Fprintf(&builder, "      AND work_item_id = ANY($%d)\n", len(args)+baseArgCount)
	}
	if value := strings.TrimSpace(scopeID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, "      AND scope_id = $%d\n", len(args)+baseArgCount)
	}
	if value := strings.TrimSpace(stage); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, "      AND stage = $%d\n", len(args)+baseArgCount)
	}
	if value := strings.TrimSpace(failureClass); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, "      AND failure_class = $%d\n", len(args)+baseArgCount)
	}
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	_, _ = fmt.Fprintf(&builder, "    ORDER BY updated_at DESC, work_item_id ASC LIMIT $%d\n", len(args)+baseArgCount)
	builder.WriteString(`), updated AS (
    UPDATE fact_work_items AS work
`)
	builder.WriteString(updateClause)
	builder.WriteString(`
    FROM selected
    WHERE work.work_item_id = selected.work_item_id
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
        work.visible_at
)
SELECT * FROM updated ORDER BY updated_at DESC, work_item_id ASC
`)
	return builder.String(), args
}

func scanAdminWorkItems(ctx context.Context, db pgstatus.ExecQueryer, query string, args ...any) ([]AdminWorkItem, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminWorkItem
	for rows.Next() {
		var item AdminWorkItem
		var leaseOwner sql.NullString
		var failureClass sql.NullString
		var failureMessage sql.NullString
		var visibleAt sql.NullTime
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
		); err != nil {
			return nil, fmt.Errorf("scan work item: %w", err)
		}
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

func newAdminID(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixNano(), hex.EncodeToString(raw[:])), nil
}

func (s *postgresAdminStore) time() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}
