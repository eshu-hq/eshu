package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	workflowEnqueueBatchSize                = 250
	workflowColumnsPerWorkItem              = 23
	DefaultWorkflowClaimLeaseTTL            = 60 * time.Second
	DefaultWorkflowClaimHeartbeatInterval   = 20 * time.Second
	DefaultWorkflowExpiredClaimRequeueDelay = 5 * time.Second
)

// ErrWorkflowClaimRejected reports that a fenced claim mutation was rejected.
var ErrWorkflowClaimRejected = errors.New("workflow claim rejected")

// ClaimSelector aliases the workflow package selector at the storage boundary.
type ClaimSelector = workflow.ClaimSelector

// ClaimMutation aliases the workflow package mutation shape at the storage boundary.
type ClaimMutation = workflow.ClaimMutation

// WorkflowControlStore persists workflow coordinator control-plane state.
type WorkflowControlStore struct {
	db                         ExecQueryer
	beginner                   Beginner
	DefaultClaimLeaseTTL       time.Duration
	DefaultHeartbeatInterval   time.Duration
	DefaultExpiredRequeueDelay time.Duration
}

// NewWorkflowControlStore constructs a Postgres-backed workflow control store.
func NewWorkflowControlStore(db ExecQueryer) *WorkflowControlStore {
	beginner, _ := db.(Beginner)
	return &WorkflowControlStore{
		db:                         db,
		beginner:                   beginner,
		DefaultClaimLeaseTTL:       DefaultWorkflowClaimLeaseTTL,
		DefaultHeartbeatInterval:   DefaultWorkflowClaimHeartbeatInterval,
		DefaultExpiredRequeueDelay: DefaultWorkflowExpiredClaimRequeueDelay,
	}
}

// WorkflowControlSchemaSQL returns the DDL for the workflow control plane.
func WorkflowControlSchemaSQL() string {
	return workflowControlSchemaSQL
}

// EnsureSchema applies the workflow control-plane schema DDL.
func (s *WorkflowControlStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	_, err := s.db.ExecContext(ctx, workflowControlSchemaSQL)
	if err != nil {
		return fmt.Errorf("ensure workflow control schema: %w", err)
	}
	_, err = s.db.ExecContext(ctx, workflowCoordinatorStateSchemaSQL)
	if err != nil {
		return fmt.Errorf("ensure workflow coordinator state schema: %w", err)
	}
	return nil
}

// CreateRun upserts one durable workflow run.
func (s *WorkflowControlStore) CreateRun(ctx context.Context, run workflow.Run) error {
	if s.db == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	if err := run.Validate(); err != nil {
		return fmt.Errorf("create workflow run: %w", err)
	}
	finishedAt := nullableRFC3339(run.FinishedAt)
	_, err := s.db.ExecContext(
		ctx,
		createWorkflowRunQuery,
		run.RunID,
		string(run.TriggerKind),
		string(run.Status),
		normalizeRequestedScopeSet(run.RequestedScopeSet),
		run.RequestedCollector,
		run.CreatedAt.UTC(),
		run.UpdatedAt.UTC(),
		finishedAt,
	)
	if err != nil {
		return fmt.Errorf("create workflow run: %w", err)
	}
	return nil
}

