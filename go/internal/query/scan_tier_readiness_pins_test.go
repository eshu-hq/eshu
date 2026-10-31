// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestPostgresSupplyChainImpactReadinessOSPackageCountUsesSemiJoinNotFanOut
// is a regression for a JOIN-vs-EXISTS review finding: an inner JOIN between
// os_package_active and scanner_worker_analysis_in_scope would make
// vulnerability_os_package's COUNT(*) fan out to
// COUNT(os_package rows) * COUNT(matching analysis rows) whenever more than
// one scanner_worker.analysis fact matches a scope (a second analyzer, or
// analyses spread across sibling generations since #8878, for example). The
// shipped query must use EXISTS so each os_package fact is counted at most
// once regardless of how many analysis rows match. The sibling key is
// pair-keyed first (same scope+generation, issue #9685) with a scope-level
// fallback for provably single-digest scopes: the generation equality alone
// would reintroduce the #8878 blind spot where a superseded analysis
// generation orphans the active generation's os_package rows, so the
// fallback must stay and must stay guarded by the multi-digest guard.
func TestPostgresSupplyChainImpactReadinessOSPackageCountUsesSemiJoinNotFanOut(t *testing.T) {
	t.Parallel()

	if strings.Contains(listSupplyChainImpactReadinessQuery, "JOIN scanner_worker_analysis_in_scope AS analysis") {
		t.Fatalf("listSupplyChainImpactReadinessQuery joins scanner_worker_analysis_in_scope directly into vulnerability_os_package's FROM clause, which fans out COUNT(*) whenever more than one analysis fact matches a scope; use EXISTS instead:\n%s", listSupplyChainImpactReadinessQuery)
	}
	if !strings.Contains(listSupplyChainImpactReadinessQuery, "FROM os_package_active AS os_package\n    WHERE EXISTS (") {
		t.Fatalf("listSupplyChainImpactReadinessQuery does not count vulnerability.os_package via an EXISTS semi-join on scanner_worker_analysis_in_scope:\n%s", listSupplyChainImpactReadinessQuery)
	}
	if !strings.Contains(listSupplyChainImpactReadinessQuery, "analysis.generation_id = os_package.generation_id") {
		t.Fatalf("listSupplyChainImpactReadinessQuery lost the pair-keyed primary sibling attribution (issue #9685); without it a multi-digest scope falls back to scope-level attribution and counts another image's packages:\n%s", listSupplyChainImpactReadinessQuery)
	}
	if !strings.Contains(listSupplyChainImpactReadinessQuery, "FROM scope_with_multiple_digests AS mixed") {
		t.Fatalf("listSupplyChainImpactReadinessQuery lost the multi-digest guard on the scope-level fallback (issue #9685); an unguarded fallback counts another image's packages on a rescanned scope:\n%s", listSupplyChainImpactReadinessQuery)
	}
}
