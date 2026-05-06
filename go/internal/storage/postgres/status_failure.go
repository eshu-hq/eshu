package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const latestQueueFailureQuery = `
SELECT stage,
       domain,
       status,
       work_item_id,
       scope_id,
       generation_id,
       COALESCE(failure_class, '') AS failure_class,
       COALESCE(failure_message, '') AS failure_message,
       COALESCE(failure_details, '') AS failure_details,
       updated_at
FROM fact_work_items
WHERE status IN ('retrying', 'failed', 'dead_letter')
  AND (
    NULLIF(BTRIM(COALESCE(failure_class, '')), '') IS NOT NULL
    OR NULLIF(BTRIM(COALESCE(failure_message, '')), '') IS NOT NULL
    OR NULLIF(BTRIM(COALESCE(failure_details, '')), '') IS NOT NULL
  )
ORDER BY updated_at DESC, work_item_id ASC
LIMIT 1
`

func readLatestQueueFailure(
	ctx context.Context,
	queryer Queryer,
) (*statuspkg.QueueFailureSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, latestQueueFailureQuery)
	if err != nil {
		return nil, fmt.Errorf("read latest queue failure: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read latest queue failure: %w", err)
		}
		return nil, nil
	}

	var snapshot statuspkg.QueueFailureSnapshot
	var updatedAt sql.NullTime
	if scanErr := rows.Scan(
		&snapshot.Stage,
		&snapshot.Domain,
		&snapshot.Status,
		&snapshot.WorkItemID,
		&snapshot.ScopeID,
		&snapshot.GenerationID,
		&snapshot.FailureClass,
		&snapshot.FailureMessage,
		&snapshot.FailureDetails,
		&updatedAt,
	); scanErr != nil {
		return nil, fmt.Errorf("read latest queue failure: %w", scanErr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read latest queue failure: %w", err)
	}
	if updatedAt.Valid {
		snapshot.UpdatedAt = updatedAt.Time.UTC()
	}

	snapshot.Stage = strings.TrimSpace(snapshot.Stage)
	snapshot.Domain = strings.TrimSpace(snapshot.Domain)
	snapshot.Status = strings.TrimSpace(snapshot.Status)
	snapshot.WorkItemID = strings.TrimSpace(snapshot.WorkItemID)
	snapshot.ScopeID = strings.TrimSpace(snapshot.ScopeID)
	snapshot.GenerationID = strings.TrimSpace(snapshot.GenerationID)
	snapshot.FailureClass = strings.TrimSpace(snapshot.FailureClass)
	snapshot.FailureMessage = strings.TrimSpace(snapshot.FailureMessage)
	snapshot.FailureDetails = strings.TrimSpace(snapshot.FailureDetails)

	return &snapshot, nil
}
