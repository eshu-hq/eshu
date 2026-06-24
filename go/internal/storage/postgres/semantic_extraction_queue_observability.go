// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// ObservabilitySnapshot returns audit-safe semantic extraction queue, budget,
// and enablement aggregates for operator status surfaces.
func (s SemanticExtractionQueueStore) ObservabilitySnapshot(
	ctx context.Context,
) (statuspkg.SemanticExtractionStatus, error) {
	if s.db == nil {
		return statuspkg.SemanticExtractionStatus{}, fmt.Errorf("semantic extraction queue store db is required")
	}
	return readSemanticExtractionObservability(ctx, s.db)
}

func readSemanticExtractionObservability(
	ctx context.Context,
	queryer Queryer,
) (statuspkg.SemanticExtractionStatus, error) {
	rows, err := queryer.QueryContext(ctx, semanticExtractionObservabilityQuery)
	if err != nil {
		return statuspkg.SemanticExtractionStatus{}, fmt.Errorf("read semantic extraction observability: %w", err)
	}
	defer func() { _ = rows.Close() }()

	snapshot := statuspkg.DefaultSemanticExtractionStatus()
	for rows.Next() {
		row := semanticExtractionObservabilityRow{}
		if err := rows.Scan(
			&row.status,
			&row.sourceClass,
			&row.providerKind,
			&row.providerProfileID,
			&row.providerProfileClass,
			&row.policyState,
			&row.policyReason,
			&row.guardState,
			&row.guardReason,
			&row.actorClass,
			&row.aclState,
			&row.failureClass,
			&row.budgetState,
			&row.budgetReason,
			&row.budgetUnit,
			&row.count,
			&row.estimatedInputTokens,
			&row.estimatedOutputTokens,
			&row.estimatedCostMicros,
			&row.actualInputTokens,
			&row.actualOutputTokens,
			&row.actualCostMicros,
			&row.remainingTokens,
			&row.remainingCostMicros,
			&row.updatedAt,
		); err != nil {
			return statuspkg.SemanticExtractionStatus{}, fmt.Errorf("scan semantic extraction observability: %w", err)
		}
		applySemanticExtractionObservabilityRow(&snapshot, row)
	}
	if err := rows.Err(); err != nil {
		return statuspkg.SemanticExtractionStatus{}, fmt.Errorf("iterate semantic extraction observability: %w", err)
	}
	report := statuspkg.BuildReport(
		statuspkg.RawSnapshot{SemanticExtraction: snapshot},
		statuspkg.DefaultOptions(),
	)
	return report.SemanticExtraction, nil
}

type semanticExtractionObservabilityRow struct {
	status                string
	sourceClass           string
	providerKind          string
	providerProfileID     string
	providerProfileClass  string
	policyState           string
	policyReason          string
	guardState            string
	guardReason           string
	actorClass            string
	aclState              string
	failureClass          string
	budgetState           string
	budgetReason          string
	budgetUnit            string
	count                 int64
	estimatedInputTokens  int64
	estimatedOutputTokens int64
	estimatedCostMicros   int64
	actualInputTokens     int64
	actualOutputTokens    int64
	actualCostMicros      int64
	remainingTokens       int64
	remainingCostMicros   int64
	updatedAt             time.Time
}

