// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// Fragments of the #9611 per-(package manager, name) probe. The planner
// ignores expression statistics on partial indexes, so a flat
// "= ANY($1)" / "= ANY($2)" read scanned the whole package-manager prefix
// on every page. Each requested pair now drives its own index probe.
const (
	manifestProbeManagers  = "FROM (SELECT DISTINCT manager FROM unnest($1::text[]) AS manager) AS requested_manager"
	manifestProbeNames     = "CROSS JOIN (SELECT DISTINCT name FROM unnest($2::text[]) AS name) AS requested_name"
	manifestProbeLateral   = "CROSS JOIN LATERAL ("
	manifestProbeManagerEq = "fact.payload->'entity_metadata'->>'package_manager' = requested_manager.manager"
	manifestProbeNameEq    = manifestDependencyEntityNameFold + " = requested_name.name"
	manifestProbeInnerTop  = "ORDER BY fact.observed_at ASC, fact.fact_id ASC\n    LIMIT $5\n) AS page"
	manifestProbeOuterTop  = "ORDER BY page.observed_at ASC, page.fact_id ASC\nLIMIT $5"
	manifestFlatManagerAny = "fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])"
	manifestFlatNameAny    = manifestDependencyEntityNameFold + " = ANY($2::text[])"
)

// manifestNameProbeViolations returns every way one manifest query text
// (active or derived base) departs from the #9611 probe shape: one probe per
// text, both inputs de-duplicated, the probe's inner top-L matching the
// outer page, and no flat array match on the manager or the folded name.
//
// It compares whitespace-collapsed text, so the Maven-family branch's deeper
// indentation does not matter.
func manifestNameProbeViolations(raw string) []string {
	text := squashSQLSpace(raw)
	var violations []string
	for _, fragment := range []string{
		manifestProbeManagers, manifestProbeNames, manifestProbeLateral,
		manifestProbeManagerEq, manifestProbeNameEq, manifestProbeInnerTop,
	} {
		want := squashSQLSpace(fragment)
		if got := strings.Count(text, want); got != 1 {
			violations = append(violations, "want exactly one "+want)
		}
	}
	if !strings.Contains(text, squashSQLSpace(manifestProbeOuterTop)) {
		violations = append(violations, "missing the outer page "+manifestProbeOuterTop)
	}
	for _, flat := range []string{manifestFlatManagerAny, manifestFlatNameAny} {
		if strings.Contains(text, squashSQLSpace(flat)) {
			violations = append(violations, "keeps the flat array match "+flat)
		}
	}
	// The fact branch, with its active joins, must sit inside the probe:
	// a branch outside it would scan the manager prefix again.
	lateral := strings.Index(text, squashSQLSpace(manifestProbeLateral))
	inner := strings.Index(text, squashSQLSpace(manifestProbeInnerTop))
	branch := strings.Index(text, "FROM fact_records AS fact")
	if lateral < 0 || inner < 0 || branch < lateral || branch > inner {
		violations = append(violations, "the first fact branch is not inside the per-name probe")
	}
	return violations
}

// TestManifestDependencyQueriesProbeEachRequestedName pins the #9611 shape
// on both active texts and on the base text deriveDeltaOverlayBaseQuery
// derives from each, so the delta base pages get the same probe.
func TestManifestDependencyQueriesProbeEachRequestedName(t *testing.T) {
	t.Parallel()

	for name, active := range map[string]string{
		"plain":        listActivePackageManifestDependencyFactsQuery,
		"maven_family": listActivePackageManifestDependencyFactsWithSBTQuery,
	} {
		base, err := deriveDeltaOverlayBaseQuery(active, manifestDeltaOverlayScopeParam)
		if err != nil {
			t.Fatalf("%s: derive base: %v", name, err)
		}
		for kind, text := range map[string]string{"active": active, "base": base} {
			if violations := manifestNameProbeViolations(text); len(violations) > 0 {
				t.Errorf("%s %s text departs from the per-name probe:\n  %s", name, kind, strings.Join(violations, "\n  "))
			}
		}
		// The base swap must land inside the probe, beside the active
		// branch's own joins.
		flat := squashSQLSpace(base)
		inner := strings.Index(flat, squashSQLSpace(manifestProbeInnerTop))
		for _, swapped := range []string{deltaOverlayBaseJoin, "AND fact.generation_id = ANY($6::text[])"} {
			at := strings.Index(flat, squashSQLSpace(swapped))
			if at < strings.Index(flat, manifestProbeLateral) || at > inner {
				t.Errorf("%s base text: %q is not inside the per-name probe", name, swapped)
			}
		}
	}
}

// TestManifestNameProbeGuardFiresOnTheFlatShape is the seeded-violation pair:
// the guard must fire on the flat = ANY($2) name match the read used before
// #9611, on a missing inner limit, and stay quiet on the shipped texts.
func TestManifestNameProbeGuardFiresOnTheFlatShape(t *testing.T) {
	t.Parallel()

	shipped := listActivePackageManifestDependencyFactsQuery
	for name, seeded := range map[string]string{
		"flat name":        strings.Replace(shipped, manifestProbeNameEq, manifestFlatNameAny, 1),
		"flat manager":     strings.Replace(shipped, manifestProbeManagerEq, manifestFlatManagerAny, 1),
		"no inner limit":   strings.Replace(shipped, manifestProbeInnerTop, "ORDER BY fact.observed_at ASC, fact.fact_id ASC\n) AS page", 1),
		"names not unique": strings.Replace(shipped, "SELECT DISTINCT name", "SELECT name", 1),
	} {
		if len(manifestNameProbeViolations(seeded)) == 0 {
			t.Errorf("guard is quiet on the seeded %s violation", name)
		}
	}
	if violations := manifestNameProbeViolations(shipped); len(violations) > 0 {
		t.Errorf("guard fires on the shipped plain text: %v", violations)
	}
}

// squashSQLSpace collapses every whitespace run to one space.
func squashSQLSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
