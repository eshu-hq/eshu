package recovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// DrainableClass is a durable failure_class that the dead-letter backlog drain
// is allowed to replay without operator force. The drain path defaults to the
// transient bucket so an unscoped drain never touches a manual-review (poison)
// row.
type DrainableClass string

const (
	// DrainableClassRetryExhausted is the safe transient dead-letter bucket: a
	// retryable cause that exhausted its retry budget (see
	// projector.TriageClassRetryExhausted). These items are safe to replay once
	// the underlying dependency recovers, which is exactly what issue #3560's
	// backlog drain targets. It is the drain default when no class is given.
	DrainableClassRetryExhausted DrainableClass = DrainableClass(projector.TriageClassRetryExhausted)
)

// manualReviewDrainExclusions returns the durable failure_class values the drain
// must never replay, sourced from the projector triage so the recovery package
// cannot drift from the manual-review disposition #3557 writes onto a
// dead-letter row. These are the poison buckets (projection_bug,
// resource_exhausted): replaying one unchanged re-fails immediately or
// re-exhausts a constrained resource.
func manualReviewDrainExclusions() []string {
	return projector.ManualReviewTriageClasses()
}

// isManualReviewClass reports whether failureClass is one the drain must refuse
// to target explicitly. It guards an operator from routing poison through the
// drain by naming its class directly.
func isManualReviewClass(failureClass string) bool {
	trimmed := strings.TrimSpace(failureClass)
	for _, class := range manualReviewDrainExclusions() {
		if class == trimmed {
			return true
		}
	}
	return false
}

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

	// ExcludeFailureClasses lists durable failure_class values that must never
	// be selected, regardless of FailureClass. The drain path sets it to the
	// manual-review (poison) triage classes so an unscoped backlog drain can
	// never replay a projection_bug or resource_exhausted row. It is additive to
	// FailureClass: when both are set, the store applies the exclusion as well,
	// which is a no-op for a single safe class but guards the broad selector.
	ExcludeFailureClasses []string

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

// DrainFilter constrains a dead-letter backlog drain. Unlike a raw ReplayFilter,
// it is the safe operator-facing entry point for issue #3560's backlog drain: it
// defaults to the transient retry_exhausted bucket and refuses to target a
// manual-review (poison) class, so an unscoped drain can never replay work that
// would immediately re-fail or re-exhaust a constrained resource.
type DrainFilter struct {
	// Stage limits the drain to a specific pipeline stage. Required.
	Stage Stage

	// ScopeIDs limits the drain to specific ingestion scopes. Empty means all.
	ScopeIDs []string

	// FailureClass optionally narrows the drain to one drainable failure class.
	// Empty defaults to DrainableClassRetryExhausted. A manual-review class is
	// refused so poison cannot be drained through this path.
	FailureClass string

	// Limit caps the number of items drained. Zero means no limit.
	Limit int
}

// Validate returns an error if the drain filter is not usable or targets an
// unsafe class. It refuses a manual-review class before any store read or write
// so an operator can never drain poison by naming its class directly.
func (f DrainFilter) Validate() error {
	switch f.Stage {
	case StageProjector, StageReducer:
	default:
		return fmt.Errorf("drain filter requires a valid stage, got %q", f.Stage)
	}

	if isManualReviewClass(f.FailureClass) {
		return fmt.Errorf(
			"drain refuses manual-review failure class %q: replaying it unchanged risks immediate re-failure or resource re-exhaustion; investigate and force the replay instead",
			strings.TrimSpace(f.FailureClass),
		)
	}

	return nil
}

// toReplayFilter projects the drain filter onto the ReplayFilter the store
// understands. It pins the failure class to the safe default when unset and
// always carries the manual-review exclusion so even a broad selector cannot
// move a poison row.
func (f DrainFilter) toReplayFilter() ReplayFilter {
	failureClass := strings.TrimSpace(f.FailureClass)
	if failureClass == "" {
		failureClass = string(DrainableClassRetryExhausted)
	}

	return ReplayFilter{
		Stage:                 f.Stage,
		ScopeIDs:              f.ScopeIDs,
		FailureClass:          failureClass,
		ExcludeFailureClasses: manualReviewDrainExclusions(),
		Limit:                 f.Limit,
	}
}

// DrainResult captures the outcome of a dead-letter backlog drain. BacklogDepth
// Before is the matching terminal count read before the drain replayed, so an
// operator can watch a drain make progress (depth before vs. items replayed).
type DrainResult struct {
	Stage              Stage
	Replayed           int
	BacklogDepthBefore int
	WorkItemIDs        []string
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

	// CountDeadLetterBacklog reports how many terminal (dead_letter/failed) work
	// items currently match the filter, before any replay runs. The drain path
	// uses it to record backlog depth so an operator can watch a drain make
	// progress (depth before vs. items replayed). It must apply the same Stage,
	// ScopeIDs, FailureClass, and ExcludeFailureClasses predicates the replay
	// would, so the count reflects exactly the rows the drain is allowed to move.
	CountDeadLetterBacklog(ctx context.Context, filter ReplayFilter) (int, error)

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

// DrainBacklog safely drains the dead-letter backlog for one stage. It validates
// the filter (refusing manual-review classes), reads the matching backlog depth
// so the operator gets a before-count, then replays only the drainable rows.
// The store-side exclusion guarantees a poison row is never replayed even though
// the drain selects a broad, unscoped set by default.
//
// Drain order is read-then-replay: the depth is captured before the replay
// mutates rows, so BacklogDepthBefore reflects the backlog the drain set out to
// move. A store read error is surfaced rather than silently proceeding with a
// zero depth, so an operator never reads a misleading progress number.
func (h *Handler) DrainBacklog(ctx context.Context, filter DrainFilter) (DrainResult, error) {
	if err := filter.Validate(); err != nil {
		return DrainResult{}, fmt.Errorf("drain backlog: %w", err)
	}

	replayFilter := filter.toReplayFilter()

	depth, err := h.store.CountDeadLetterBacklog(ctx, replayFilter)
	if err != nil {
		return DrainResult{}, fmt.Errorf("drain backlog: count: %w", err)
	}

	result, err := h.store.ReplayFailedWorkItems(ctx, replayFilter, h.time())
	if err != nil {
		return DrainResult{}, fmt.Errorf("drain backlog: replay: %w", err)
	}

	return DrainResult{
		Stage:              result.Stage,
		Replayed:           result.Replayed,
		BacklogDepthBefore: depth,
		WorkItemIDs:        result.WorkItemIDs,
	}, nil
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
