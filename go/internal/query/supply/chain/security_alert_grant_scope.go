// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// securityAlertGrantCatalogMaxIDs bounds the grant-to-slug catalog read: one
// statement however many grants the scoped caller holds, but a grant set past
// the bound fails closed (bound_exceeded) instead of silently dropping grants,
// which would misreport target membership the way a truncated target read
// would. The bound covers the largest measured grant set (5,000;
// docs/internal/evidence/8837-security-alert-grant-scope.md).
const securityAlertGrantCatalogMaxIDs = 5000

// securityAlertGrantSlugs maps the scoped caller's grants to normalized
// owner/name repository slugs with one bounded catalog read (#8837). It
// returns the slugs and whether any repository grant mapped to no slug: a
// repository grant the catalog does not know (or whose entry carries no slug
// evidence) is unmapped, and the classifier reports
// repository_outside_target_set for it rather than silently ignoring a
// repository the caller may read. Scope grants that name a repository-kind
// catalog row or a git repository scope contribute that repository's slugs,
// except a row with no slug evidence, which leaves its grant unmapped; any
// other scope grant names no repository and is ignored.
func (h *Handler) securityAlertGrantSlugs(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
) ([]string, bool, error) {
	repoIDs := UniqueSortedNonEmpty(access.AllowedRepositoryIDs)
	for _, scopeID := range access.AllowedScopeIDs {
		// A git repository ingestion scope names the repository it owns, so
		// a token granted only the scope still maps to that repository's
		// slug (the same decoding WithCanonicalScopeRepositories applies).
		if repoID := querycontract.CanonicalRepositoryIDForScopeID(scopeID); repoID != "" {
			repoIDs = append(repoIDs, repoID)
		}
	}
	repoIDs = UniqueSortedNonEmpty(repoIDs)
	ids := UniqueSortedNonEmpty(append(append([]string(nil), repoIDs...), access.AllowedScopeIDs...))
	if len(ids) > securityAlertGrantCatalogMaxIDs {
		return nil, false, fmt.Errorf("security alert grant catalog read exceeds the %d-id bound: %w",
			securityAlertGrantCatalogMaxIDs, ErrSecurityAlertCollectionStateBound)
	}
	if h == nil || h.Content == nil {
		return nil, false, errors.New("security alert grant catalog read needs the repository catalog")
	}
	entries, err := h.Content.MatchRepositoriesByIDs(ctx, ids)
	if err != nil {
		return nil, false, fmt.Errorf("match repository catalog rows by ids: %w", err)
	}
	byKey := make(map[string][]querycontract.RepositoryCatalogEntry, len(entries))
	for _, entry := range entries {
		if entry.ID != "" {
			byKey[entry.ID] = append(byKey[entry.ID], entry)
		}
		if entry.ScopeID != "" && entry.ScopeID != entry.ID {
			byKey[entry.ScopeID] = append(byKey[entry.ScopeID], entry)
		}
	}
	slugsForKey := func(key string) []string {
		out := make([]string, 0, 1)
		for _, entry := range byKey[key] {
			out = append(out, securityAlertGrantEntrySlugs(entry)...)
		}
		return out
	}
	slugs := make([]string, 0, len(entries))
	unmapped := false
	for _, repoID := range repoIDs {
		grantSlugs := slugsForKey(repoID)
		if len(grantSlugs) == 0 {
			unmapped = true
			continue
		}
		slugs = append(slugs, grantSlugs...)
	}
	for _, scopeID := range UniqueSortedNonEmpty(access.AllowedScopeIDs) {
		grantSlugs := slugsForKey(scopeID)
		if len(grantSlugs) == 0 {
			// A scope grant with no slug evidence poisons the verdict
			// only when it names a repository-kind catalog row: any
			// other scope grant names no repository and stays ignored.
			if _, ok := byKey[scopeID]; ok {
				unmapped = true
			}
			continue
		}
		slugs = append(slugs, grantSlugs...)
	}
	return UniqueSortedNonEmpty(slugs), unmapped, nil
}

// securityAlertGrantEntrySlugs extracts the normalized owner/name slugs one
// catalog entry evidences, from its repo_slug or its remote URL, the same
// evidence the anchored selector path reads.
func securityAlertGrantEntrySlugs(entry querycontract.RepositoryCatalogEntry) []string {
	out := make([]string, 0, 2)
	for _, raw := range []string{entry.RepoSlug, entry.RemoteURL} {
		if slug := cleanSecurityAlertGitHubSlug(raw); slug != "" {
			if normalized := normalizeSecurityAlertRepository(slug); normalized != "" {
				out = append(out, normalized)
			}
		}
	}
	return out
}

