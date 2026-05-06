package scope

import (
	"fmt"
	"strings"
	"time"
)

// ScopeKind identifies the durable source scope family.
type ScopeKind string

const (
	// KindRepository represents a repository snapshot scope.
	KindRepository ScopeKind = "repository"
	// KindAccount represents a cloud account or subscription scope.
	KindAccount ScopeKind = "account"
	// KindRegion represents a cloud region scope.
	KindRegion ScopeKind = "region"
	// KindCluster represents a runtime or orchestration cluster scope.
	KindCluster ScopeKind = "cluster"
	// KindStateSnapshot represents a point-in-time state snapshot scope.
	KindStateSnapshot ScopeKind = "state_snapshot"
	// KindEventTrigger represents an event-driven freshness trigger scope.
	KindEventTrigger ScopeKind = "event_trigger"
)

// CollectorKind identifies the collector family that owns the scope.
type CollectorKind string

const (
	// CollectorGit represents the Git repository collector.
	CollectorGit CollectorKind = "git"
	// CollectorAWS represents the cloud inventory collector.
	CollectorAWS CollectorKind = "aws"
	// CollectorTerraformState represents the Terraform state collector.
	CollectorTerraformState CollectorKind = "terraform_state"
	// CollectorWebhook represents the event/webhook collector.
	CollectorWebhook CollectorKind = "webhook"
)

// TriggerKind identifies how a generation was produced.
type TriggerKind string

const (
	// TriggerKindSnapshot represents a snapshot-driven generation.
	TriggerKindSnapshot TriggerKind = "snapshot"
)

// GenerationStatus describes the lifecycle state of a scope generation.
type GenerationStatus string

const (
	// GenerationStatusPending means the generation exists but is not active yet.
	GenerationStatusPending GenerationStatus = "pending"
	// GenerationStatusActive means the generation is currently authoritative.
	GenerationStatusActive GenerationStatus = "active"
	// GenerationStatusSuperseded means a newer generation replaced this one.
	GenerationStatusSuperseded GenerationStatus = "superseded"
	// GenerationStatusCompleted means the generation finished successfully.
	GenerationStatusCompleted GenerationStatus = "completed"
	// GenerationStatusFailed means the generation finished unsuccessfully.
	GenerationStatusFailed GenerationStatus = "failed"
)

var allowedGenerationTransitions = map[GenerationStatus]map[GenerationStatus]struct{}{
	GenerationStatusPending: {
		GenerationStatusActive: {},
		GenerationStatusFailed: {},
	},
	GenerationStatusActive: {
		GenerationStatusSuperseded: {},
		GenerationStatusCompleted:  {},
		GenerationStatusFailed:     {},
	},
	GenerationStatusSuperseded: {},
	GenerationStatusCompleted:  {},
	GenerationStatusFailed:     {},
}

// IngestionScope is the durable identity for a source-local scope.
type IngestionScope struct {
	ScopeID       string
	SourceSystem  string
	ScopeKind     ScopeKind
	ParentScopeID string
	CollectorKind CollectorKind
	PartitionKey  string
	// ActiveGenerationID is the currently authoritative generation, if one
	// exists. This is not a reliable "prior generation exists" signal because
	// a failed or superseded prior generation may leave no active generation.
	ActiveGenerationID string
	// PreviousGenerationExists is true when the claimed generation is not the
	// first generation ever seen for this scope. Projection uses this to avoid
	// skipping cleanup after a failed first-generation attempt.
	PreviousGenerationExists bool
	Metadata                 map[string]string
}

// HasPriorGeneration reports whether this scope has any generation before the
// one currently being projected, including failed generations that were never
// promoted to ActiveGenerationID.
func (s IngestionScope) HasPriorGeneration() bool {
	return s.PreviousGenerationExists
}

