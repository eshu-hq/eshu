// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// Default and maximum scope counts of ListTerminalFailures.
const (
	defaultTerminalFailureListLimit = 100
	maxTerminalFailureListLimit     = 500
)

// TerminalFailureListFilter names the one collector instance whose unresolved
// terminal failures ListTerminalFailures returns, one page at a time. Limit
// counts scopes (default 100, at most 500); a scope can list two items, one
// unacknowledged and one acknowledged. Cursor is the last scope id of the
// previous page.
type TerminalFailureListFilter struct {
	CollectorKind string
	InstanceID    string
	Limit         int
	Cursor        string
}

// TerminalFailureListItem is one unresolved terminal failure of a scope. Class
// and operation are bounded tokens, never a failure message. AcknowledgedAt is
// zero unless Acknowledged.
type TerminalFailureListItem struct {
	ScopeID          string
	FailureClass     string
	FailureOperation string
	FailedAt         time.Time
	WorkItemID       string
	Acknowledged     bool
	AcknowledgedAt   time.Time
}

// TerminalFailureListPage is one page of ListTerminalFailures, ordered by scope
// id then unacknowledged before acknowledged. NextCursor is set exactly when
// Truncated is.
type TerminalFailureListPage struct {
	Items      []TerminalFailureListItem
	Truncated  bool
	NextCursor string
}

// terminalFailureListQuery lists the unresolved terminal failures of one
// instance per scope, inside the instance's window ($3 is the cutoff, bound from
// terminalFailureCutoff in Go). It is the status read's two laterals unrolled
// per scope, built from the same shared predicates: the failed_terminal window
// filter seeks workflow_work_items_failed_terminal_instance_updated_idx,
// workflowTerminalFailureResolvedBy decides "resolved", and
// workflowTerminalFailureAckCovers decides "acknowledged". So the
// acknowledged=false items are what the status read's u lateral considers, and
// the acknowledged=true items what its a lateral considers; the status rows are
// the newest of each set.
//
// The completed probe carries one extra bound of its own, s.updated_at >= $3.
// Any completed row that can resolve an in-window row is itself inside the
// window, and without the bound the planner hashes every completed row of the
// instance: 35 ms against 0.3 ms on the 542,880-row fixture for an instance
// with 115,200 completed rows, a scan of its full history.
//
// DISTINCT ON collapses each (scope, acknowledged) group to its newest row, so a
// target that fails every cycle lists once. The page is the first $5 scope ids
// after the cursor ($4), so a scope's two items never split across pages.
const terminalFailureListQuery = `
WITH cand AS MATERIALIZED (
    SELECT
        t.scope_id,
        t.last_failure_class,
        t.last_failure_operation,
        t.updated_at,
        t.work_item_id,
        k.acknowledged_at IS NOT NULL AS acknowledged,
        k.acknowledged_at
    FROM workflow_work_items AS t
    LEFT JOIN LATERAL (
        SELECT k.acknowledged_at
        FROM collector_terminal_failure_acknowledgements AS k
        WHERE ` + workflowTerminalFailureAckCovers + `
        LIMIT 1
    ) AS k ON TRUE
    WHERE t.collector_kind = $1
      AND t.collector_instance_id = $2
      AND t.status = 'failed_terminal'
      AND t.updated_at >= $3
      AND t.scope_id > $4
      AND NOT EXISTS (
          SELECT 1
          FROM workflow_work_items AS s
          WHERE ` + workflowTerminalFailureResolvedBy + `
            AND s.updated_at >= $3
      )
), page AS (
    SELECT DISTINCT scope_id FROM cand ORDER BY scope_id LIMIT $5
)
SELECT DISTINCT ON (c.scope_id, c.acknowledged)
    c.scope_id,
    COALESCE(c.last_failure_class, ''),
    COALESCE(c.last_failure_operation, ''),
    c.updated_at,
    c.work_item_id,
    c.acknowledged,
    c.acknowledged_at
FROM cand AS c
WHERE c.scope_id IN (SELECT scope_id FROM page)
ORDER BY c.scope_id ASC, c.acknowledged ASC, c.updated_at DESC, c.work_item_id ASC
`

// ListTerminalFailures returns one page of the unresolved terminal failures of
// one collector instance, per scope, with whether an operator acknowledged each.
// It is the discovery step before an acknowledgement: the scope ids it returns
// are the ones the acknowledge route accepts, because both reads share the same
// window, resolved, and cover predicates. An unknown, kind-mismatched, disabled,
// or deactivated instance returns an empty page: a read is not a decision.
func (s *TerminalFailureAckStore) ListTerminalFailures(ctx context.Context, filter TerminalFailureListFilter) (TerminalFailureListPage, error) {
	if s == nil || s.db == nil {
		return TerminalFailureListPage{}, errors.New("terminal failure acknowledgement store is not configured")
	}
	kind := strings.TrimSpace(filter.CollectorKind)
	instanceID := strings.TrimSpace(filter.InstanceID)
	if kind == "" || instanceID == "" {
		return TerminalFailureListPage{}, errors.New("collector kind and instance id are required")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultTerminalFailureListLimit
	}
	limit = min(limit, maxTerminalFailureListLimit)

	instances, err := listCoordinatorCollectorInstances(ctx, s.db)
	if err != nil {
		return TerminalFailureListPage{}, err
	}
	var scanInterval time.Duration
	var extensionHosted, found bool
	for _, instance := range instances {
		if instance.InstanceID == instanceID && instance.CollectorKind == kind &&
			instance.Enabled && instance.DeactivatedAt.IsZero() {
			scanInterval, extensionHosted, found = instance.ScanInterval, instance.ExtensionHosted, true
			break
		}
	}
	if !found {
		return TerminalFailureListPage{}, nil
	}
	cutoff := terminalFailureCutoff(s.now(), scanInterval)
	rows, err := s.db.QueryContext(ctx, terminalFailureListQuery, kind, instanceID, cutoff, filter.Cursor, limit+1)
	if err != nil {
		return TerminalFailureListPage{}, fmt.Errorf("list collector terminal failures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var page TerminalFailureListPage
	lastScope, scopes := "", 0
	for rows.Next() {
		var (
			item           TerminalFailureListItem
			rawClass       string
			operation      string
			acknowledgedAt *time.Time
		)
		if err := rows.Scan(&item.ScopeID, &rawClass, &operation, &item.FailedAt, &item.WorkItemID,
			&item.Acknowledged, &acknowledgedAt); err != nil {
			return TerminalFailureListPage{}, fmt.Errorf("list collector terminal failures: %w", err)
		}
		if item.ScopeID != lastScope {
			scopes++
			lastScope = item.ScopeID
		}
		if scopes > limit {
			// The extra scope only proves there is a next page.
			page.Truncated = true
			continue
		}
		item.FailureClass = collector.BoundedFailureClassForInstance(kind, rawClass, extensionHosted)
		item.FailureOperation = collector.BoundedFailureOperation(operation)
		item.FailedAt = item.FailedAt.UTC()
		if acknowledgedAt != nil {
			item.AcknowledgedAt = acknowledgedAt.UTC()
		}
		page.Items = append(page.Items, item)
		page.NextCursor = item.ScopeID
	}
	if err := rows.Err(); err != nil {
		return TerminalFailureListPage{}, fmt.Errorf("list collector terminal failures: %w", err)
	}
	if !page.Truncated {
		page.NextCursor = ""
	}
	return page, nil
}