// securityAlertGrantCoveringScopeIDs returns the sorted, unique scope ids of
// the targets that cover any granted slug. A granted slug no target covers
// contributes no scope id; the classifier reports it as
// repository_outside_target_set.
func securityAlertGrantCoveringScopeIDs(targets []securityAlertTargetConfig, slugs []string) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		for _, slug := range slugs {
			if securityAlertTargetCovers(target, slug) {
				out = append(out, target.ScopeID)
				break
			}
		}
	}
	return UniqueSortedNonEmpty(out)
}

// classifySecurityAlertGrantCollection turns the decoded targets and their
// states into one verdict over the caller's granted slugs (#8837). Each slug
// takes the best covering target (one healthy target makes that repository
// complete, the anchored semantic); the answer takes the worst slug, so one
// uncollected repository keeps the page from reading complete. A slug no
// target covers ranks below every state as repository_outside_target_set, as
// does a grant the catalog never mapped. Reasons are the union across slugs,
// the unanchored convention, so a collecting target beside an outside grant
// names both.
func classifySecurityAlertGrantCollection(
	targets []securityAlertTargetConfig,
	slugs []string,
	unmapped bool,
	states []SecurityAlertTargetState,
) securityAlertCoverageVerdict {
	if len(slugs) == 0 {
		return securityAlertOutsideTargetsVerdict()
	}
	byScope := make(map[string]SecurityAlertTargetState, len(states))
	for _, state := range states {
		byScope[state.ScopeID] = state
	}
	worstRank := 6
	var worst securityAlertCoverageVerdict
	var reasons []string
	if unmapped {
		worstRank, worst = 0, securityAlertOutsideTargetsVerdict()
		reasons = append(reasons, securityAlertReasonRepositoryOutsideTargets)
	}
	for _, slug := range slugs {
		rank, verdict := bestSecurityAlertGrantTarget(targets, byScope, slug)
		if rank < worstRank {
			worstRank, worst = rank, verdict
		}
		reasons = append(reasons, verdict.reasons...)
	}
	worst.reasons = UniqueSortedNonEmpty(reasons)
	return worst
}

// bestSecurityAlertGrantTarget ranks one granted slug over the targets that
// cover it, best wins. A slug no target covers is outside the target set. The
// per-slug state is narrowed to that slug before ranking, so another
// repository's uncollected generation or unread row cannot fail this one: a
// target whose latest committed generation did not collect the slug (#8838)
// ranks as still collecting (or a terminal failure that never collected), and
// only the slug's own unread row (#8943) counts.
func bestSecurityAlertGrantTarget(
	targets []securityAlertTargetConfig,
	byScope map[string]SecurityAlertTargetState,
	slug string,
) (int, securityAlertCoverageVerdict) {
	bestRank := 0
	best := securityAlertOutsideTargetsVerdict()
	for _, target := range targets {
		if !securityAlertTargetCovers(target, slug) {
			continue
		}
		state := byScope[target.ScopeID]
		narrowed := state
		if securityAlertGrantTargetUncollected(targets, state, slug) {
			narrowed.CommittedGeneration = false
		}
		if reason, ok := state.UnreadRequested[slug]; ok {
			narrowed.UnreadRequested = map[string]string{slug: reason}
		} else {
			narrowed.UnreadRequested = nil
		}
		if rank, verdict := classifySecurityAlertTarget(narrowed, true); rank > bestRank {
			bestRank, best = rank, verdict
		}
	}
	return bestRank, best
}

// securityAlertGrantTargetUncollected reports whether the state's latest
// committed generation did not collect slug although a configured target with
// the state's scope id covers it (#8838): a repository added to the target's
// allowlist, or a scope_id reused for another repository, after that
// generation.
func securityAlertGrantTargetUncollected(
	targets []securityAlertTargetConfig,
	state SecurityAlertTargetState,
	slug string,
) bool {
	if !slices.Contains(state.UncollectedRequested, slug) {
		return false
	}
	for _, target := range targets {
		if target.ScopeID == state.ScopeID && securityAlertTargetCovers(target, slug) {
			return true
		}
	}
	return false
}
