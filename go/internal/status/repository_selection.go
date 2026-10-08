// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// RepositorySelectionState is the coarse selection verdict for one
// repository scope: is its repository still in some live selector's listing,
// and if not, how certain is that (#7625).
type RepositorySelectionState string

// The five states a repository selection read can render. See
// ComputeRepositorySelectionState for the precedence between them.
const (
	// RepositorySelectionSelected means at least one live selector lists
	// the repository.
	RepositorySelectionSelected RepositorySelectionState = "selected"
	// RepositorySelectionNotSelected means every live selector excludes the
	// repository, every exclusion is confirmed, and no generation was
	// observed after the exclusions began.
	RepositorySelectionNotSelected RepositorySelectionState = "not_selected"
	// RepositorySelectionPendingConfirmation means every live selector
	// excludes the repository but at least one exclusion is not yet
	// confirmed.
	RepositorySelectionPendingConfirmation RepositorySelectionState = "pending_confirmation"
	// RepositorySelectionExcludedStillIngested means every live exclusion
	// is confirmed, but a generation was observed after the latest
	// exclusion began -- something is still ingesting the repository.
	RepositorySelectionExcludedStillIngested RepositorySelectionState = "excluded_still_ingested"
	// RepositorySelectionUnknown means no selector has a live observation
	// row for the scope, so nothing can be said about its selection.
	RepositorySelectionUnknown RepositorySelectionState = "unknown"
)

// Closed reasons for a repository selection state. They name which rule
// decided, so an operator can tell "no selector has looked lately" apart
// from "every selector agrees the repository is gone".
const (
	// RepositorySelectionReasonNoLiveObservations means no observation row
	// is live: either no selector ever evaluated the scope, or every row
	// lapsed past its liveness window.
	RepositorySelectionReasonNoLiveObservations = "no_live_observations"
	// RepositorySelectionReasonLiveSelectedRow means at least one live row
	// still lists the repository.
	RepositorySelectionReasonLiveSelectedRow = "live_selected_row"
	// RepositorySelectionReasonUnconfirmedExclusion means every live row
	// excludes the repository but at least one exclusion was seen fewer
	// than twice or within the last five minutes.
	RepositorySelectionReasonUnconfirmedExclusion = "unconfirmed_exclusion"
	// RepositorySelectionReasonGenerationObservedAfterStateSince means the
	// scope produced a generation after the latest live exclusion began.
	RepositorySelectionReasonGenerationObservedAfterStateSince = "generation_observed_after_state_since"
	// RepositorySelectionReasonConfirmedExclusion means every live row
	// excludes the repository, every exclusion is confirmed, and no
	// generation was observed after the exclusions began.
	RepositorySelectionReasonConfirmedExclusion = "confirmed_exclusion"
)

// selectionConfirmationGap is the minimum span between the first and latest
// sightings of an exclusion state for it to count as confirmed (#7625
// amendment 1: two evaluations at least five minutes apart, for every
// exclusion state alike).
const selectionConfirmationGap = 5 * time.Minute

// SelectionObservationRow is one stored repository_selection_observations
// row for a scope, as the selection read needs it. It mirrors the table:
// SelectorID and State are never blank, GitHubID is 0 when the listing
// never carried a numeric id, and LastListedAt is zero when no evaluation
// ever saw the scope listed.
type SelectionObservationRow struct {
	SelectorID            string
	State                 string
	GitHubID              int64
	EvaluatedAt           time.Time
	LivenessWindowSeconds int
	StateSince            time.Time
	StateCycleCount       int
	LastListedAt          time.Time
}

// RepositorySelection is the additive selection block on the repository
// freshness read (#7625): whether the scope's repository is still selected,
// and the evidence behind that answer. Times are zero and LiveSelectorCount
// is 0 when the state is unknown.
type RepositorySelection struct {
	State             RepositorySelectionState
	Reason            string
	StateSince        time.Time
	LastListedAt      time.Time
	EvaluatedAt       time.Time
	LiveSelectorCount int
}

// ComputeRepositorySelectionState derives the selection block from one
// scope's observation rows, the scope's latest generation observed_at (zero
// when the scope never produced a generation), and now. It is pure so every
// rule -- including liveness lapse and the still-ingested race -- is unit
// testable without a database.
//
// A row is live while evaluated_at plus its liveness window still covers
// now; a scope with no live rows reads unknown, so a stalled collector fails
// open instead of condemning repositories it stopped looking at. A live row
// is confirmed when its state is not selected, it was seen in at least two
// evaluations, and those evaluations span at least five minutes.
//
// Precedence (evaluated top to bottom; the first matching rule wins):
//
//  1. unknown: no live row.
//  2. selected: some live row lists the repository.
//  3. pending_confirmation: every live row excludes the repository, but some
//     exclusion is unconfirmed.
//  4. excluded_still_ingested: every live exclusion is confirmed, but a
//     generation was observed after the latest live state_since.
//  5. not_selected: every live exclusion is confirmed and no generation was
//     observed after the latest live state_since.
func ComputeRepositorySelectionState(
	rows []SelectionObservationRow,
	latestObservedAt time.Time,
	now time.Time,
) RepositorySelection {
	now = now.UTC()
	live := make([]SelectionObservationRow, 0, len(rows))
	for _, row := range rows {
		if row.LivenessWindowSeconds <= 0 {
			continue
		}
		window := time.Duration(row.LivenessWindowSeconds) * time.Second
		if row.EvaluatedAt.UTC().Add(window).Before(now) {
			continue
		}
		live = append(live, row)
	}
	if len(live) == 0 {
		return RepositorySelection{
			State:  RepositorySelectionUnknown,
			Reason: RepositorySelectionReasonNoLiveObservations,
		}
	}

	selection := RepositorySelection{LiveSelectorCount: len(live)}
	for _, row := range live {
		if row.EvaluatedAt.UTC().After(selection.EvaluatedAt) {
			selection.EvaluatedAt = row.EvaluatedAt.UTC()
		}
		if row.StateSince.UTC().After(selection.StateSince) {
			selection.StateSince = row.StateSince.UTC()
		}
		if row.LastListedAt.UTC().After(selection.LastListedAt) {
			selection.LastListedAt = row.LastListedAt.UTC()
		}
		if row.State == scope.SelectionStateSelected {
			selection.State = RepositorySelectionSelected
			selection.Reason = RepositorySelectionReasonLiveSelectedRow
		}
	}
	if selection.State == RepositorySelectionSelected {
		return selection
	}
	for _, row := range live {
		if row.StateCycleCount < 2 || row.EvaluatedAt.UTC().Sub(row.StateSince.UTC()) < selectionConfirmationGap {
			selection.State = RepositorySelectionPendingConfirmation
			selection.Reason = RepositorySelectionReasonUnconfirmedExclusion
			return selection
		}
	}
	if !latestObservedAt.IsZero() && latestObservedAt.UTC().After(selection.StateSince) {
		selection.State = RepositorySelectionExcludedStillIngested
		selection.Reason = RepositorySelectionReasonGenerationObservedAfterStateSince
		return selection
	}
	selection.State = RepositorySelectionNotSelected
	selection.Reason = RepositorySelectionReasonConfirmedExclusion
	return selection
}