// EnqueueWorkItems inserts workflow work items in batches.
func (s *WorkflowControlStore) EnqueueWorkItems(ctx context.Context, items []workflow.WorkItem) error {
	if s.db == nil {
		return fmt.Errorf("workflow control store database is required")
	}
	if len(items) == 0 {
		return nil
	}

	for _, item := range items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("enqueue workflow work items: %w", err)
		}
	}

	for i := 0; i < len(items); i += workflowEnqueueBatchSize {
		end := i + workflowEnqueueBatchSize
		if end > len(items) {
			end = len(items)
		}
		if err := s.enqueueWorkItemBatch(ctx, items[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// ClaimNextEligible claims the next bounded work item for one collector actor.
func (s *WorkflowControlStore) ClaimNextEligible(
	ctx context.Context,
	selector workflow.ClaimSelector,
	now time.Time,
	leaseDuration time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	if s.db == nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, fmt.Errorf("workflow control store database is required")
	}
	if err := validateClaimSelector(selector); err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, err
	}
	effectiveLeaseTTL, err := s.effectiveClaimLeaseTTL(leaseDuration)
	if err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, err
	}

	rows, err := s.db.QueryContext(
		ctx,
		claimNextWorkflowWorkItemQuery,
		string(selector.CollectorKind),
		selector.CollectorInstanceID,
		now.UTC(),
		selector.OwnerID,
		selector.ClaimID,
		now.UTC().Add(effectiveLeaseTTL),
	)
	if err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, fmt.Errorf("claim workflow work item: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return workflow.WorkItem{}, workflow.Claim{}, false, fmt.Errorf("claim workflow work item: %w", err)
		}
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}

	item, claim, err := scanClaimedWorkflowWorkItem(rows)
	if err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, fmt.Errorf("claim workflow work item: %w", err)
	}
	if err := rows.Err(); err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, fmt.Errorf("claim workflow work item: %w", err)
	}

	return item, claim, true, nil
}

// HeartbeatClaim extends the active ownership epoch.
func (s *WorkflowControlStore) HeartbeatClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	effectiveLeaseTTL, err := s.effectiveClaimLeaseTTL(mutation.LeaseDuration)
	if err != nil {
		return err
	}
	return s.execClaimMutation(ctx, mutation, heartbeatWorkflowClaimQuery, mutation.ObservedAt.UTC().Add(effectiveLeaseTTL))
}

// CompleteClaim marks the active ownership epoch and work item complete.
func (s *WorkflowControlStore) CompleteClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return s.execClaimMutation(ctx, mutation, completeWorkflowClaimQuery, time.Time{})
}

// ReleaseClaim gives up an active claim without recording a collector failure.
func (s *WorkflowControlStore) ReleaseClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	return s.execClaimMutation(ctx, mutation, releaseWorkflowClaimQuery, time.Time{})
}

// FailClaimRetryable marks the current epoch retryable and requeues the work item.
func (s *WorkflowControlStore) FailClaimRetryable(ctx context.Context, mutation workflow.ClaimMutation) error {
	if mutation.VisibleAt.IsZero() {
		mutation.VisibleAt = mutation.ObservedAt
	}
	return s.execTerminalClaimMutation(ctx, mutation, failWorkflowClaimRetryableQuery)
}

// FailClaimTerminal marks the current epoch terminal without requeueing.
func (s *WorkflowControlStore) FailClaimTerminal(ctx context.Context, mutation workflow.ClaimMutation) error {
	return s.execTerminalClaimMutation(ctx, mutation, failWorkflowClaimTerminalQuery)
}

// ReapExpiredClaims expires stale active claims atomically and requeues their work.
func (s *WorkflowControlStore) ReapExpiredClaims(
	ctx context.Context,
	asOf time.Time,
	limit int,
	requeueDelay time.Duration,
) ([]workflow.Claim, error) {
	if s.db == nil {
		return nil, fmt.Errorf("workflow control store database is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("expired claim limit must be positive")
	}
	effectiveRequeueDelay := s.effectiveExpiredRequeueDelay(requeueDelay)
	rows, err := s.db.QueryContext(ctx, reapExpiredWorkflowClaimsQuery, asOf.UTC(), limit, asOf.UTC().Add(effectiveRequeueDelay))
	if err != nil {
		return nil, fmt.Errorf("reap expired workflow claims: %w", err)
	}
	defer func() { _ = rows.Close() }()

	claims := make([]workflow.Claim, 0)
	for rows.Next() {
		claim, err := scanWorkflowClaim(rows)
		if err != nil {
			return nil, fmt.Errorf("reap expired workflow claims: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reap expired workflow claims: %w", err)
	}
	return claims, nil
}

func workflowControlBootstrapDefinition() Definition {
	return Definition{
		Name: "workflow_control_plane",
		Path: "schema/data-plane/postgres/014_workflow_control_plane.sql",
		SQL:  workflowControlSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, workflowControlBootstrapDefinition())
}

var _ workflow.ControlStore = (*WorkflowControlStore)(nil)