// Validate checks that the scope has the minimum durable identity fields.
func (s IngestionScope) Validate() error {
	if err := validateIdentifier("scope_id", s.ScopeID); err != nil {
		return err
	}
	if err := validateIdentifier("source_system", s.SourceSystem); err != nil {
		return err
	}
	if err := validateIdentifier("scope_kind", string(s.ScopeKind)); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(s.CollectorKind)); err != nil {
		return err
	}
	if err := validateIdentifier("partition_key", s.PartitionKey); err != nil {
		return err
	}
	if s.ParentScopeID != "" && s.ParentScopeID == s.ScopeID {
		return fmt.Errorf("parent_scope_id must differ from scope_id")
	}
	return nil
}

// MetadataCopy returns a defensive copy of the scope metadata map.
func (s IngestionScope) MetadataCopy() map[string]string {
	if len(s.Metadata) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(s.Metadata))
	for key, value := range s.Metadata {
		cloned[key] = value
	}
	return cloned
}

// ScopeGeneration is the durable truth boundary for one observed scope snapshot.
type ScopeGeneration struct {
	GenerationID  string
	ScopeID       string
	ObservedAt    time.Time
	IngestedAt    time.Time
	Status        GenerationStatus
	TriggerKind   TriggerKind
	FreshnessHint string
}

// Validate checks the generation fields and lifecycle status.
func (g ScopeGeneration) Validate() error {
	if err := validateIdentifier("generation_id", g.GenerationID); err != nil {
		return err
	}
	if err := validateIdentifier("scope_id", g.ScopeID); err != nil {
		return err
	}
	if err := validateTime("observed_at", g.ObservedAt); err != nil {
		return err
	}
	if err := validateTime("ingested_at", g.IngestedAt); err != nil {
		return err
	}
	if g.IngestedAt.Before(g.ObservedAt) {
		return fmt.Errorf("ingested_at must not be before observed_at")
	}
	if err := g.Status.Validate(); err != nil {
		return err
	}
	if err := validateIdentifier("trigger_kind", string(g.TriggerKind)); err != nil {
		return err
	}
	return nil
}

// ValidateForScope ensures the generation belongs to the supplied scope.
func (g ScopeGeneration) ValidateForScope(scope IngestionScope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := g.Validate(); err != nil {
		return err
	}
	if g.ScopeID != scope.ScopeID {
		return fmt.Errorf("generation scope_id %q does not match scope scope_id %q", g.ScopeID, scope.ScopeID)
	}
	return nil
}

// IsTerminal reports whether the generation cannot move to another status.
func (g ScopeGeneration) IsTerminal() bool {
	return g.Status.IsTerminal()
}

// CanTransitionTo reports whether the generation may move to the next status.
func (g ScopeGeneration) CanTransitionTo(next GenerationStatus) bool {
	_, ok := allowedGenerationTransitions[g.Status][next]
	return ok
}

// TransitionTo returns a copy with the requested status if the move is allowed.
func (g ScopeGeneration) TransitionTo(next GenerationStatus) (ScopeGeneration, error) {
	if err := g.Validate(); err != nil {
		return ScopeGeneration{}, err
	}
	if !g.CanTransitionTo(next) {
		return ScopeGeneration{}, fmt.Errorf("cannot transition generation status from %q to %q", g.Status, next)
	}

	g.Status = next
	return g, nil
}

// MarkActive promotes a pending generation to active.
func (g ScopeGeneration) MarkActive() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusActive)
}

// MarkCompleted marks an active generation as completed.
func (g ScopeGeneration) MarkCompleted() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusCompleted)
}

// MarkSuperseded marks an active generation as replaced by a newer one.
func (g ScopeGeneration) MarkSuperseded() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusSuperseded)
}

// MarkFailed marks a pending or active generation as failed.
func (g ScopeGeneration) MarkFailed() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusFailed)
}

// Validate checks that the generation status is known and stable.
func (status GenerationStatus) Validate() error {
	switch status {
	case GenerationStatusPending, GenerationStatusActive, GenerationStatusSuperseded, GenerationStatusCompleted, GenerationStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown generation status %q", status)
	}
}

// IsTerminal reports whether the status cannot transition to another state.
func (status GenerationStatus) IsTerminal() bool {
	switch status {
	case GenerationStatusSuperseded, GenerationStatusCompleted, GenerationStatusFailed:
		return true
	default:
		return false
	}
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
