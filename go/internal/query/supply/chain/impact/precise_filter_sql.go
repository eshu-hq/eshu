// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// supplyChainImpactPreciseMatchReasons are the match reasons that make a finding
// without a stamped detection profile precise. The shipped reads spell the same
// list inline (ListFindingsQuery, ListFindingsFromWinnersQuery, the aggregate and
// grouped winners reads, the supports reads); a test pins that every one of those
// texts contains this list in this order.
var supplyChainImpactPreciseMatchReasons = []string{
	"npm_semver_affected_range", "npm_semver_known_fixed",
	"nuget_semver_affected_range", "nuget_semver_known_fixed",
	"cargo_semver_affected_range", "cargo_semver_known_fixed",
	"hex_semver_affected_range", "hex_semver_known_fixed",
	"maven_range_match", "maven_known_fixed",
	"swift_semver_affected_range", "swift_semver_known_fixed",
}

// supplyChainImpactPrecisePredicateSQL is the precise detection filter over one
// table alias (the empty alias gives the text of a partial index predicate). It
// has the two arms of the shipped filter: a finding the writer stamped precise,
// and a finding from before profile tagging (an empty profile) whose status,
// observed version and match reason qualify. The supplement probe, the winners
// branch of route V and the partial index of migration 343 all use this one
// text, so the index predicate is implied by the read's WHERE clause.
func supplyChainImpactPrecisePredicateSQL(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	reasons := "'" + strings.Join(supplyChainImpactPreciseMatchReasons, "', '") + "'"
	return "(" + prefix + "detection_profile = 'precise' OR (" + prefix + "detection_profile = '' AND " +
		prefix + "impact_status IN ('affected_exact', 'not_affected_known_fixed') AND " +
		prefix + "observed_version <> '' AND " + prefix + "match_reason IN (" + reasons + ")))"
}

// SupportsPreciseIndexPredicateSQL is the WHERE clause of the partial index
// supply_chain_impact_finding_supports_precise_key_ordered_idx (migration 343).
// A change to the precise filter changes this text and needs a new index
// migration; TestPreciseIndexMigrationCarriesThePredicate fails until it has one.
var SupportsPreciseIndexPredicateSQL = supplyChainImpactPrecisePredicateSQL("")

// supplyChainImpactPreciseStatusPruneSQL keeps only the winners that can have a
// precise support: precise needs one of two statuses and a non-empty observed
// version, and both are parts of the canonical key, so every support of a key
// shares them with its winner. The writer invariant test
// (TestPreciseImpliesStatusAndObservedVersion) pins that precision implies both.
func supplyChainImpactPreciseStatusPruneSQL(alias string) string {
	return alias + ".impact_status IN ('affected_exact', 'not_affected_known_fixed') AND " + alias + ".observed_version <> ''"
}
