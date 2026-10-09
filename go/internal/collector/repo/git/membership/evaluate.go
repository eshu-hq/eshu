// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Outcome is the closed result of one evaluation, recorded on the evaluation
// counter's outcome label.
type Outcome string

const (
	// OutcomeEvaluated means the batch was computed and is written.
	OutcomeEvaluated Outcome = telemetry.RepositorySelectionOutcomeEvaluated
	// OutcomeListingTruncated means the listing stopped at the repository
	// limit, so nothing is read or written.
	OutcomeListingTruncated Outcome = telemetry.RepositorySelectionOutcomeListingTruncated
	// OutcomeGuardTripped means the mass-miss guard held the write.
	OutcomeGuardTripped Outcome = telemetry.RepositorySelectionOutcomeGuardTripped
	// OutcomeStoreError means a store read or write failed.
	OutcomeStoreError Outcome = telemetry.RepositorySelectionOutcomeStoreError
)

const (
	// notListedSampleLimit caps the slugs the evaluated log carries.
	notListedSampleLimit = 10
	// guardMinimum and guardFraction bound newly unlisted scopes per cycle:
	// more than max(guardMinimum, ceil(guardFraction * known)) trips the guard.
	guardMinimum  = 10
	guardFraction = 0.10
)

// ListedRepository is one repository from a complete listing, classified by
// the selector. ScopeID is the scope the collector would write for it.
type ListedRepository struct {
	ScopeID  string
	Slug     string
	GitHubID int64
	State    State
}

// Listing is one discovery listing. Complete is false when the listing
// stopped at the repository limit.
type Listing struct {
	Complete     bool
	Repositories []ListedRepository
}

// KnownScope is one repository scope already in ingestion_scopes for the
// selector's org, with its stored repo slug.
type KnownScope struct {
	ScopeID string
	Slug    string
}

// Input is everything one evaluation reads. Now is the cycle's observation
// time; LivenessWindow is the configured window every written row carries
// (zero or negative means DefaultLivenessWindow).
type Input struct {
	Selector       Selector
	Now            time.Time
	LivenessWindow time.Duration
	Listing        Listing
	Known          []KnownScope
	Prior          []Observation
}

// Counts are the evaluation's log counters. Listed, Selectable,
// ArchivedExcluded, and RuleExcluded count listing records; the rest count
// known scopes.
type Counts struct {
	Listed           int
	Selectable       int
	ArchivedExcluded int
	RuleExcluded     int
	Known            int
	NewlyUnlisted    int
	NotListed        int
	Relisted         int
}

// ScopeCounts are the known scopes per gauge state after the write.
type ScopeCounts struct {
	Selected         int
	NotListedPending int
	NotListed        int
	ArchivedExcluded int
	RuleExcluded     int
}

// Result is one evaluation's decision. Batch.Rows and Projected are set only
// for OutcomeEvaluated; Projected is the post-write observation per row.
// PreviousEvaluatedAt is the selector's newest prior evaluated_at, zero on
// its first evaluation. ExpiredDeleted is set only by Observer: the expired
// rows its sweep deleted this cycle.
type Result struct {
	Outcome             Outcome
	Batch               Batch
	Projected           []Observation
	PreviousEvaluatedAt time.Time
	Counts              Counts
	Scopes              ScopeCounts
	NotListedSample     []string
	GuardThreshold      int
	ExpiredDeleted      int64
}

// Evaluate decides one selector's observation batch from a listing, the known
// scopes in the selector's owner, and the selector's prior observations. It is
// pure: the same input always yields the same result.
//
// A github_org selector writes one row per known scope: the listed state, or
// not_listed when the complete listing missed it, held back by the mass-miss
// guard. An explicit selector writes a selected row only for each listed
// repository that already has a scope, never not_listed or excluded rows, so
// it skips the guard. Every row carries the liveness window truncated to
// whole seconds, the stored precision.
func Evaluate(in Input) Result {
	var result Result
	listed := indexListing(in.Listing.Repositories, &result.Counts)
	if !in.Listing.Complete {
		result.Outcome = OutcomeListingTruncated
		return result
	}
	now := in.Now.UTC().Truncate(time.Microsecond)
	known := partitionKnown(in.Known, in.Selector.Owner)
	result.Counts.Known = len(known)
	prior := make(map[string]Observation, len(in.Prior))
	for _, observation := range in.Prior {
		prior[observation.ScopeID] = observation
		if observation.EvaluatedAt.After(result.PreviousEvaluatedAt) {
			result.PreviousEvaluatedAt = observation.EvaluatedAt
		}
	}
	window := livenessWindow(in.LivenessWindow)
	result.Batch = Batch{Selector: in.Selector, EvaluatedAt: now, LivenessWindow: window}
	if in.Selector.Kind == KindExplicit {
		evaluateExplicit(&result, known, listed, prior)
		return result
	}

	rows := make([]Row, 0, len(known))
	projected := make([]Observation, 0, len(known))
	sample := make([]string, 0)
	for _, scope := range known {
		entry, isListed := listed[scope.ScopeID]
		previous, hasPrior := prior[scope.ScopeID]
		row := Row{ScopeID: scope.ScopeID, State: StateNotListed}
		switch {
		case isListed:
			row.State, row.GitHubRepoID = entry.State, entry.GitHubID
			if hasPrior && previous.State == StateNotListed {
				result.Counts.Relisted++
			}
		default:
			result.Counts.NotListed++
			sample = append(sample, scope.Slug)
			if !hasPrior || previous.State != StateNotListed {
				result.Counts.NewlyUnlisted++
			}
		}
		rows = append(rows, row)
		projected = append(projected, project(previous, hasPrior, row, now, window))
	}
	slices.Sort(sample)
	result.NotListedSample = sample[:min(len(sample), notListedSampleLimit)]
	result.GuardThreshold = max(guardMinimum, int(math.Ceil(guardFraction*float64(len(known)))))
	if (len(in.Listing.Repositories) == 0 && len(known) > 0) || result.Counts.NewlyUnlisted > result.GuardThreshold {
		result.Outcome = OutcomeGuardTripped
		return result
	}
	result.Outcome = OutcomeEvaluated
	result.Batch.Rows = rows
	result.Projected = projected
	result.Scopes = countScopes(projected)
	return result
}

