package recovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage identifies a write-plane pipeline stage for replay filtering.
type Stage string

const (
	// StageProjector targets source-local projection work items.
	StageProjector Stage = "projector"

	// StageReducer targets cross-scope reducer work items.
	StageReducer Stage = "reducer"
)

// ReplayFilter constrains which failed work items to replay.
type ReplayFilter struct {
	// Stage limits replay to a specific pipeline stage. Required.
	Stage Stage

	// ScopeIDs limits replay to specific ingestion scopes. Empty means all.
	ScopeIDs []string

	// FailureClass limits replay to a specific failure classification.
	FailureClass string

	// Limit caps the number of items replayed. Zero means no limit.
	Limit int
}

// Validate returns an error if the filter is not usable.
func (f ReplayFilter) Validate() error {
	switch f.Stage {
	case StageProjector, StageReducer:
	default:
		return fmt.Errorf("replay filter requires a valid stage, got %q", f.Stage)
	}

	return nil
}

// ReplayResult captures the outcome of a replay operation.
type ReplayResult struct {
	Stage       Stage
	Replayed    int
	WorkItemIDs []string
}

// RefinalizeFilter constrains which scopes to re-enqueue for projection.
type RefinalizeFilter struct {
	// ScopeIDs targets specific ingestion scopes. Required and non-empty.
	ScopeIDs []string
}

// Validate returns an error if the filter is not usable.
func (f RefinalizeFilter) Validate() error {
	if len(f.ScopeIDs) == 0 {
		return errors.New("refinalize filter requires at least one scope_id")
	}

	return nil
}

// RefinalizeResult captures the outcome of a refinalize operation.
type RefinalizeResult struct {
	Enqueued int
	ScopeIDs []string
}

// CollectorGenerationReplayFilter constrains collector generation commit
// failures that should be marked for source-level replay.
type CollectorGenerationReplayFilter struct {
	// ScopeIDs limits replay requests to specific ingestion scopes. Empty means
	// all scopes for the selected collector kind and failure class.
	ScopeIDs []string

	// FailureClass limits replay requests to a specific commit failure class.
	FailureClass string

	// CollectorKind limits replay requests to one collector family. Required.
	CollectorKind string

	// Limit caps the number of generation replay requests. Zero means the store
	// chooses its bounded default.
	Limit int
}

// Validate returns an error if the collector generation replay filter is not
// usable.
func (f CollectorGenerationReplayFilter) Validate() error {
	if strings.TrimSpace(f.CollectorKind) == "" {
		return errors.New("collector generation replay requires collector_kind")
	}
	return nil
}

// CollectorGenerationReplayResult captures collector generation replay request
// outcomes.
type CollectorGenerationReplayResult struct {
	Replayed      int
	GenerationIDs []string
}

// ReplayStore provides the database operations needed for recovery.
type ReplayStore interface {
	// ReplayFailedWorkItems resets failed work items to pending for the
	// given stage and filter criteria.
	ReplayFailedWorkItems(ctx context.Context, filter ReplayFilter, now time.Time) (ReplayResult, error)

	// RefinalizeScopeProjections re-enqueues projector work for the given
	// scope IDs by inserting new pending work items for their active
	// generations.
	RefinalizeScopeProjections(ctx context.Context, filter RefinalizeFilter, now time.Time) (RefinalizeResult, error)

	// ReplayCollectorGenerations marks collector generation commit failures for
	// source-level replay.
	ReplayCollectorGenerations(
		ctx context.Context,
		filter CollectorGenerationReplayFilter,
		now time.Time,
	) (CollectorGenerationReplayResult, error)
}

// Handler orchestrates recovery operations through the store.
type Handler struct {
	store ReplayStore
	now   func() time.Time
}

// NewHandler constructs a recovery handler over the given store.
func NewHandler(store ReplayStore) (*Handler, error) {
	if store == nil {
		return nil, errors.New("recovery store is required")
	}

	return &Handler{store: store}, nil
}

// ReplayFailed replays failed work items matching the given filter.
func (h *Handler) ReplayFailed(ctx context.Context, filter ReplayFilter) (ReplayResult, error) {
	if err := filter.Validate(); err != nil {
		return ReplayResult{}, fmt.Errorf("replay failed: %w", err)
	}

	return h.store.ReplayFailedWorkItems(ctx, filter, h.time())
}

// Refinalize re-enqueues projector work for the given scopes, causing
// their active generations to be re-projected through the write plane.
func (h *Handler) Refinalize(ctx context.Context, filter RefinalizeFilter) (RefinalizeResult, error) {
	if err := filter.Validate(); err != nil {
		return RefinalizeResult{}, fmt.Errorf("refinalize: %w", err)
	}

	return h.store.RefinalizeScopeProjections(ctx, filter, h.time())
}

// ReplayCollectorGenerations marks collector generation commit failures for
// source-level replay.
func (h *Handler) ReplayCollectorGenerations(
	ctx context.Context,
	filter CollectorGenerationReplayFilter,
) (CollectorGenerationReplayResult, error) {
	if err := filter.Validate(); err != nil {
		return CollectorGenerationReplayResult{}, fmt.Errorf("replay collector generations: %w", err)
	}

	return h.store.ReplayCollectorGenerations(ctx, filter, h.time())
}

func (h *Handler) time() time.Time {
	if h.now != nil {
		return h.now().UTC()
	}

	return time.Now().UTC()
}
