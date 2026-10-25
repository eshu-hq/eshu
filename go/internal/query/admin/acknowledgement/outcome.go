// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"time"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// outcomeFields is the number of strings encodeOutcome stores in the idempotency
// ledger's outcome id list.
const outcomeFields = 8

// encodeOutcome flattens a recorded acknowledgement into the string list the
// replay idempotency ledger stores for a completed key, in this order: collector
// kind, instance id, scope id, work item id, bounded class, bounded operation,
// failure time, acknowledgement time. A duplicate request replays it.
func encodeOutcome(result pgstorage.TerminalFailureAckResult) []string {
	return []string{
		result.CollectorKind, result.InstanceID, result.ScopeID, result.WorkItemID,
		result.FailureClass, result.FailureOperation,
		result.FailureAt.UTC().Format(time.RFC3339Nano),
		result.AcknowledgedAt.UTC().Format(time.RFC3339Nano),
	}
}

// decodeOutcome reverses encodeOutcome. It reports false for a ledger row that
// does not carry the encoding (an older or foreign row), so the caller falls back
// to a bare duplicate answer.
func decodeOutcome(fields []string) (pgstorage.TerminalFailureAckResult, bool) {
	if len(fields) != outcomeFields {
		return pgstorage.TerminalFailureAckResult{}, false
	}
	failureAt, err := time.Parse(time.RFC3339Nano, fields[6])
	if err != nil {
		return pgstorage.TerminalFailureAckResult{}, false
	}
	acknowledgedAt, err := time.Parse(time.RFC3339Nano, fields[7])
	if err != nil {
		return pgstorage.TerminalFailureAckResult{}, false
	}
	return pgstorage.TerminalFailureAckResult{
		TerminalFailureAckTarget: pgstorage.TerminalFailureAckTarget{
			CollectorKind: fields[0], InstanceID: fields[1], ScopeID: fields[2], WorkItemID: fields[3],
			FailureClass: fields[4], FailureOperation: fields[5], FailureAt: failureAt,
		},
		AcknowledgedAt: acknowledgedAt,
	}, true
}

// responseBody renders the 200 body. It carries the bounded class and
// operation, the failure and acknowledgement times, and the acknowledged work
// item, never the operator's reason (an admin reads it from the query route).
func responseBody(status, idempotencyKey string, result pgstorage.TerminalFailureAckResult) map[string]any {
	return map[string]any{
		"status":          status,
		"idempotency_key": idempotencyKey,
		"acknowledgement": map[string]any{
			"collector_kind":    result.CollectorKind,
			"instance_id":       result.InstanceID,
			"scope_id":          result.ScopeID,
			"work_item_id":      result.WorkItemID,
			"failure_class":     result.FailureClass,
			"failure_operation": result.FailureOperation,
			"failed_at":         result.FailureAt.UTC().Format(time.RFC3339),
			"acknowledged_at":   result.AcknowledgedAt.UTC().Format(time.RFC3339),
		},
	}
}
