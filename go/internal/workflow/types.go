package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// CollectorMode describes how one collector instance is expected to run.
type CollectorMode string

const (
	CollectorModeContinuous CollectorMode = "continuous"
	CollectorModeScheduled  CollectorMode = "scheduled"
	CollectorModeManual     CollectorMode = "manual"
)

// Validate checks that the collector mode is known.
func (m CollectorMode) Validate() error {
	switch m {
	case CollectorModeContinuous, CollectorModeScheduled, CollectorModeManual:
		return nil
	default:
		return fmt.Errorf("unknown collector mode %q", m)
	}
}

// TriggerKind identifies why a workflow run was created.
type TriggerKind string

const (
	TriggerKindBootstrap        TriggerKind = "bootstrap"
	TriggerKindSchedule         TriggerKind = "schedule"
	TriggerKindWebhook          TriggerKind = "webhook"
	TriggerKindReplay           TriggerKind = "replay"
	TriggerKindOperatorRecovery TriggerKind = "operator_recovery"
)

// Validate checks that the trigger kind is known.
func (k TriggerKind) Validate() error {
	switch k {
	case TriggerKindBootstrap, TriggerKindSchedule, TriggerKindWebhook, TriggerKindReplay, TriggerKindOperatorRecovery:
		return nil
	default:
		return fmt.Errorf("unknown workflow trigger kind %q", k)
	}
}

// RunStatus describes layered workflow completion.
type RunStatus string

const (
	RunStatusCollectionPending  RunStatus = "collection_pending"
	RunStatusCollectionActive   RunStatus = "collection_active"
	RunStatusCollectionComplete RunStatus = "collection_complete"
	RunStatusReducerConverging  RunStatus = "reducer_converging"
	RunStatusComplete           RunStatus = "complete"
	RunStatusFailed             RunStatus = "failed"
)

// Validate checks that the workflow status is known.
func (s RunStatus) Validate() error {
	switch s {
	case RunStatusCollectionPending, RunStatusCollectionActive, RunStatusCollectionComplete, RunStatusReducerConverging, RunStatusComplete, RunStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown workflow run status %q", s)
	}
}

// WorkItemStatus describes the durable lifecycle of one bounded collector slice.
type WorkItemStatus string

const (
	WorkItemStatusPending         WorkItemStatus = "pending"
	WorkItemStatusClaimed         WorkItemStatus = "claimed"
	WorkItemStatusCompleted       WorkItemStatus = "completed"
	WorkItemStatusFailedRetryable WorkItemStatus = "failed_retryable"
	WorkItemStatusFailedTerminal  WorkItemStatus = "failed_terminal"
	WorkItemStatusExpired         WorkItemStatus = "expired"
)

// Validate checks that the work-item status is known.
func (s WorkItemStatus) Validate() error {
	switch s {
	case WorkItemStatusPending, WorkItemStatusClaimed, WorkItemStatusCompleted, WorkItemStatusFailedRetryable, WorkItemStatusFailedTerminal, WorkItemStatusExpired:
		return nil
	default:
		return fmt.Errorf("unknown workflow work-item status %q", s)
	}
}

// ClaimStatus describes the lifecycle of one ownership epoch.
type ClaimStatus string

const (
	ClaimStatusActive          ClaimStatus = "active"
	ClaimStatusCompleted       ClaimStatus = "completed"
	ClaimStatusFailedRetryable ClaimStatus = "failed_retryable"
	ClaimStatusFailedTerminal  ClaimStatus = "failed_terminal"
	ClaimStatusExpired         ClaimStatus = "expired"
	ClaimStatusReleased        ClaimStatus = "released"
)

// Validate checks that the claim status is known.
func (s ClaimStatus) Validate() error {
	switch s {
	case ClaimStatusActive, ClaimStatusCompleted, ClaimStatusFailedRetryable, ClaimStatusFailedTerminal, ClaimStatusExpired, ClaimStatusReleased:
		return nil
	default:
		return fmt.Errorf("unknown workflow claim status %q", s)
	}
}

// Run is the durable root record for one workflow execution.
type Run struct {
	RunID              string
	TriggerKind        TriggerKind
	Status             RunStatus
	RequestedScopeSet  string
	RequestedCollector string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	FinishedAt         time.Time
}

// Validate checks that the run carries the minimum durable identity.
func (r Run) Validate() error {
	if err := validateIdentifier("run_id", r.RunID); err != nil {
		return err
	}
	if err := r.TriggerKind.Validate(); err != nil {
		return err
	}
	if err := r.Status.Validate(); err != nil {
		return err
	}
	if err := validateTime("created_at", r.CreatedAt); err != nil {
		return err
	}
	if err := validateTime("updated_at", r.UpdatedAt); err != nil {
		return err
	}
	if r.UpdatedAt.Before(r.CreatedAt) {
		return fmt.Errorf("updated_at must not be before created_at")
	}
	if !r.FinishedAt.IsZero() && r.FinishedAt.Before(r.CreatedAt) {
		return fmt.Errorf("finished_at must not be before created_at")
	}
	return nil
}

