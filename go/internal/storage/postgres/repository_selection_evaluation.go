// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// selectionMassMissFloor is the absolute floor of the mass-miss guard: an
// evaluation writes nothing when the newly-missing count exceeds
// max(selectionMassMissFloor, 10% of known scopes).
const selectionMassMissFloor = 10

// selectionPriorRow is one stored observation row as the evaluation plan
// needs it: the state the last evaluation left and when it ran.
type selectionPriorRow struct {
	state       string
	evaluatedAt time.Time
}

// normalizedSelectionEvaluation is a validated evaluation with trimmed,
// lower-cased org, UTC stamp, and deduplicated per-category scope IDs.
type normalizedSelectionEvaluation struct {
	scope.SelectionEvaluation
	listedIDs       []string
	listedGitHubIDs map[string]int64
	archivedIDs     []string
	archivedGitHubs map[string]int64
	ruleExcludedIDs []string
	ruleGitHubIDs   map[string]int64
}

// normalizeSelectionEvaluation validates the evaluation boundary: a blank
// selector, an unknown kind, a zero stamp, a non-positive window, or (for
// githubOrg) a blank or slashed org is a caller bug and fails the recording
// before anything is read. Per-category scope IDs are trimmed and
// deduplicated; a blank scope ID fails the recording.
func normalizeSelectionEvaluation(evaluation scope.SelectionEvaluation) (normalizedSelectionEvaluation, error) {
	normalized := normalizedSelectionEvaluation{SelectionEvaluation: evaluation}
	normalized.SelectorID = strings.TrimSpace(evaluation.SelectorID)
	if normalized.SelectorID == "" {
		return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: selector id is required")
	}
	switch evaluation.SelectorKind {
	case scope.SelectionSelectorKindGitHubOrg, scope.SelectionSelectorKindExplicit:
		normalized.SelectorKind = evaluation.SelectorKind
	default:
		return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: unknown selector kind %q", evaluation.SelectorKind)
	}
	if evaluation.EvaluatedAt.IsZero() {
		return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: evaluated_at is required")
	}
	normalized.EvaluatedAt = evaluation.EvaluatedAt.UTC()
	if evaluation.LivenessWindowSeconds <= 0 {
		return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: liveness window must be positive")
	}
	normalized.Org = strings.ToLower(strings.TrimSpace(evaluation.Org))
	if normalized.SelectorKind == scope.SelectionSelectorKindGitHubOrg {
		if normalized.Org == "" || strings.Contains(normalized.Org, "/") {
			return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: githubOrg evaluation requires a bare org")
		}
	}
	var err error
	if normalized.listedIDs, normalized.listedGitHubIDs, err = normalizeEvaluatedRepositories(evaluation.Listed); err != nil {
		return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: listed: %w", err)
	}
	if normalized.archivedIDs, normalized.archivedGitHubs, err = normalizeEvaluatedRepositories(evaluation.Archived); err != nil {
		return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: archived: %w", err)
	}
	if normalized.ruleExcludedIDs, normalized.ruleGitHubIDs, err = normalizeEvaluatedRepositories(evaluation.RuleExcluded); err != nil {
		return normalizedSelectionEvaluation{}, fmt.Errorf("record selection evaluation: rule excluded: %w", err)
	}
	return normalized, nil
}

// normalizeEvaluatedRepositories trims scope IDs, drops duplicates keeping
// the first GitHub id, and rejects blanks.
func normalizeEvaluatedRepositories(repositories []scope.EvaluatedRepository) ([]string, map[string]int64, error) {
	ids := make([]string, 0, len(repositories))
	githubIDs := make(map[string]int64, len(repositories))
	for _, repository := range repositories {
		id := strings.TrimSpace(repository.ScopeID)
		if id == "" {
			return nil, nil, fmt.Errorf("scope id must not be blank")
		}
		if _, seen := githubIDs[id]; seen {
			continue
		}
		// A missing id still marks the scope seen: the map doubles as the
		// dedupe set, and a zero value reads back as unknown.
		githubIDs[id] = repository.GitHubID
		ids = append(ids, id)
	}
	return ids, githubIDs, nil
}

