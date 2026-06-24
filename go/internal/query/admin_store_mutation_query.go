// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

func buildMutatingWorkItemsQuery(
	workItemIDs []string,
	scopeID, stage, failureClass string,
	limit int,
	baseArgCount int,
	updateClause string,
	excludeFailureClasses ...string,
) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
WITH selected AS (
    SELECT work_item_id
    FROM fact_work_items
    WHERE status IN ('dead_letter', 'failed')
`)
	args := make([]any, 0, 6)
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
	if len(excludeFailureClasses) > 0 {
		args = append(args, excludeFailureClasses)
		// Skip unsafe-to-replay classes; NULL failure_class is never excluded.
		_, _ = fmt.Fprintf(&builder, "      AND (failure_class IS NULL OR failure_class <> ALL($%d))\n", len(args)+baseArgCount)
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
