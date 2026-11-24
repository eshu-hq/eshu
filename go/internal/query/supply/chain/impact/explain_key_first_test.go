// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// The ungranted selector explain drives from its own key (#10880 PR-C1 f, arbiter
// ruling 2026-11-24 sections 7 and 8). The first statement of this read put the whole
// supports table in a NOT MATERIALIZED source joined to the active scopes, and on the
// QA replica the planner drove the join from the scopes and read every supports row
// through the scope index, with the selector only as a filter (cve, advisory, package,
// repository and identity shapes all hit the 20 s statement timeout). These tests pin
// the shape that replaces it.

// plan-shape checker -------------------------------------------------------------

var supportsNodeLine = regexp.MustCompile(
	`^(\s*)(->\s+)?(Parallel )?(Seq Scan|Index Scan|Index Only Scan|Bitmap Heap Scan) (using (\S+) )?on supply_chain_impact_finding_supports\b`)

// supportsNodes returns, for each plan node that reads supply_chain_impact_finding_supports,
// the access kind, the index it names and the cond/filter lines of its block (a bitmap heap
// scan's block includes its child bitmap index scans).
type supportsNode struct {
	kind, index string
	conds       []string // Index Cond and Recheck Cond lines
	filters     []string // Filter lines
}

func supportsNodes(plan string) []supportsNode {
	lines := strings.Split(plan, "\n")
	var nodes []supportsNode
	for i, line := range lines {
		m := supportsNodeLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		node := supportsNode{kind: m[4], index: m[6]}
		for _, next := range lines[i+1:] {
			trim := strings.TrimLeft(next, " ")
			if strings.TrimSpace(next) == "" {
				break
			}
			if len(next)-len(trim) <= indent+1 && strings.HasPrefix(trim, "->") {
				break
			}
			switch {
			case strings.HasPrefix(trim, "Index Cond:"), strings.HasPrefix(trim, "Recheck Cond:"):
				node.conds = append(node.conds, trim)
			case strings.HasPrefix(trim, "Filter:"):
				node.filters = append(node.filters, trim)
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// planShapeViolations applies the arbiter's section 7.2 to one plain EXPLAIN of a key-first
// ungranted explain: no sequential scan of supports; no supports node driven from the active
// scopes (the scope-generation index); no supports node that carries the route's key only as
// a Filter. keyColumns are the route's own key columns (advisory-or-CVE has two arms). The
// operator-override probe (operator index, canonical key) and primary-key lookups by fact id
// are the only supports nodes allowed to name something other than the key.
func planShapeViolations(plan string, keyColumns ...string) []string {
	var out []string
	keyEq := regexp.MustCompile(`\b(` + strings.Join(keyColumns, "|") + `) = `)
	for _, n := range supportsNodes(plan) {
		keyInCond := keyEq.MatchString(strings.Join(n.conds, " "))
		keyInFilter := keyEq.MatchString(strings.Join(n.filters, " "))
		if n.kind == "Seq Scan" {
			out = append(out, "Seq Scan on supports")
		}
		if strings.HasSuffix(n.index, "_scope_generation_idx") {
			out = append(out, "supports probed from the active scopes ("+n.index+")")
		}
		// the operator override probe and fact-id lookups are not the key leg
		if strings.HasSuffix(n.index, "_operator_idx") || strings.HasSuffix(n.index, "_pkey") {
			continue
		}
		switch {
		case keyInCond:
		case keyInFilter:
			out = append(out, "supports node carries the key only as a Filter ("+n.kind+" using "+n.index+")")
		case n.kind != "Seq Scan":
			out = append(out, "supports node without the route's key as an Index Cond ("+n.kind+" using "+n.index+")")
		}
	}
	return out
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(body)
}

// TestPlanShapeCheckerRejectsTheScopeDrivenItemFPlan is the seeded red half: the real plan
// the first item f statement got on the QA replica (cve and package selectors, custom plan)
// must fail the section 7.2 check, for the right reasons.
func TestPlanShapeCheckerRejectsTheScopeDrivenItemFPlan(t *testing.T) {
	t.Parallel()
	for fixture, column := range map[string]string{
		"explain_item_f_cve_plain_qa_replica.red.txt":     "cve_id",
		"explain_item_f_package_plain_qa_replica.red.txt": "package_id",
	} {
		violations := planShapeViolations(readFixture(t, fixture), column)
		joined := strings.Join(violations, "\n")
		if !strings.Contains(joined, "probed from the active scopes") || !strings.Contains(joined, "only as a Filter") {
			t.Fatalf("%s: violations = %v, want a scope-driven probe and a key-only-as-Filter node", fixture, violations)
		}
	}
}

// TestKeyFirstPlansOnTheQAReplicaPassTheShapeCheck is the green half: the plain EXPLAIN of each
// key-first statement on the QA replica (custom plan and force_generic_plan, 2026-11-24T16:30Z,
// indexes 339, 340, 345 and 346 present) has a driving supports node that is an index or bitmap
// scan on the route's own key, no key-only filter, and no scope-driven probe. The fixtures are the
// supports-node projection of the real plans; the matching red plans are the *.red.txt files.
func TestKeyFirstPlansOnTheQAReplicaPassTheShapeCheck(t *testing.T) {
	t.Parallel()
	keys := map[string][]string{
		"cve": {"cve_id"}, "advisory": {"advisory_id", "cve_id"}, "package": {"package_id"}, "repository": {"repository_id"},
	}
	for key, columns := range keys {
		for _, variant := range []string{"plain", "generic"} {
			fixture := "explain_item_f_" + key + "_" + variant + "_qa_replica.green.txt"
			plan := readFixture(t, fixture)
			if nodes := supportsNodes(plan); len(nodes) == 0 {
				t.Fatalf("%s: no supports node parsed from the fixture", fixture)
			}
			if violations := planShapeViolations(plan, columns...); len(violations) != 0 {
				t.Fatalf("%s: violations = %v", fixture, violations)
			}
		}
	}
}

func TestPlanShapeCheckerAcceptsAKeyFirstPlan(t *testing.T) {
	t.Parallel()
	good := `
 Limit  (cost=1.0..2.0 rows=2 width=8)
   CTE key_supports
     ->  Index Scan using supply_chain_impact_finding_supports_cve_idx on supply_chain_impact_finding_supports  (cost=0.4..9.0 rows=10 width=1)
           Index Cond: ((cve_id = $4) AND (cve_id <> ''::text))
   ->  Index Scan using supply_chain_impact_finding_supports_operator_idx on supply_chain_impact_finding_supports operator  (cost=0.1..2.0 rows=1 width=1)
         Index Cond: (canonical_key = source.canonical_key)
`
	if v := planShapeViolations(good, "cve_id"); len(v) != 0 {
		t.Fatalf("violations on a key-first plan: %v", v)
	}
	bitmap := `
 CTE key_supports
   ->  Bitmap Heap Scan on supply_chain_impact_finding_supports  (cost=1.0..2.0 rows=10 width=1)
         Recheck Cond: ((package_id = $5) AND (package_id <> ''::text))
         ->  Bitmap Index Scan on supply_chain_impact_finding_supports_package_idx  (cost=0.0..1.0 rows=10 width=0)
               Index Cond: ((package_id = $5) AND (package_id <> ''::text))
`
	if v := planShapeViolations(bitmap, "package_id"); len(v) != 0 {
		t.Fatalf("violations on a bitmap key leg: %v", v)
	}
}

func TestPlanShapeCheckerRejectsSeqScanAndFilterOnlyKey(t *testing.T) {
	t.Parallel()
	seq := ` ->  Seq Scan on supply_chain_impact_finding_supports fact  (cost=0..1 rows=1 width=1)
         Filter: (cve_id = $4)
`
	if v := planShapeViolations(seq, "cve_id"); len(v) == 0 || !strings.Contains(v[0], "Seq Scan") {
		t.Fatalf("seq scan not rejected: %v", v)
	}
	other := ` ->  Index Scan using supply_chain_impact_finding_supports_repository_idx on supply_chain_impact_finding_supports fact  (cost=0..1 rows=1 width=1)
         Index Cond: (repository_id = $6)
         Filter: (cve_id = $4)
`
	if v := planShapeViolations(other, "cve_id"); len(v) == 0 || !strings.Contains(v[0], "only as a Filter") {
		t.Fatalf("key-only-as-Filter not rejected: %v", v)
	}
}

// statement shape and routing -----------------------------------------------------

func TestUngrantedExplainStatementsDriveFromTheirKey(t *testing.T) {
	t.Parallel()
	if len(ExplainFindingUngrantedByKeyQuery) != len(explainKeyLegs) || len(explainKeyLegs) != 4 {
		t.Fatalf("%d decide-first statements for %d key legs, want one per indexed selector (4)", len(ExplainFindingUngrantedByKeyQuery), len(explainKeyLegs))
	}
	for key, arms := range explainKeyLegs {
		query := ExplainFindingUngrantedByKeyQuery[key]
		fenceStart := strings.Index(query, "key_supports AS MATERIALIZED (")
		fenceEnd := strings.Index(query, "identity_keys AS (")
		if fenceStart < 0 || fenceEnd < fenceStart {
			t.Fatalf("%s: the key leg is not a MATERIALIZED fence before the identity keys:\n%s", key, query)
		}
		fence := query[fenceStart:fenceEnd]
		for _, arm := range arms {
			if !strings.Contains(fence, "AND "+arm+"\n") {
				t.Fatalf("%s: missing index leg %q in the fence", key, arm)
			}
		}
		if got := strings.Count(fence, "FROM supply_chain_impact_finding_supports AS fact"); got != len(arms) {
			t.Fatalf("%s: the fence reads supports %d times, want one per leg (%d)", key, got, len(arms))
		}
		// Stage 1 is narrow: no payload and no source confidence cross the fence.
		if strings.Contains(fence, "fact.payload") || strings.Contains(fence, "source_confidence") {
			t.Fatalf("%s: the key fence carries the payload: it must stay a narrow tuple", key)
		}
		// Decide first, streaming: the first hit and the second hit are LIMIT 1 reads of the
		// lazy fence, the second one compares the canonical key before its authority probe,
		// and nothing between the fence and the second hit blocks (no sort, aggregate or
		// distinct), so the scan can stop at two canonical keys.
		for _, want := range []string{
			"first_hit AS MATERIALIZED (",
			"second_hit AS MATERIALIZED (",
			"WHERE fact.canonical_key <> (SELECT first_key.canonical_key FROM first_hit AS first_key)",
			"WHERE NOT EXISTS (SELECT 1 FROM second_hit)",
			"ranked_copies AS MATERIALIZED (",
			"best_copy AS MATERIALIZED (",
			"ORDER BY fact.priority_score DESC, fact.has_payload_finding_id DESC, fact.fact_id ASC",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s: the statement lost %q", key, want)
			}
		}
		streamStart := strings.Index(query, "key_supports AS MATERIALIZED (")
		streamEnd := strings.Index(query, "ambiguous_rows AS (")
		stream := query[streamStart:streamEnd]
		for _, blocking := range []string{"ORDER BY", "GROUP BY", "DISTINCT", "array_agg", "count(", "unnest("} {
			if strings.Contains(stream, blocking) {
				t.Fatalf("%s: %q between the fence and the second hit blocks the early exit", key, blocking)
			}
		}
		if got := strings.Count(query, "LIMIT 1\n  ) AS authority"); got != 4 {
			t.Fatalf("%s: %d LIMITed authority probes, want 4 (identity keys, first hit, second hit, best ranked copy)", key, got)
		}
		// The probe runs after the cheap compare: it is a lateral subquery, never a plain join the
		// planner could order from a row estimate, and the fence itself applies none.
		if strings.Contains(fence, "ingestion_scopes") || strings.Contains(fence, "scanner_image_result_fact") {
			t.Fatalf("%s: the key fence applies the per-candidate authority probes", key)
		}
		if strings.Contains(query, "JOIN ingestion_scopes AS scope\n    ON scope.scope_id = fact.scope_id") {
			t.Fatalf("%s: a plain join of the active scopes is back: the probe must stay a LIMITed lateral", key)
		}
		// The selector, grant and identity predicates the legacy statement applies stay.
		for _, kept := range []string{"fact.subject_digest = $7", "generation.status = 'active'", "finding_association.finding_fact_id IS NULL", "fact.finding_id = $2", "fact.scope_id NOT LIKE 'image://derived/%'"} {
			if !strings.Contains(query, kept) {
				t.Fatalf("%s: lost %q", key, kept)
			}
		}
	}
}

func TestUngrantedExplainRoutePicksTheDrivingKey(t *testing.T) {
	t.Parallel()
	store := PostgresFindingStore{ReadFromWinners: true}
	for name, tc := range map[string]struct {
		filter packet.ExplanationFilter
		key    explainDrivingKey
	}{
		"cve":                    {packet.ExplanationFilter{CVEID: "CVE-1"}, explainKeyCVE},
		"advisory":               {packet.ExplanationFilter{AdvisoryID: "GHSA-1"}, explainKeyAdvisory},
		"repository":             {packet.ExplanationFilter{RepositoryID: "repository:a"}, explainKeyRepository},
		"package":                {packet.ExplanationFilter{PackageID: "pkg:npm/a"}, explainKeyPackage},
		"cve beats package":      {packet.ExplanationFilter{CVEID: "CVE-1", PackageID: "pkg:npm/a"}, explainKeyCVE},
		"advisory beats package": {packet.ExplanationFilter{AdvisoryID: "GHSA-1", PackageID: "pkg:npm/a", SubjectDigest: "sha256:x"}, explainKeyAdvisory},
		"identity with a cve":    {packet.ExplanationFilter{FindingID: "fact:x", CVEID: "CVE-1", PackageID: "pkg:npm/a"}, explainKeyCVE},
		"repository beats pkg":   {packet.ExplanationFilter{RepositoryID: "repository:a", PackageID: "pkg:npm/a"}, explainKeyRepository},
	} {
		got := store.explainRoute(tc.filter)
		if got.selector != ExplainFindingUngrantedByKeyQuery[tc.key] || got.selectorRoute != RouteSupportsSelector {
			t.Errorf("%s: selector route = %q (key-first %q expected)", name, got.selectorRoute, tc.key)
		}
		if got.publicIDRoute != RouteSupportsFindingID || !got.supports {
			t.Errorf("%s: public id lookup moved: route %q supports %v", name, got.publicIDRoute, got.supports)
		}
	}
	// No indexed selector: the lookup keeps the legacy read until its own migration exists.
	for name, filter := range map[string]packet.ExplanationFilter{
		"subject digest only":        {SubjectDigest: "sha256:x"},
		"bare identity, fact id":     {FindingID: "fact:x"},
		"bare identity, finding id":  {FindingID: "finding:x"},
		"workload and service only":  {WorkloadID: "workload:a", ServiceID: "service:a"},
		"subject digest and finding": {FindingID: "k", SubjectDigest: "sha256:x"},
	} {
		got := store.explainRoute(filter)
		if got.selector != ExplainFindingQuery || got.selectorRoute != RouteLegacy {
			t.Errorf("%s: selector route = %q, want the legacy read", name, got.selectorRoute)
		}
	}
	// An image reference still takes the image route, and a grant the grant arms.
	if got := store.explainRoute(packet.ExplanationFilter{CVEID: "CVE-1", ImageRef: "registry.example/a:1"}); got.selectorRoute != RouteSupportsImage {
		t.Errorf("image call route = %q", got.selectorRoute)
	}
	if got := store.explainRoute(packet.ExplanationFilter{CVEID: "CVE-1", AllowedScopeIDs: []string{"scope:a"}}); got.selectorRoute != RouteSupportsGrant {
		t.Errorf("granted call route = %q", got.selectorRoute)
	}
	if got := (PostgresFindingStore{}).explainRoute(packet.ExplanationFilter{CVEID: "CVE-1"}); got.selectorRoute != RouteLegacy {
		t.Errorf("gate off route = %q", got.selectorRoute)
	}
}