// listingEmpty reports whether the discovery listing carried no repository
// in any category.
func (evaluation normalizedSelectionEvaluation) listingEmpty() bool {
	return len(evaluation.listedIDs) == 0 &&
		len(evaluation.archivedIDs) == 0 &&
		len(evaluation.ruleExcludedIDs) == 0
}

// selectionPlannedRow is one scope's computed state plus the GitHub id to
// store (0 keeps the previously stored id).
type selectionPlannedRow struct {
	scopeID  string
	state    string
	githubID int64
}

// selectionPlan maps scope IDs to their computed states in a deterministic
// (sorted) order for the ordered upsert.
type selectionPlan struct {
	rows []selectionPlannedRow
}

// count returns how many planned rows carry a state.
func (plan selectionPlan) count(state string) int {
	total := 0
	for _, row := range plan.rows {
		if row.state == state {
			total++
		}
	}
	return total
}

// positiveRows plans selected rows for the listed scopes (explicit mode).
func (evaluation normalizedSelectionEvaluation) positiveRows() selectionPlan {
	plan := selectionPlan{rows: make([]selectionPlannedRow, 0, len(evaluation.listedIDs))}
	for _, id := range evaluation.listedIDs {
		plan.rows = append(plan.rows, selectionPlannedRow{
			scopeID:  id,
			state:    scope.SelectionStateSelected,
			githubID: evaluation.listedGitHubIDs[id],
		})
	}
	plan.sort()
	return plan
}

// fullPlan plans a state for every known same-org scope (githubOrg mode):
// listed scopes stay selected, archived and rule-excluded scopes take their
// exclusion, and known scopes absent from every category read not_listed.
// A scope named in two categories (a caller bug: the categories partition
// one listing) resolves to the strongest claim, listed first.
func (evaluation normalizedSelectionEvaluation) fullPlan(known []string) selectionPlan {
	states := make(map[string]selectionPlannedRow, len(known))
	for _, id := range evaluation.ruleExcludedIDs {
		states[id] = selectionPlannedRow{scopeID: id, state: scope.SelectionStateRuleExcluded, githubID: evaluation.ruleGitHubIDs[id]}
	}
	for _, id := range evaluation.archivedIDs {
		states[id] = selectionPlannedRow{scopeID: id, state: scope.SelectionStateArchivedExcluded, githubID: evaluation.archivedGitHubs[id]}
	}
	for _, id := range evaluation.listedIDs {
		states[id] = selectionPlannedRow{scopeID: id, state: scope.SelectionStateSelected, githubID: evaluation.listedGitHubIDs[id]}
	}
	plan := selectionPlan{rows: make([]selectionPlannedRow, 0, len(known))}
	for _, id := range known {
		if row, ok := states[id]; ok {
			plan.rows = append(plan.rows, row)
			continue
		}
		plan.rows = append(plan.rows, selectionPlannedRow{scopeID: id, state: scope.SelectionStateNotListed})
	}
	plan.sort()
	return plan
}

func (plan *selectionPlan) sort() {
	slices.SortFunc(plan.rows, func(a, b selectionPlannedRow) int {
		return strings.Compare(a.scopeID, b.scopeID)
	})
}

// selectionNewlyMissing counts the planned not_listed rows whose prior row
// is missing or selected: scopes that were listed (or never seen) and now
// are not. Scopes already recorded as excluded do not count, so a small org
// with long-missing repositories does not trip the guard on every cycle.
func selectionNewlyMissing(plan selectionPlan, prior map[string]selectionPriorRow) int {
	newlyMissing := 0
	for _, row := range plan.rows {
		if row.state != scope.SelectionStateNotListed {
			continue
		}
		previous, ok := prior[row.scopeID]
		if !ok || previous.state == scope.SelectionStateSelected {
			newlyMissing++
		}
	}
	return newlyMissing
}