// indexListing counts the listing and maps each scope id to its record. When
// two records share a scope id the most selecting state wins (selected, then
// rule excluded, then archived excluded), so the result is order-independent.
func indexListing(records []ListedRepository, counts *Counts) map[string]ListedRepository {
	listed := make(map[string]ListedRepository, len(records))
	for _, record := range records {
		switch record.State {
		case StateSelected:
			counts.Selectable++
		case StateArchivedExcluded:
			counts.ArchivedExcluded++
		case StateRuleExcluded:
			counts.RuleExcluded++
		case StateNotListed:
			// Only Evaluate assigns not_listed. A listed record carrying it is
			// a caller bug; ignoring it keeps it from resetting a scope's
			// unlisted counters.
			continue
		}
		counts.Listed++
		if record.ScopeID == "" {
			continue
		}
		if existing, ok := listed[record.ScopeID]; ok && stateRank(existing.State) <= stateRank(record.State) {
			continue
		}
		listed[record.ScopeID] = record
	}
	return listed
}

func stateRank(state State) int {
	switch state {
	case StateSelected:
		return 0
	case StateRuleExcluded:
		return 1
	default:
		return 2
	}
}

// partitionKnown keeps the distinct known scopes whose slug's org equals
// owner case-insensitively, sorted by scope id. The store already filters by
// org; this keeps the evaluator from ever writing another org's scope.
func partitionKnown(known []KnownScope, owner string) []KnownScope {
	kept := make([]KnownScope, 0, len(known))
	seen := make(map[string]struct{}, len(known))
	for _, scope := range known {
		if scope.ScopeID == "" || !strings.EqualFold(slugOwner(scope.Slug), owner) {
			continue
		}
		if _, ok := seen[scope.ScopeID]; ok {
			continue
		}
		seen[scope.ScopeID] = struct{}{}
		kept = append(kept, scope)
	}
	slices.SortFunc(kept, func(a, b KnownScope) int { return cmp.Compare(a.ScopeID, b.ScopeID) })
	return kept
}

func slugOwner(slug string) string {
	owner, _, _ := strings.Cut(strings.TrimSpace(slug), "/")
	return owner
}

// evaluateExplicit fills result for an explicit selector: one selected row per
// listed repository that already has a scope, in scope id order. A configured
// repository without a scope gets no row, and a known scope that is not
// configured is left alone: an explicit list says nothing about it.
func evaluateExplicit(result *Result, known []KnownScope, listed map[string]ListedRepository, prior map[string]Observation) {
	rows := make([]Row, 0, len(listed))
	projected := make([]Observation, 0, len(listed))
	for _, scope := range known {
		entry, isListed := listed[scope.ScopeID]
		if !isListed || entry.State != StateSelected {
			continue
		}
		row := Row{ScopeID: scope.ScopeID, State: StateSelected, GitHubRepoID: entry.GitHubID}
		previous, hasPrior := prior[scope.ScopeID]
		rows = append(rows, row)
		projected = append(projected, project(previous, hasPrior, row, result.Batch.EvaluatedAt, result.Batch.LivenessWindow))
	}
	result.Outcome = OutcomeEvaluated
	result.Batch.Rows = rows
	result.Projected = projected
	result.Scopes = countScopes(projected)
}

func countScopes(projected []Observation) ScopeCounts {
	var counts ScopeCounts
	for _, observation := range projected {
		switch observation.State {
		case StateSelected:
			counts.Selected++
		case StateArchivedExcluded:
			counts.ArchivedExcluded++
		case StateRuleExcluded:
			counts.RuleExcluded++
		case StateNotListed:
			if Confirmed(observation) {
				counts.NotListed++
			} else {
				counts.NotListedPending++
			}
		}
	}
	return counts
}
