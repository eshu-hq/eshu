package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const enqueueWorkflowWorkItemValueFormat = "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, NULLIF($%d, ''), $%d, $%d, NULLIF($%d, ''), $%d, NULLIF($%d, ''), NULLIF($%d, '')::timestamptz, NULLIF($%d, '')::timestamptz, NULLIF($%d, '')::timestamptz, NULLIF($%d, '')::timestamptz, NULLIF($%d, ''), NULLIF($%d, ''), $%d, $%d)"

func (s *WorkflowControlStore) enqueueWorkItemBatch(ctx context.Context, items []workflow.WorkItem) error {
	args := make([]any, 0, len(items)*workflowColumnsPerWorkItem)
	var values strings.Builder

	for i, item := range items {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * workflowColumnsPerWorkItem
		fmt.Fprintf(
			&values,
			enqueueWorkflowWorkItemValueFormat,
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7, offset+8,
			offset+9, offset+10, offset+11, offset+12, offset+13, offset+14, offset+15,
			offset+16, offset+17, offset+18, offset+19, offset+20, offset+21, offset+22, offset+23,
		)
		args = append(args,
			item.WorkItemID,
			item.RunID,
			string(item.CollectorKind),
			item.CollectorInstanceID,
			item.SourceSystem,
			item.ScopeID,
			item.AcceptanceUnitID,
			item.SourceRunID,
			item.GenerationID,
			item.FairnessKey,
			string(item.Status),
			item.AttemptCount,
			item.CurrentClaimID,
			item.CurrentFencingToken,
			item.CurrentOwnerID,
			nullableRFC3339(item.LeaseExpiresAt),
			nullableRFC3339(item.VisibleAt),
			nullableRFC3339(item.LastClaimedAt),
			nullableRFC3339(item.LastCompletedAt),
			item.LastFailureClass,
			item.LastFailureMessage,
			item.CreatedAt.UTC(),
			item.UpdatedAt.UTC(),
		)
	}

	query := enqueueWorkflowWorkItemsPrefix + values.String() + enqueueWorkflowWorkItemsSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("enqueue workflow work item batch (%d items): %w", len(items), err)
	}
	return nil
}

func (s *WorkflowControlStore) execClaimMutation(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	query string,
	leaseExpiresAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	if err := validateClaimMutation(mutation); err != nil {
		return err
	}

	args := []any{
		mutation.ObservedAt.UTC(),
		nullableTime(leaseExpiresAt),
		mutation.FencingToken,
		mutation.OwnerID,
		mutation.ClaimID,
		mutation.WorkItemID,
	}
	if strings.Contains(query, "$7") {
		args = append(args, mutation.FailureClass, mutation.FailureMessage)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mutate workflow claim: %w", err)
	}
	return validateMutationResult(result)
}

func (s *WorkflowControlStore) execTerminalClaimMutation(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	query string,
) error {
	if mutation.VisibleAt.IsZero() {
		mutation.VisibleAt = mutation.ObservedAt
	}
	if s.db == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	if err := validateClaimMutation(mutation); err != nil {
		return err
	}
	args := []any{
		mutation.ObservedAt.UTC(),
		mutation.VisibleAt.UTC(),
		mutation.FencingToken,
		mutation.OwnerID,
		mutation.ClaimID,
		mutation.WorkItemID,
		mutation.FailureClass,
		mutation.FailureMessage,
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mutate terminal workflow claim: %w", err)
	}
	return validateMutationResult(result)
}

