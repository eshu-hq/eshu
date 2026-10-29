// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
)

func TestAdvisoryEvidenceQuerySeedsFromFactRecordsNotBroadActiveSet(t *testing.T) {
	t.Parallel()

	if strings.Contains(listAdvisoryEvidenceQuery, "FROM active") {
		t.Fatalf("advisory evidence query must not seed or return from a broad active CTE:\n%s", listAdvisoryEvidenceQuery)
	}
	seedStart := strings.Index(listAdvisoryEvidenceQuery, "seed AS (")
	matchesStart := strings.Index(listAdvisoryEvidenceQuery, "matched_facts AS (")
	if seedStart < 0 || matchesStart < 0 {
		t.Fatalf("query must keep selector seed and matched facts as explicit CTEs:\n%s", listAdvisoryEvidenceQuery)
	}
	if seedStart > matchesStart {
		t.Fatalf("query must derive seed keys before matching facts:\n%s", listAdvisoryEvidenceQuery)
	}
	for _, want := range []string{
		// Legs probe fact_records through a lookup-driven LATERAL
		// subquery (issue #8870): the probe order is enforced, not
		// planned, so the pin is on the subquery source, not a join.
		"FROM fact_records AS fact",
		"fact.fact_kind = ANY($1::text[])",
		"fact.fact_kind IN (",
		"'vulnerability.reference'",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"LIMIT $5",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing %q:\n%s", want, listAdvisoryEvidenceQuery)
		}
	}
}

func TestAdvisoryEvidenceQueryUsesIndexableJSONBPredicates(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{
		"FROM jsonb_array_elements_text",
		"UPPER(payload->>'cve_id')",
		"UPPER(payload->>'advisory_id')",
		"UPPER(payload->>'ghsa_id')",
		"ANY(keys.values)",
		"fact.payload->'aliases' ? lookup.value",
		"fact.payload->'correlation_anchors' ? lookup.value",
	} {
		if strings.Contains(listAdvisoryEvidenceQuery, forbidden) {
			t.Fatalf("listAdvisoryEvidenceQuery contains unbounded predicate %q:\n%s", forbidden, listAdvisoryEvidenceQuery)
		}
	}
	for _, want := range []string{
		"JOIN LATERAL unnest(keys.values) AS lookup(value) ON TRUE",
		"fact.payload->>'cve_id' = lookup.value",
		"fact.payload->>'advisory_id' = lookup.value",
		"fact.payload->>'ghsa_id' = lookup.value",
		"key_source = 'identity'",
		"key_source = 'alias'",
		"UPPER(TRIM(key_value)) LIKE 'CVE-%'",
		"fact.payload->>'package_id' = pkg.value",
		"fact.payload->>'purl' = pkg.value",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing %q:\n%s", want, listAdvisoryEvidenceQuery)
		}
	}
}

