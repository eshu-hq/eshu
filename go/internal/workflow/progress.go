// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	// DeadLetterDomain names the reducer Domain that owns writing this phase,
	// when exactly one reducer domain is responsible for its publication
	// (issue #4459). A blank value means no reducer domain is bridged for
	// this phase yet, so a terminal dead-letter can never be attributed to
	// it — the readiness gate stays a pure "not yet published" pending state
	// instead of guessing, which fails closed (never a false block) rather
	// than open (never wedging attribution).
	DeadLetterDomain reducer.Domain
}

// PhasePublicationKey identifies one published reducer checkpoint.
type PhasePublicationKey struct {
	Keyspace  reducer.GraphProjectionKeyspace
	PhaseName reducer.GraphProjectionPhase
}

// Validate checks that the phase requirement is well formed. DeadLetterDomain
// is validated only when set: blank is the valid "not bridged yet" sentinel
// (#4459), but a non-blank value now participates in completeness blocking
// (ReconcileRunProgress), so it must name a real, known reducer.Domain rather
// than an accidental or misspelled string that could never match a live
// fact_work_items.domain value (Copilot review finding on PR #4518).
func (r PhaseRequirement) Validate() error {
	if err := validateIdentifier("keyspace", string(r.Keyspace)); err != nil {
		return err
	}
	if err := validateIdentifier("phase_name", string(r.PhaseName)); err != nil {
		return err
	}
	if r.DeadLetterDomain != "" {
		if err := r.DeadLetterDomain.Validate(); err != nil {
			return fmt.Errorf("dead_letter_domain: %w", err)
		}
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
	// TerminalDeadLetterCounts counts, per required phase, how many
	// same-scope-generation fact_work_items rows have permanently
	// dead-lettered (status = 'dead_letter', a genuinely terminal,
	// non-retryable reducer failure — never 'retrying', 'pending',
	// 'claimed', or 'running') for the reducer domain that owns publishing
	// that phase. A non-zero count here is the only signal that may block a
	// required phase's completeness on a terminal failure instead of
	// leaving it pending; see #4459. Callers MUST NOT populate this map from
	// anything other than a confirmed terminal dead-letter row, or a
	// transient retry will be mis-reported as a permanent block.
	TerminalDeadLetterCounts map[PhasePublicationKey]int
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
			published := collector.PublishedPhaseCounts[publicationKey]
			status := CompletenessStatusPending
			detail := fmt.Sprintf(
				"published for %d of %d work items",
				published,
				collector.TotalWorkItems,
			)
			// terminalDeadLetters only ever reflects a confirmed
			// fact_work_items status='dead_letter' row (#4459); a
			// still-retrying or otherwise non-terminal reducer failure MUST
			// leave this at zero, so an in-flight phase always reports
			// pending here, never a false block.
			terminalDeadLetters := requirement.DeadLetterDomain != "" && collector.TerminalDeadLetterCounts[publicationKey] > 0
			switch {
			case collector.FailedTerminalItems > 0:
				status = CompletenessStatusBlocked
				detail = "terminal collector failure prevents downstream completion"
			case collector.TotalWorkItems > 0 && published >= collector.TotalWorkItems:
				status = CompletenessStatusReady
				detail = fmt.Sprintf("published for all %d work items", collector.TotalWorkItems)
			case terminalDeadLetters:
				// The phase never published and never will: its owning
				// reducer domain dead-lettered terminally for this scope
				// generation. Report the run as blocked/failed instead of
				// wedging in reducer_converging forever.
				status = CompletenessStatusBlocked
				detail = fmt.Sprintf(
					"reducer domain %q dead-lettered %d work item(s) terminally; phase will never publish for this generation",
					requirement.DeadLetterDomain,
					collector.TerminalDeadLetterCounts[publicationKey],
				)
				anyFailedTerminal = true
			default:
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
