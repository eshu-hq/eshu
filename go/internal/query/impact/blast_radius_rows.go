// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"log/slog"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the pure row-shaping helpers for blast-radius results
// (dedup, sort, distinct-value extraction, tier-ambiguity grouping). They
// issue no graph queries, so they live apart from blast_radius.go to keep
// that dispatcher file under the 500-line cap without moving any
// query-owner symbol the queryplan manifest keys on (blastRadiusAffected,
// enrichBlastRadiusTiers).

// distinctRepoIDs returns the unique non-empty repo ids from the rows. Used to
// anchor the terraform_module dependents traversal on concrete source-repo ids
// rather than names, so same-named-but-unrelated repos are not pulled in.
func distinctRepoIDs(rows []map[string]any) []string {
	return distinctFieldValues(rows, "repo_id")
}

// distinctFieldValues returns the unique non-empty values of key across rows,
// preserving first-seen order.
func distinctFieldValues(rows []map[string]any, key string) []string {
	seen := make(map[string]bool, len(rows))
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		v := querycontract.StringVal(row, key)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// mergeBlastRadiusRows de-duplicates affected rows by repo name, keeping the
// minimum hop distance (so a repo reachable both directly and transitively, or
// a source repo that is also a dependent, is reported at its shortest path) and
// preserving the first-seen repo_id/claim. Results are sorted by (hops asc,
// repo asc) to match the removed Cypher ORDER BY. This is where per-repo
// min-hop lives now that the affected queries can no longer fold it across the
// UNION/two-query merge.
func mergeBlastRadiusRows(rows []map[string]any) []map[string]any {
	byRepo := make(map[string]map[string]any, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		name := querycontract.StringVal(row, "repo")
		if name == "" {
			continue
		}
		existing, ok := byRepo[name]
		if !ok {
			merged := map[string]any{"repo": name}
			if v := querycontract.StringVal(row, "repo_id"); v != "" {
				merged["repo_id"] = v
			}
			if v := querycontract.StringVal(row, "claim"); v != "" {
				merged["claim"] = v
			}
			merged["hops"] = querycontract.IntVal(row, "hops")
			byRepo[name] = merged
			order = append(order, name)
			continue
		}
		if hops := querycontract.IntVal(row, "hops"); hops < querycontract.IntVal(existing, "hops") {
			existing["hops"] = hops
		}
		if querycontract.StringVal(existing, "repo_id") == "" {
			if v := querycontract.StringVal(row, "repo_id"); v != "" {
				existing["repo_id"] = v
			}
		}
		if querycontract.StringVal(existing, "claim") == "" {
			if v := querycontract.StringVal(row, "claim"); v != "" {
				existing["claim"] = v
			}
		}
	}
	merged := make([]map[string]any, 0, len(order))
	for _, name := range order {
		merged = append(merged, byRepo[name])
	}
	sortBlastRadiusRows(merged)
	return merged
}

// sortBlastRadiusRows orders affected rows by ascending hop distance then repo
// name, matching the ORDER BY the affected Cypher applied before it was moved to
// Go (min-hop dedup can no longer rely on the query's ordering).
func sortBlastRadiusRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		hi, hj := querycontract.IntVal(rows[i], "hops"), querycontract.IntVal(rows[j], "hops")
		if hi != hj {
			return hi < hj
		}
		return querycontract.StringVal(rows[i], "repo") < querycontract.StringVal(rows[j], "repo")
	})
}

// blastRadiusRepoTier is one resolved (tier, risk) pair for a repository.
type blastRadiusRepoTier struct {
	tier string
	risk string
}

// blastRadiusRepoTiers groups blastRadiusTierLookupCypher rows per repo_id
// and resolves each group to a single (tier, risk) pair, or withholds it
// (#6590). The statement has no enforced per-repo cardinality (see its doc
// comment in blast_radius.go), so a repo whose rows disagree -- more than
// one DISTINCT (tier, risk) pair -- is ambiguous: it is left out of the
// returned map entirely (the caller then sets no tier/risk key), and logged
// so an operator can see which repo and how many candidates. Duplicate
// identical rows for the same repo collapse silently; they are the same
// fact observed more than once, not ambiguity, and still resolve normally.
func blastRadiusRepoTiers(rows []map[string]any, logger *slog.Logger) map[string]blastRadiusRepoTier {
	candidatesByRepo := make(map[string][]blastRadiusRepoTier, len(rows))
	for _, row := range rows {
		id := querycontract.StringVal(row, "repo_id")
		if id == "" {
			continue
		}
		candidate := blastRadiusRepoTier{
			tier: querycontract.StringVal(row, "tier"),
			risk: querycontract.StringVal(row, "risk"),
		}
		duplicate := false
		for _, existing := range candidatesByRepo[id] {
			if existing == candidate {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidatesByRepo[id] = append(candidatesByRepo[id], candidate)
		}
	}
	tiers := make(map[string]blastRadiusRepoTier, len(candidatesByRepo))
	for id, candidates := range candidatesByRepo {
		if len(candidates) > 1 {
			if logger != nil {
				logger.Warn("blast-radius tier enrichment ambiguous; withholding tier",
					"repo_id", id, "tier_candidates", len(candidates))
			}
			continue
		}
		tiers[id] = candidates[0]
	}
	return tiers
}