// WorkItem is the durable unit claimed by one collector instance.
type WorkItem struct {
	WorkItemID          string
	RunID               string
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
	SourceSystem        string
	ScopeID             string
	AcceptanceUnitID    string
	SourceRunID         string
	GenerationID        string
	FairnessKey         string
	Status              WorkItemStatus
	AttemptCount        int
	CurrentClaimID      string
	CurrentFencingToken int64
	CurrentOwnerID      string
	LeaseExpiresAt      time.Time
	VisibleAt           time.Time
	LastClaimedAt       time.Time
	LastCompletedAt     time.Time
	LastFailureClass    string
	LastFailureMessage  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Validate checks that the work item has durable identity and lifecycle shape.
func (w WorkItem) Validate() error {
	if err := validateIdentifier("work_item_id", w.WorkItemID); err != nil {
		return err
	}
	if err := validateIdentifier("run_id", w.RunID); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(w.CollectorKind)); err != nil {
		return err
	}
	if err := validateIdentifier("collector_instance_id", w.CollectorInstanceID); err != nil {
		return err
	}
	if err := validateIdentifier("source_system", w.SourceSystem); err != nil {
		return err
	}
	if err := validateIdentifier("scope_id", w.ScopeID); err != nil {
		return err
	}
	if err := validateIdentifier("acceptance_unit_id", w.AcceptanceUnitID); err != nil {
		return err
	}
	if err := validateIdentifier("source_run_id", w.SourceRunID); err != nil {
		return err
	}
	if err := validateIdentifier("generation_id", w.GenerationID); err != nil {
		return err
	}
	if err := w.Status.Validate(); err != nil {
		return err
	}
	if w.AttemptCount < 0 {
		return fmt.Errorf("attempt_count must not be negative")
	}
	if w.CurrentFencingToken < 0 {
		return fmt.Errorf("current_fencing_token must not be negative")
	}
	if err := validateTime("created_at", w.CreatedAt); err != nil {
		return err
	}
	if err := validateTime("updated_at", w.UpdatedAt); err != nil {
		return err
	}
	if w.UpdatedAt.Before(w.CreatedAt) {
		return fmt.Errorf("updated_at must not be before created_at")
	}
	return nil
}

// Claim is one durable ownership epoch for a workflow work item.
type Claim struct {
	ClaimID        string
	WorkItemID     string
	FencingToken   int64
	OwnerID        string
	Status         ClaimStatus
	ClaimedAt      time.Time
	HeartbeatAt    time.Time
	LeaseExpiresAt time.Time
	FinishedAt     time.Time
	FailureClass   string
	FailureMessage string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate checks that the claim carries durable ownership identity.
func (c Claim) Validate() error {
	if err := validateIdentifier("claim_id", c.ClaimID); err != nil {
		return err
	}
	if err := validateIdentifier("work_item_id", c.WorkItemID); err != nil {
		return err
	}
	if c.FencingToken <= 0 {
		return fmt.Errorf("fencing_token must be positive")
	}
	if err := validateIdentifier("owner_id", c.OwnerID); err != nil {
		return err
	}
	if err := c.Status.Validate(); err != nil {
		return err
	}
	if err := validateTime("claimed_at", c.ClaimedAt); err != nil {
		return err
	}
	if err := validateTime("heartbeat_at", c.HeartbeatAt); err != nil {
		return err
	}
	if err := validateTime("lease_expires_at", c.LeaseExpiresAt); err != nil {
		return err
	}
	if err := validateTime("created_at", c.CreatedAt); err != nil {
		return err
	}
	if err := validateTime("updated_at", c.UpdatedAt); err != nil {
		return err
	}
	if c.HeartbeatAt.Before(c.ClaimedAt) {
		return fmt.Errorf("heartbeat_at must not be before claimed_at")
	}
	if c.LeaseExpiresAt.Before(c.HeartbeatAt) {
		return fmt.Errorf("lease_expires_at must not be before heartbeat_at")
	}
	if c.UpdatedAt.Before(c.CreatedAt) {
		return fmt.Errorf("updated_at must not be before created_at")
	}
	if !c.FinishedAt.IsZero() && c.FinishedAt.Before(c.ClaimedAt) {
		return fmt.Errorf("finished_at must not be before claimed_at")
	}
	return nil
}

func validateIdentifier(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be blank", field)
	}
	return nil
}

func validateTime(field string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s must not be zero", field)
	}
	return nil
}
