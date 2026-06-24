// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extraction

import (
	"fmt"
	"sort"
	"strings"
)

// schemaCriteria are the criteria whose unmet state means a collector's facts or
// identity still co-evolve with core admission. They drive the "blocked by
// schema" explanation.
var schemaCriteria = map[Criterion]struct{}{
	SourceCoupling:  {},
	FactContract:    {},
	ScopeGeneration: {},
}

// Validate reports whether the profile covers every canonical criterion exactly
// once with a recognized state. It is used by the catalog test and by callers
// that build profiles dynamically; Evaluate tolerates incomplete profiles by
// treating any missing criterion as unmet so an authoring gap fails closed.
func (p Profile) Validate() error {
	if strings.TrimSpace(string(p.Family)) == "" {
		return fmt.Errorf("extraction: profile is missing a collector family")
	}
	seen := make(map[Criterion]int, len(p.Criteria))
	for _, result := range p.Criteria {
		if !result.Criterion.Valid() {
			return fmt.Errorf("extraction: %s has unknown criterion %q", p.Family, result.Criterion)
		}
		if !result.State.Valid() {
			return fmt.Errorf("extraction: %s criterion %s has unknown state %q", p.Family, result.Criterion, result.State)
		}
		seen[result.Criterion]++
	}
	for _, criterion := range orderedCriteria {
		switch seen[criterion] {
		case 1:
		case 0:
			return fmt.Errorf("extraction: %s is missing criterion %s", p.Family, criterion)
		default:
			return fmt.Errorf("extraction: %s lists criterion %s more than once", p.Family, criterion)
		}
	}
	return nil
}

// Evaluate computes the advisory extraction readiness for one collector family.
// It is deterministic and total: the same profile always yields the same
// verdict, and an incomplete profile fails closed by treating missing criteria
// as unmet. Evaluate never mutates the profile.
func Evaluate(p Profile) Readiness {
	criteria := normalizedCriteria(p.Criteria)
	blockers := make([]CriterionResult, 0, len(criteria))
	for _, result := range criteria {
		if result.State == Unmet {
			blockers = append(blockers, result)
		}
	}

	classification, rationale := classify(p, blockers)
	if strings.TrimSpace(p.Rationale) != "" {
		rationale = rationale + " " + strings.TrimSpace(p.Rationale)
	}

	readiness := Readiness{
		Family:         p.Family,
		DisplayName:    p.DisplayName,
		Classification: classification,
		Criteria:       criteria,
		Rationale:      rationale,
	}
	if classification == Blocked {
		readiness.Blockers = blockers
	}
	return readiness
}

// classify applies the policy rules in priority order. Correlation-critical
// cores stay in tree; any unmet criterion blocks; a clean candidate that has
// completed its proof and runs out of tree is external-ready; otherwise it is an
// extraction candidate.
func classify(p Profile, blockers []CriterionResult) (Classification, string) {
	if p.CorrelationCritical {
		return KeepInTree, "Correlation-critical core collector: it creates or preserves code-to-cloud join keys, so it stays in tree until a separate architecture gate proves a split keeps correlation correct."
	}
	if len(blockers) > 0 {
		return Blocked, blockedRationale(blockers)
	}
	if p.Extracted && p.BoundaryProofComplete {
		return ExternalReady, "Out-of-tree proof is complete and the family runs out of tree as its default path."
	}
	if p.BoundaryProofComplete {
		return ExtractionCandidate, "Extraction candidate: every criterion is met and the out-of-tree boundary proof is complete, but the family still runs in tree as the production default."
	}
	return ExtractionCandidate, "Extraction candidate: every criterion is met, but the out-of-tree boundary proof has not been completed yet."
}

// blockedRationale explains which class of criteria is unmet so a contributor
// can tell a schema/identity gap (blocked by schema) from a hosted-runtime gap
// (blocked by runtime).
func blockedRationale(blockers []CriterionResult) string {
	schema := false
	runtime := false
	others := false
	for _, blocker := range blockers {
		switch {
		case isSchemaCriterion(blocker.Criterion):
			schema = true
		case blocker.Criterion == RuntimeBehavior:
			runtime = true
		default:
			others = true
		}
	}
	reasons := make([]string, 0, 3)
	if schema {
		reasons = append(reasons, "blocked by schema: its fact contract or scope/generation identity still co-evolves with core admission")
	}
	if runtime {
		reasons = append(reasons, "blocked by runtime: the hosted path lacks bounded claims, isolation, or operator evidence")
	}
	if others {
		reasons = append(reasons, "blocked by an unmet trust, release-cadence, or proof criterion")
	}
	return "Not ready to move out of tree: " + strings.Join(reasons, "; ") + "."
}

func isSchemaCriterion(c Criterion) bool {
	_, ok := schemaCriteria[c]
	return ok
}

// normalizedCriteria returns the criteria in canonical order with each criterion
// present exactly once. Missing criteria are reported as unmet so an authoring
// gap fails closed instead of silently passing. Duplicates keep the first
// occurrence in the input.
func normalizedCriteria(in []CriterionResult) []CriterionResult {
	byCriterion := make(map[Criterion]CriterionResult, len(in))
	for _, result := range in {
		if !result.Criterion.Valid() {
			continue
		}
		if _, exists := byCriterion[result.Criterion]; exists {
			continue
		}
		byCriterion[result.Criterion] = result
	}
	out := make([]CriterionResult, 0, len(orderedCriteria))
	for _, criterion := range orderedCriteria {
		if result, ok := byCriterion[criterion]; ok {
			out = append(out, result)
			continue
		}
		out = append(out, CriterionResult{
			Criterion: criterion,
			State:     Unmet,
			Detail:    "no documented evidence for this criterion",
		})
	}
	return out
}

// SortReadiness orders readiness rows by classification severity and then by
// family so CLI and test output is stable. KeepInTree sorts first, then
// ExternalReady, ExtractionCandidate, and Blocked last so the most actionable
// rows are easy to find at the bottom.
func SortReadiness(rows []Readiness) {
	rank := map[Classification]int{
		KeepInTree:          0,
		ExternalReady:       1,
		ExtractionCandidate: 2,
		Blocked:             3,
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rank[rows[i].Classification] != rank[rows[j].Classification] {
			return rank[rows[i].Classification] < rank[rows[j].Classification]
		}
		return rows[i].Family < rows[j].Family
	})
}