// selectionMassMissTripped reports whether the mass-miss guard fires: the
// newly-missing count exceeds max(10, 10%) of known scopes.
func selectionMassMissTripped(newlyMissing, known int) bool {
	threshold := known / 10
	if threshold < selectionMassMissFloor {
		threshold = selectionMassMissFloor
	}
	return newlyMissing > threshold
}

// readSelectionPriorRows reads one selector's stored rows into a
// scope-keyed map and returns the newest prior evaluated_at (zero when the
// selector never evaluated).
func readSelectionPriorRows(
	ctx context.Context,
	queryer db.Queryer,
	selectorID string,
) (map[string]selectionPriorRow, time.Time, error) {
	rows, err := queryer.QueryContext(ctx, selectionObservationPriorRowsQuery, selectorID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("record selection evaluation: read prior rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	prior := make(map[string]selectionPriorRow)
	var newest time.Time
	for rows.Next() {
		var scopeID, state string
		var evaluatedAt time.Time
		if scanErr := rows.Scan(&scopeID, &state, &evaluatedAt); scanErr != nil {
			return nil, time.Time{}, fmt.Errorf("record selection evaluation: read prior rows: %w", scanErr)
		}
		evaluatedAt = evaluatedAt.UTC()
		prior[scopeID] = selectionPriorRow{state: state, evaluatedAt: evaluatedAt}
		if evaluatedAt.After(newest) {
			newest = evaluatedAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("record selection evaluation: read prior rows: %w", err)
	}
	return prior, newest, nil
}

// readSelectionOrgScopes returns the sorted scope IDs of the known git
// default-branch repository scopes whose payload repo_slug sits under org.
// The org comparison lower-cases the stored slug: slugs are stored
// lower-cased but the read must not depend on that. Scopes without a slug
// are unactionable -- same-org membership cannot be proven for them -- so
// they stay out of the known set and their rows lapse into unknown.
func readSelectionOrgScopes(ctx context.Context, queryer db.Queryer, org string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, selectionObservationOrgScopesQuery)
	if err != nil {
		return nil, fmt.Errorf("record selection evaluation: read org scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	prefix := org + "/"
	known := make([]string, 0)
	for rows.Next() {
		var scopeID string
		var slug sql.NullString
		if scanErr := rows.Scan(&scopeID, &slug); scanErr != nil {
			return nil, fmt.Errorf("record selection evaluation: read org scopes: %w", scanErr)
		}
		if !slug.Valid || !strings.HasPrefix(strings.ToLower(slug.String), prefix) {
			continue
		}
		known = append(known, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("record selection evaluation: read org scopes: %w", err)
	}
	slices.Sort(known)
	return known, nil
}

// writeSelectionEvaluationRows upserts the planned rows in one ordered
// statement. The plan is non-empty and sorted by scope ID.
func writeSelectionEvaluationRows(
	ctx context.Context,
	exec db.Executor,
	evaluation normalizedSelectionEvaluation,
	plan selectionPlan,
) error {
	scopeIDs := make([]string, 0, len(plan.rows))
	states := make([]string, 0, len(plan.rows))
	githubIDs := make([]int64, 0, len(plan.rows))
	for _, row := range plan.rows {
		scopeIDs = append(scopeIDs, row.scopeID)
		states = append(states, row.state)
		githubIDs = append(githubIDs, row.githubID)
	}
	_, err := exec.ExecContext(
		ctx,
		recordSelectionEvaluationQuery,
		scopeIDs,
		evaluation.SelectorID,
		evaluation.EvaluatedAt,
		evaluation.LivenessWindowSeconds,
		states,
		githubIDs,
	)
	if err != nil {
		return fmt.Errorf("record selection evaluation: upsert rows: %w", err)
	}
	return nil
}
