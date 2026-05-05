package workflow

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	CompletenessStatusPending = "pending"
	CompletenessStatusReady   = "ready"
	CompletenessStatusBlocked = "blocked"
)

// PhaseRequirement identifies one reducer-owned phase the coordinator must
// observe before a bounded collector slice may be considered complete.
type PhaseRequirement struct {
	Keyspace  reducer.GraphProjectionKeyspace
	PhaseName reducer.GraphProjectionPhase
	Required  bool
}

// PhasePublicationKey identifies one published reducer checkpoint.
type PhasePublicationKey struct {
	Keyspace  reducer.GraphProjectionKeyspace
	PhaseName reducer.GraphProjectionPhase
}

// Validate checks that the phase requirement is well formed.
func (r PhaseRequirement) Validate() error {
	if err := validateIdentifier("keyspace", string(r.Keyspace)); err != nil {
		return err
	}
	if err := validateIdentifier("phase_name", string(r.PhaseName)); err != nil {
		return err
	}
	return nil
}

// Validate checks that the published checkpoint key is well formed.
func (k PhasePublicationKey) Validate() error {
	if err := validateIdentifier("keyspace", string(k.Keyspace)); err != nil {
		return err
	}
	if err := validateIdentifier("phase_name", string(k.PhaseName)); err != nil {
		return err
	}
	return nil
}

// CollectorRunProgress captures the collector-visible and reducer-visible
// progress for one collector kind inside a workflow run.
type CollectorRunProgress struct {
	CollectorKind        scope.CollectorKind
	TotalWorkItems       int
	PendingWorkItems     int
	ClaimedWorkItems     int
	CompletedWorkItems   int
	FailedTerminalItems  int
	PublishedPhaseCounts map[PhasePublicationKey]int
}

// Validate checks that the collector progress row is internally consistent.
func (p CollectorRunProgress) Validate() error {
	if err := validateIdentifier("collector_kind", string(p.CollectorKind)); err != nil {
		return err
	}
	for _, value := range []struct {
		field string
		count int
	}{
		{field: "total_work_items", count: p.TotalWorkItems},
		{field: "pending_work_items", count: p.PendingWorkItems},
		{field: "claimed_work_items", count: p.ClaimedWorkItems},
		{field: "completed_work_items", count: p.CompletedWorkItems},
		{field: "failed_terminal_items", count: p.FailedTerminalItems},
	} {
		if value.count < 0 {
			return fmt.Errorf("%s must not be negative", value.field)
		}
	}
	if p.PendingWorkItems+p.ClaimedWorkItems+p.CompletedWorkItems+p.FailedTerminalItems > p.TotalWorkItems {
		return fmt.Errorf("collector progress counts exceed total work items")
	}
	for key, count := range p.PublishedPhaseCounts {
		if err := key.Validate(); err != nil {
			return err
		}
		if count < 0 {
			return fmt.Errorf("published phase count must not be negative")
		}
	}
	return nil
}

// RunProgressSnapshot captures the durable inputs required to reconcile a
// workflow run into operator-visible status and completeness rows.
type RunProgressSnapshot struct {
	Run        Run
	Collectors []CollectorRunProgress
}

// ReconcileRunProgress derives workflow run status and completeness rows from
// bounded collector progress and reducer-owned phase publications.
func ReconcileRunProgress(snapshot RunProgressSnapshot, observedAt time.Time) (Run, []CompletenessState, error) {
	if err := snapshot.Run.Validate(); err != nil {
		return Run{}, nil, err
	}
	if observedAt.IsZero() {
		return Run{}, nil, fmt.Errorf("observed_at must not be zero")
	}
	if len(snapshot.Collectors) == 0 {
		run := snapshot.Run
		run.Status = RunStatusCollectionPending
		run.UpdatedAt = observedAt.UTC()
		run.FinishedAt = time.Time{}
		return run, nil, nil
	}

	run := snapshot.Run
	run.UpdatedAt = observedAt.UTC()
	run.FinishedAt = time.Time{}
	completeness := make([]CompletenessState, 0)
	anyPending := false
	anyClaimed := false
	anyCompleted := false
	anyFailedTerminal := false
	allCollectionComplete := true
	allRequiredPhasesReady := true

	for _, collector := range snapshot.Collectors {
		if err := collector.Validate(); err != nil {
			return Run{}, nil, err
		}
		requirements := RequiredPhasesForCollector(collector.CollectorKind)
		for _, requirement := range requirements {
			if err := requirement.Validate(); err != nil {
				return Run{}, nil, err
			}
			publicationKey := PhasePublicationKey{
				Keyspace:  requirement.Keyspace,
				PhaseName: requirement.PhaseName,
			}
			status := CompletenessStatusPending
			detail := fmt.Sprintf(
				"published for %d of %d work items",
				collector.PublishedPhaseCounts[publicationKey],
				collector.TotalWorkItems,
			)
			if collector.FailedTerminalItems > 0 {
				status = CompletenessStatusBlocked
				detail = "terminal collector failure prevents downstream completion"
			} else if collector.TotalWorkItems > 0 && collector.PublishedPhaseCounts[publicationKey] >= collector.TotalWorkItems {
				status = CompletenessStatusReady
				detail = fmt.Sprintf("published for all %d work items", collector.TotalWorkItems)
			} else {
				allRequiredPhasesReady = false
			}
			completeness = append(completeness, CompletenessState{
				RunID:         snapshot.Run.RunID,
				CollectorKind: collector.CollectorKind,
				Keyspace:      requirement.Keyspace,
				PhaseName:     string(requirement.PhaseName),
				Required:      requirement.Required,
				Status:        status,
				Detail:        detail,
				ObservedAt:    run.UpdatedAt,
				UpdatedAt:     run.UpdatedAt,
			})
		}

		if collector.PendingWorkItems > 0 {
			anyPending = true
			allCollectionComplete = false
		}
		if collector.ClaimedWorkItems > 0 {
			anyClaimed = true
			allCollectionComplete = false
		}
		if collector.CompletedWorkItems > 0 {
			anyCompleted = true
		}
		if collector.FailedTerminalItems > 0 {
			anyFailedTerminal = true
			allCollectionComplete = false
			allRequiredPhasesReady = false
		}
		if collector.CompletedWorkItems < collector.TotalWorkItems && collector.FailedTerminalItems == 0 {
			allCollectionComplete = false
		}
	}

	switch {
	case anyFailedTerminal:
		run.Status = RunStatusFailed
		run.FinishedAt = run.UpdatedAt
	case allCollectionComplete && allRequiredPhasesReady:
		run.Status = RunStatusComplete
		run.FinishedAt = run.UpdatedAt
	case allCollectionComplete:
		run.Status = RunStatusReducerConverging
	case anyClaimed || (anyPending && anyCompleted):
		run.Status = RunStatusCollectionActive
	default:
		run.Status = RunStatusCollectionPending
	}

	slices.SortFunc(completeness, func(left, right CompletenessState) int {
		if cmp := strings.Compare(string(left.CollectorKind), string(right.CollectorKind)); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(string(left.Keyspace), string(right.Keyspace)); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.PhaseName, right.PhaseName)
	})
	return run, completeness, nil
}