func TestAdvisoryEvidenceQueryProbesLookupValueFirstWithoutScopeFanout(t *testing.T) {
	t.Parallel()

	// Issue #8870 advisory-evidence timeout: every seed/matched leg joined
	// scope_active_generations ON TRUE, fanning each lookup value out to one
	// probe per active scope (~1476) before DISTINCT ON/LIMIT materialized.
	// Legs must drive from the lookup value into the payload-key probe and
	// test active-generation membership with EXISTS instead.
	if strings.Contains(listAdvisoryEvidenceQuery, "JOIN scope_active_generations AS scope ON TRUE") {
		t.Fatalf("advisory evidence query must not fan out over scope_active_generations with JOIN ON TRUE:\n%s", listAdvisoryEvidenceQuery)
	}
	// Each package OR leg must be split so every leg carries a single
	// payload-key equality probe the planner can feed to an index.
	if strings.Contains(listAdvisoryEvidenceQuery, "(fact.payload->>'package_id' = pkg.value OR fact.payload->>'purl' = pkg.value)") {
		t.Fatalf("advisory evidence query must split the package OR into per-key legs:\n%s", listAdvisoryEvidenceQuery)
	}
	if !strings.Contains(listAdvisoryEvidenceQuery, "FROM scope_active_generations AS scope") {
		t.Fatalf("advisory evidence query must keep active-generation membership as EXISTS over scope_active_generations:\n%s", listAdvisoryEvidenceQuery)
	}
	// Each affected_package slice must pin the affected_package kind
	// literal so the planner can use the package_id/purl partial indexes
	// that are partial on vulnerability.affected_package. GitLab CVE
	// envelopes also carry package_id
	// (gitlab_gemnasium_envelope.go newGitLabCVEEnvelope), so the
	// affected_package pin alone would drop package-matching rows of
	// other kinds (issue #9474 P1): the four five-kind slices below
	// cover them. Without the literals the legs fall back to per-scope
	// keyset scans (~65s on the primary, issue #8870).
	if got := strings.Count(listAdvisoryEvidenceQuery, "AND fact.fact_kind = 'vulnerability.affected_package'"); got != 4 {
		t.Fatalf("advisory evidence query has %d package-leg kind literals, want 4 (seed package_id, seed purl, matched package_id, matched purl):\n%s", got, listAdvisoryEvidenceQuery)
	}
	// Each five-kind slice must pin the exact five-kind IN (every
	// advisory kind except affected_package) so the planner proves the
	// fact_records_vulnerability_active_package_lookup_v2_idx /
	// fact_records_vulnerability_active_purl_lookup_v2_idx partial
	// predicates while the shared six-kind IN envelope stays intact.
	fiveKindSlice := `AND fact.fact_kind IN (
                'vulnerability.cve',
                'vulnerability.affected_product',
                'vulnerability.epss_score',
                'vulnerability.known_exploited',
                'vulnerability.reference'
            )`
	if got := strings.Count(listAdvisoryEvidenceQuery, fiveKindSlice); got != 4 {
		t.Fatalf("advisory evidence query has %d five-kind package slices, want 4 (seed package_id, seed purl, matched package_id, matched purl):\n%s", got, listAdvisoryEvidenceQuery)
	}
	// OFFSET 0 keeps each active-generation EXISTS a correlated subplan:
	// without it the planner flattens the membership test into a
	// scope-outer semi-join and reintroduces one probe per active scope
	// (~1476) on every leg. All fourteen legs must carry it.
	if got := strings.Count(listAdvisoryEvidenceQuery, "OFFSET 0"); got != 14 {
		t.Fatalf("advisory evidence query has %d OFFSET 0 guards, want 14 (one per leg):\n%s", got, listAdvisoryEvidenceQuery)
	}
}

func TestAdvisoryEvidenceImpactCandidatesProbeRepositoryFirst(t *testing.T) {
	t.Parallel()

	// Issue #9487: the impact_candidates CTE fanned every impact-scoped
	// input out to one probe per active scope before DISTINCT ON/LIMIT
	// materialized (~900ms of a 2.8s impact query on the primary). A
	// repository-anchored input must drive from the repository-key probe
	// into the payload index and test active-generation membership after,
	// while every other selector shape keeps the scope-driven fan-in.
	for _, want := range []string{
		"repo_probe AS MATERIALIZED",
		"fact.payload->>'repository_id' = selector.repository_id",
		"AND selector.repository_id <> ''",
		"AND s2.repository_id = ''",
		"UNION ALL",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing repository-first probe %q:\n%s", want, listAdvisoryEvidenceQuery)
		}
	}
	// The repository OR predicate must not survive as a per-row filter:
	// with the probe it is enforced by the branch guards instead.
	if strings.Contains(listAdvisoryEvidenceQuery, "(selector.repository_id = '' OR fact.payload->>'repository_id' = selector.repository_id)") {
		t.Fatalf("listAdvisoryEvidenceQuery keeps the scope-fan-in repository filter:\n%s", listAdvisoryEvidenceQuery)
	}
}

func TestAdvisoryEvidenceLookupIDsStaySeparateFromPackageScope(t *testing.T) {
	t.Parallel()

	got := advisory.EvidenceLookupIDs(advisory.EvidenceFilter{
		CVEID:     " cve-2026-0002 ",
		ID:        " GHSA-aaaa-bbbb-cccc ",
		PackageID: "pkg:npm/example",
	})
	if joined := strings.Join(got, ","); joined != "CVE-2026-0002,GHSA-aaaa-bbbb-cccc" {
		t.Fatalf("advisoryEvidenceLookupIDs() = %#v, want only normalized advisory ids", got)
	}
}
