// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import "time"

// Selection evaluation outcome vocabulary for the #7625 repository selection
// observer. These are the closed outcome values the observer records on
// eshu_dp_collector_repository_selection_evaluations_total and returns from
// a store evaluation.
const (
	// SelectionEvaluationEvaluated means the evaluation wrote its rows.
	SelectionEvaluationEvaluated = "evaluated"
	// SelectionEvaluationListingTruncated means the discovery listing was
	// cut off at RepoLimit, so the observer skipped the evaluation rather
	// than mistake present repositories for missing ones. It is decided
	// before the store is reached.
	SelectionEvaluationListingTruncated = "listing_truncated"
	// SelectionEvaluationGuardTripped means a safety rail fired (an empty
	// listing or the mass-miss guard) and the store wrote nothing.
	SelectionEvaluationGuardTripped = "guard_tripped"
	// SelectionEvaluationStoreError means the observation write failed;
	// ingestion continues and the failure is a WARN, never fatal. It is
	// decided at the call site, never returned by the store.
	SelectionEvaluationStoreError = "store_error"
)

// Selection observation states for repository_selection_observations
// (#7625). The store writes them; the freshness read interprets them.
const (
	// SelectionStateSelected means the scope's repository was in the
	// selector's listing.
	SelectionStateSelected = "selected"
	// SelectionStateArchivedExcluded means the listing carried the
	// repository as archived and the selector excludes archived repos.
	SelectionStateArchivedExcluded = "archived_excluded"
	// SelectionStateRuleExcluded means the listing carried the repository
	// but no repository rule matched it.
	SelectionStateRuleExcluded = "rule_excluded"
	// SelectionStateNotListed means the scope's repository was absent from
	// the selector's listing.
	SelectionStateNotListed = "not_listed"
)

// Selection evaluation selector kinds (#7625). Only these two modes
// evaluate: githubOrg from a complete org listing, explicit by writing
// positive selected rows for its configured repositories.
const (
	// SelectionSelectorKindGitHubOrg evaluates a complete githubOrg listing
	// against the known same-org scopes.
	SelectionSelectorKindGitHubOrg = "githubOrg"
	// SelectionSelectorKindExplicit writes positive selected rows for the
	// explicit-list mode's configured repositories without org enumeration.
	SelectionSelectorKindExplicit = "explicit"
)

// EvaluatedRepository is one listed repository in a selection evaluation:
// the ingestion scope ID the collector derived for it and, when the
// discovery listing carried one, its numeric GitHub repository id. A zero
// GitHubID means unknown (explicit-list mode never has one); the store keeps
// the previously stored id then.
type EvaluatedRepository struct {
	ScopeID  string
	GitHubID int64
}

// SelectionEvaluation is one #7625 selection evaluation: the categorized
// listing one selector observed at EvaluatedAt. It lives in scope (not in
// the git collector or the Postgres store) so both sides of the
// SelectionObserver port share one shape without an import between them.
type SelectionEvaluation struct {
	// SelectorID identifies the evaluating selector: a hash of the mode,
	// org, rules, archived setting, and credential identity. Two collectors
	// that list different repositories must never share one.
	SelectorID string
	// SelectorKind is SelectionSelectorKindGitHubOrg or
	// SelectionSelectorKindExplicit.
	SelectorKind string
	// Org is the lower-cased GitHub org whose scopes the store enumerates.
	// Empty for explicit mode, which writes positive rows only.
	Org string
	// Listed holds the selected repositories; Archived the
	// archived-excluded ones; RuleExcluded the listed-but-unmatched ones.
	Listed       []EvaluatedRepository
	Archived     []EvaluatedRepository
	RuleExcluded []EvaluatedRepository
	// EvaluatedAt is the cycle time the evaluation is stamped with, in UTC.
	EvaluatedAt time.Time
	// LivenessWindowSeconds is how long a written row stays live, from
	// ESHU_REPO_SELECTION_LIVENESS_WINDOW. It must be positive.
	LivenessWindowSeconds int
}

// SelectionEvaluationOutcome is what a recorded selection evaluation
// returns: the outcome, the per-state row counts written (zero when the
// guard tripped), the known same-org scope count and the newly-missing count
// behind a guard trip (zero for explicit mode, which does not enumerate),
// and the newest prior evaluated_at for this selector (zero when this is
// the first evaluation), which the observer uses to detect a
// liveness-lapsed evaluation gap.
type SelectionEvaluationOutcome struct {
	Outcome          string
	Selected         int
	ArchivedExcluded int
	RuleExcluded     int
	NotListed        int
	KnownScopes      int
	NewlyMissing     int
	PriorEvaluatedAt time.Time
}
