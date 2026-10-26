// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Closed reason classes for a declaration a version comparison cannot place.
// They are both the by_spec_kind keys in the response and the suffix source for
// the partial_reasons strings, so a consumer reads one vocabulary.
const (
	specKindRange      = "range"
	specKindAlias      = "alias"
	specKindUnresolved = "unresolved"
	specKindPartial    = "partial"
	// specKindOther holds any spec_kind this build does not know. The contract
	// tells readers to tolerate a new member, so it is counted, never dropped.
	specKindOther = "other"
)

// Reason strings for partial_reasons. Each names the spec kind whose
// declarations were left out of the comparison; the count is in
// excluded_from_comparison.by_spec_kind.
const (
	reasonRangeExcluded      = "runtime_declaration_range_specs_excluded"
	reasonAliasExcluded      = "runtime_declaration_alias_specs_excluded"
	reasonUnresolvedExcluded = "runtime_declaration_unresolved_specs_excluded"
	reasonPartialExcluded    = "runtime_declaration_partial_specs_excluded"
	reasonOtherExcluded      = "runtime_declaration_other_specs_excluded"
	// reasonNamesUnavailable names the degrade of the display-name read. The
	// rows are still correct; only repo_name is missing.
	reasonNamesUnavailable = "runtime_declaration_repository_names_unavailable"
)

var excludedReasons = map[string]string{
	specKindRange:      reasonRangeExcluded,
	specKindAlias:      reasonAliasExcluded,
	specKindUnresolved: reasonUnresolvedExcluded,
	specKindPartial:    reasonPartialExcluded,
	specKindOther:      reasonOtherExcluded,
}

// excludedSummary is the comparison's exclusion report.
type excludedSummary struct {
	// Total is the number of declarations the comparison could not place.
	Total int `json:"total"`
	// BySpecKind counts them by closed spec kind class.
	BySpecKind map[string]int `json:"by_spec_kind"`
}

// summarizeExcluded folds the grouped counts into closed classes. A spec kind
// outside the known five lands in "other".
func summarizeExcluded(rows []map[string]any) excludedSummary {
	summary := excludedSummary{BySpecKind: map[string]int{}}
	for _, row := range rows {
		count := querycontract.IntVal(row, "excluded_count")
		if count <= 0 {
			continue
		}
		class := querycontract.StringVal(row, "spec_kind")
		if _, known := excludedReasons[class]; !known || class == specKindOther {
			class = specKindOther
		}
		summary.BySpecKind[class] += count
		summary.Total += count
	}
	return summary
}

// partialReasons renders the reason strings for the summary, sorted so the
// response is deterministic.
func (s excludedSummary) partialReasons() []string {
	reasons := make([]string, 0, len(s.BySpecKind))
	for class := range s.BySpecKind {
		reasons = append(reasons, excludedReasons[class])
	}
	sort.Strings(reasons)
	return reasons
}