func applySemanticExtractionObservabilityRow(
	snapshot *statuspkg.SemanticExtractionStatus,
	row semanticExtractionObservabilityRow,
) {
	count := int(row.count)
	if count <= 0 {
		return
	}
	snapshot.Queue.Total += count
	snapshot.Queue.StatusCounts = appendNamedCount(snapshot.Queue.StatusCounts, row.status, count)
	snapshot.Queue.SourceClassCounts = appendNamedCount(snapshot.Queue.SourceClassCounts, row.sourceClass, count)
	snapshot.Queue.FailureClassCounts = appendNamedCount(snapshot.Queue.FailureClassCounts, row.failureClass, count)
	snapshot.Queue.ProviderProfileCounts = append(snapshot.Queue.ProviderProfileCounts,
		statuspkg.SemanticExtractionProviderProfileQueueCount{
			ProviderKind:         row.providerKind,
			ProviderProfileID:    row.providerProfileID,
			ProviderProfileClass: row.providerProfileClass,
			Count:                count,
		})
	snapshot.Queue.PolicyDecisionCounts = append(snapshot.Queue.PolicyDecisionCounts,
		statuspkg.SemanticExtractionDecisionCount{
			State:  row.policyState,
			Reason: row.policyReason,
			Count:  count,
		})
	snapshot.Queue.GuardDecisionCounts = append(snapshot.Queue.GuardDecisionCounts,
		statuspkg.SemanticExtractionDecisionCount{
			State:  row.guardState,
			Reason: row.guardReason,
			Count:  count,
		})
	applySemanticExtractionQueueStatus(&snapshot.Queue, row.status, count)
	applySemanticExtractionBudget(snapshot, row, count)
	snapshot.Audit.ActorClassCounts = appendNamedCount(snapshot.Audit.ActorClassCounts, row.actorClass, count)
	snapshot.Audit.ACLStateCounts = appendNamedCount(snapshot.Audit.ACLStateCounts, row.aclState, count)
	if snapshot.Queue.UpdatedAt.Before(row.updatedAt) {
		snapshot.Queue.UpdatedAt = row.updatedAt
	}
	if snapshot.Audit.LastProcessedAt.Before(row.updatedAt) {
		snapshot.Audit.LastProcessedAt = row.updatedAt
	}
}

func applySemanticExtractionQueueStatus(
	snapshot *statuspkg.SemanticExtractionQueueSnapshot,
	status string,
	count int,
) {
	switch semanticqueue.Status(status) {
	case semanticqueue.StatusPending:
		snapshot.Pending += count
	case semanticqueue.StatusClaimed:
		snapshot.Claimed += count
	case semanticqueue.StatusRetrying:
		snapshot.Retrying += count
	case semanticqueue.StatusSucceeded:
		snapshot.Succeeded += count
	case semanticqueue.StatusDeadLetter:
		snapshot.DeadLetter += count
	case semanticqueue.StatusSkippedNoProvider:
		snapshot.Skipped += count
		snapshot.NoProvider += count
	case semanticqueue.StatusSkippedPolicy:
		snapshot.Skipped += count
		snapshot.PolicyDenied += count
	case semanticqueue.StatusSkippedBudget:
		snapshot.Skipped += count
		snapshot.BudgetExhausted += count
	case semanticqueue.StatusUnsafePayload:
		snapshot.Unsafe += count
	case semanticqueue.StatusProviderUnavailable:
		snapshot.ProviderUnavailable += count
	case semanticqueue.StatusSkippedUnchanged:
		snapshot.Skipped += count
		snapshot.Unchanged += count
	case semanticqueue.StatusStale:
		snapshot.Stale += count
	}
}

func applySemanticExtractionBudget(
	snapshot *statuspkg.SemanticExtractionStatus,
	row semanticExtractionObservabilityRow,
	count int,
) {
	snapshot.Budget.EstimatedInputTokens += row.estimatedInputTokens
	snapshot.Budget.EstimatedOutputTokens += row.estimatedOutputTokens
	snapshot.Budget.EstimatedCostMicros += row.estimatedCostMicros
	snapshot.Budget.ActualInputTokens += row.actualInputTokens
	snapshot.Budget.ActualOutputTokens += row.actualOutputTokens
	snapshot.Budget.ActualCostMicros += row.actualCostMicros
	snapshot.Budget.RemainingTokens += row.remainingTokens
	snapshot.Budget.RemainingCostMicros += row.remainingCostMicros
	if row.budgetState == semanticqueue.BudgetStateExhausted ||
		semanticqueue.Status(row.status) == semanticqueue.StatusSkippedBudget {
		snapshot.Budget.Exhausted += count
	}
	snapshot.Budget.DecisionCounts = append(snapshot.Budget.DecisionCounts,
		statuspkg.SemanticExtractionBudgetDecisionCount{
			State:      row.budgetState,
			Reason:     row.budgetReason,
			BudgetUnit: row.budgetUnit,
			Count:      count,
		})
}

func appendNamedCount(rows []statuspkg.NamedCount, name string, count int) []statuspkg.NamedCount {
	if name == "" || count <= 0 {
		return rows
	}
	return append(rows, statuspkg.NamedCount{Name: name, Count: count})
}