func scanClaimedWorkflowWorkItem(rows Rows) (workflow.WorkItem, workflow.Claim, error) {
	var item workflow.WorkItem
	var claim workflow.Claim
	var collectorKind string
	var sourceSystem string
	var acceptanceUnitID string
	var sourceRunID string
	var generationID string
	var fairnessKey string
	var itemStatus string
	var claimID string
	var currentFencing sql.NullInt64
	var currentOwner string
	var claimFence sql.NullInt64
	var claimStatus string

	if err := rows.Scan(
		&item.WorkItemID,
		&item.RunID,
		&collectorKind,
		&item.CollectorInstanceID,
		&sourceSystem,
		&item.ScopeID,
		&acceptanceUnitID,
		&sourceRunID,
		&generationID,
		&fairnessKey,
		&itemStatus,
		&item.AttemptCount,
		&claimID,
		&currentFencing,
		&currentOwner,
		&item.LeaseExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&claim.ClaimID,
		&claimFence,
		&claim.OwnerID,
		&claimStatus,
		&claim.ClaimedAt,
		&claim.HeartbeatAt,
		&claim.LeaseExpiresAt,
		&claim.CreatedAt,
		&claim.UpdatedAt,
	); err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, err
	}

	item.SourceSystem = strings.TrimSpace(sourceSystem)
	item.AcceptanceUnitID = strings.TrimSpace(acceptanceUnitID)
	item.SourceRunID = strings.TrimSpace(sourceRunID)
	item.GenerationID = strings.TrimSpace(generationID)
	item.FairnessKey = strings.TrimSpace(fairnessKey)
	item.CollectorKind = scope.CollectorKind(strings.TrimSpace(collectorKind))
	item.Status = workflow.WorkItemStatus(strings.TrimSpace(itemStatus))
	item.CurrentClaimID = strings.TrimSpace(claimID)
	item.CurrentFencingToken = currentFencing.Int64
	item.CurrentOwnerID = strings.TrimSpace(currentOwner)
	item.LastClaimedAt = claim.ClaimedAt

	claim.WorkItemID = item.WorkItemID
	claim.FencingToken = claimFence.Int64
	claim.Status = workflow.ClaimStatus(strings.TrimSpace(claimStatus))

	return item, claim, nil
}

func scanWorkflowClaim(rows Rows) (workflow.Claim, error) {
	var claim workflow.Claim
	var status string
	var fence sql.NullInt64
	if err := rows.Scan(
		&claim.ClaimID,
		&claim.WorkItemID,
		&fence,
		&claim.OwnerID,
		&status,
		&claim.ClaimedAt,
		&claim.HeartbeatAt,
		&claim.LeaseExpiresAt,
		&claim.CreatedAt,
		&claim.UpdatedAt,
	); err != nil {
		return workflow.Claim{}, err
	}
	claim.FencingToken = fence.Int64
	claim.Status = workflow.ClaimStatus(status)
	return claim, nil
}

func validateClaimSelector(selector workflow.ClaimSelector) error {
	if strings.TrimSpace(string(selector.CollectorKind)) == "" {
		return fmt.Errorf("collector kind is required")
	}
	if strings.TrimSpace(selector.CollectorInstanceID) == "" {
		return fmt.Errorf("collector instance id is required")
	}
	if strings.TrimSpace(selector.OwnerID) == "" {
		return fmt.Errorf("owner id is required")
	}
	if strings.TrimSpace(selector.ClaimID) == "" {
		return fmt.Errorf("claim id is required")
	}
	return nil
}

func validateClaimMutation(mutation workflow.ClaimMutation) error {
	if strings.TrimSpace(mutation.WorkItemID) == "" {
		return fmt.Errorf("work item id is required")
	}
	if strings.TrimSpace(mutation.ClaimID) == "" {
		return fmt.Errorf("claim id is required")
	}
	if mutation.FencingToken <= 0 {
		return fmt.Errorf("fencing token must be positive")
	}
	if strings.TrimSpace(mutation.OwnerID) == "" {
		return fmt.Errorf("owner id is required")
	}
	if mutation.ObservedAt.IsZero() {
		return fmt.Errorf("observed at is required")
	}
	return nil
}

func normalizeRequestedScopeSet(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "[]"
	}
	return raw
}

func (s *WorkflowControlStore) effectiveClaimLeaseTTL(provided time.Duration) (time.Duration, error) {
	ttl := provided
	if ttl <= 0 {
		ttl = s.DefaultClaimLeaseTTL
	}
	if ttl <= 0 {
		return 0, fmt.Errorf("claim lease duration must be positive")
	}
	if s.DefaultHeartbeatInterval <= 0 {
		return 0, fmt.Errorf("heartbeat interval must be positive")
	}
	if s.DefaultHeartbeatInterval >= ttl {
		return 0, fmt.Errorf("heartbeat interval must be less than claim lease duration")
	}
	return ttl, nil
}

func (s *WorkflowControlStore) effectiveExpiredRequeueDelay(provided time.Duration) time.Duration {
	if provided > 0 {
		return provided
	}
	if s.DefaultExpiredRequeueDelay > 0 {
		return s.DefaultExpiredRequeueDelay
	}
	return DefaultWorkflowExpiredClaimRequeueDelay
}

func validateMutationResult(result sql.Result) error {
	if result == nil {
		return fmt.Errorf("workflow claim mutation result is required")
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read workflow claim mutation rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrWorkflowClaimRejected
	}
	return nil
}

func nullableRFC3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
