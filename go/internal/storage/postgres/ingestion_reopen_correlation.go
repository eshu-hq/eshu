// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// listSucceededReducerWorkItemsByDomainQuery selects succeeded reducer work
// items for one domain. It is the domain-parameterized form of the
// deployment_mapping / code_import_repo_edge listings, used by the generic
// correlation reopen below.
const listSucceededReducerWorkItemsByDomainQuery = `
SELECT work_item_id
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = $1
  AND status = 'succeeded'
ORDER BY updated_at ASC, work_item_id ASC
`

// ReopenSucceededReducerWorkItems replays succeeded reducer work items for the
// given domains so they re-run once the cross-scope facts, resolved
// relationships, or canonical nodes they depend on — produced by an earlier
// drain plus the relationship maintenance — exist. It generalizes the
// deployment_mapping and code_import_repo_edge reopens for additive correlation
// domains (e.g. deployable_unit_correlation, which consumes resolved DEPLOYS_FROM
// relationships and has no readiness retry of its own). Reopen is idempotent and
// only transitions rows whose status is still 'succeeded'.
func (s IngestionStore) ReopenSucceededReducerWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	domains []string,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_correlation_work_items")
		defer span.End()
	}

	queue := ReducerQueue{db: s.db, Now: s.Now}
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		workItemIDs, err := listSucceededReducerWorkItemIDsForDomain(ctx, s.db, domain)
		if err != nil {
			return err
		}
		for _, workItemID := range workItemIDs {
			if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
				return fmt.Errorf("reopen %s work items: %w", domain, err)
			}
		}
		log.Printf("reducer_work_items_reopened domain=%s count=%d", domain, len(workItemIDs))
	}

	return nil
}

func listSucceededReducerWorkItemIDsForDomain(
	ctx context.Context,
	queryer Queryer,
	domain string,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, listSucceededReducerWorkItemsByDomainQuery, domain)
	if err != nil {
		return nil, fmt.Errorf("list succeeded %s work items: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()

	workItemIDs := make([]string, 0)
	for rows.Next() {
		var workItemID string
		if err := rows.Scan(&workItemID); err != nil {
			return nil, fmt.Errorf("scan succeeded %s work item: %w", domain, err)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		workItemIDs = append(workItemIDs, workItemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded %s work items: %w", domain, err)
	}
	return workItemIDs, nil
}