const semanticExtractionObservabilityQuery = `
WITH redacted AS (
    SELECT
        status,
        COALESCE(source_class, '') AS source_class,
        COALESCE(provider_kind, '') AS provider_kind,
        COALESCE(provider_profile_id, '') AS provider_profile_id,
        COALESCE(provider_profile_class, '') AS provider_profile_class,
        COALESCE(policy_state, '') AS policy_state,
        COALESCE(policy_reason, '') AS policy_reason,
        COALESCE(guard_state, '') AS guard_state,
        COALESCE(guard_reason, '') AS guard_reason,
        COALESCE(actor_class, '') AS actor_class,
        COALESCE(acl_state, '') AS acl_state,
        COALESCE(failure_class, '') AS failure_class,
        COALESCE(NULLIF(budget_metadata->>'state', ''), NULLIF(budget_metadata->>'State', ''), '') AS budget_state,
        COALESCE(NULLIF(budget_metadata->>'reason', ''), NULLIF(budget_metadata->>'Reason', ''), '') AS budget_reason,
        COALESCE(NULLIF(budget_metadata->>'budget_unit', ''), NULLIF(budget_metadata->>'BudgetUnit', ''), '') AS budget_unit,
        COALESCE(NULLIF(budget_metadata->>'estimated_input_tokens', '')::BIGINT, NULLIF(budget_metadata->>'EstimatedInputTokens', '')::BIGINT, 0) AS estimated_input_tokens,
        COALESCE(NULLIF(budget_metadata->>'estimated_output_tokens', '')::BIGINT, NULLIF(budget_metadata->>'EstimatedOutputTokens', '')::BIGINT, 0) AS estimated_output_tokens,
        COALESCE(NULLIF(budget_metadata->>'estimated_cost_micros', '')::BIGINT, NULLIF(budget_metadata->>'EstimatedCostMicros', '')::BIGINT, 0) AS estimated_cost_micros,
        COALESCE(NULLIF(budget_metadata->>'actual_input_tokens', '')::BIGINT, NULLIF(budget_metadata->>'ActualInputTokens', '')::BIGINT, 0) AS actual_input_tokens,
        COALESCE(NULLIF(budget_metadata->>'actual_output_tokens', '')::BIGINT, NULLIF(budget_metadata->>'ActualOutputTokens', '')::BIGINT, 0) AS actual_output_tokens,
        COALESCE(NULLIF(budget_metadata->>'actual_cost_micros', '')::BIGINT, NULLIF(budget_metadata->>'ActualCostMicros', '')::BIGINT, 0) AS actual_cost_micros,
        COALESCE(NULLIF(budget_metadata->>'remaining_tokens', '')::BIGINT, NULLIF(budget_metadata->>'RemainingTokens', '')::BIGINT, 0) AS remaining_tokens,
        COALESCE(NULLIF(budget_metadata->>'remaining_cost_micros', '')::BIGINT, NULLIF(budget_metadata->>'RemainingCostMicros', '')::BIGINT, 0) AS remaining_cost_micros,
        updated_at
    FROM semantic_extraction_jobs
)
SELECT
    status,
    source_class,
    provider_kind,
    provider_profile_id,
    provider_profile_class,
    policy_state,
    policy_reason,
    guard_state,
    guard_reason,
    actor_class,
    acl_state,
    failure_class,
    budget_state,
    budget_reason,
    budget_unit,
    COUNT(*)::BIGINT AS count,
    SUM(estimated_input_tokens)::BIGINT AS estimated_input_tokens,
    SUM(estimated_output_tokens)::BIGINT AS estimated_output_tokens,
    SUM(estimated_cost_micros)::BIGINT AS estimated_cost_micros,
    SUM(actual_input_tokens)::BIGINT AS actual_input_tokens,
    SUM(actual_output_tokens)::BIGINT AS actual_output_tokens,
    SUM(actual_cost_micros)::BIGINT AS actual_cost_micros,
    SUM(remaining_tokens)::BIGINT AS remaining_tokens,
    SUM(remaining_cost_micros)::BIGINT AS remaining_cost_micros,
    MAX(updated_at) AS updated_at
FROM redacted
GROUP BY
    status,
    source_class,
    provider_kind,
    provider_profile_id,
    provider_profile_class,
    policy_state,
    policy_reason,
    guard_state,
    guard_reason,
    actor_class,
    acl_state,
    failure_class,
    budget_state,
    budget_reason,
    budget_unit
ORDER BY status, source_class, provider_kind, provider_profile_id
`
