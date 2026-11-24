// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
// only other supports nodes allowed are a fetch whose Index Cond is the fact id (the primary
// key, or a (key, fact_id) index) and the operator-override probe whose Index Cond is the
// canonical key: a full scan of the primary-key or operator index with the column only as a
// Filter is a violation like any other.
func planShapeViolations(plan string, keyColumns ...string) []string {
	var out []string
	keyEq := regexp.MustCompile(`\b(` + strings.Join(keyColumns, "|") + `) = `)
	for _, n := range supportsNodes(plan) {
		conds := strings.Join(n.conds, " ")
		keyInCond := keyEq.MatchString(conds)
		keyInFilter := keyEq.MatchString(strings.Join(n.filters, " "))
		if n.kind == "Seq Scan" {
			out = append(out, "Seq Scan on supports")
		}
		if strings.HasSuffix(n.index, "_scope_generation_idx") {
			out = append(out, "supports probed from the active scopes ("+n.index+")")
		}
		// A fetch by fact id through any index whose Index Cond names the fact id (the primary key,
		// or a (key, fact_id) index the planner prefers) is a single-row probe, not the key leg.
		factFetch := strings.Contains(conds, "fact_id = ")
		switch {
		case factFetch:
			continue
		case strings.HasSuffix(n.index, "_pkey"):
			if !strings.Contains(conds, "fact_id = ") {
				out = append(out, "primary-key node without a fact_id Index Cond ("+n.kind+" using "+n.index+")")
			}
			continue
		case strings.HasSuffix(n.index, "_operator_idx"):
			if !strings.Contains(conds, "canonical_key = ") {
				out = append(out, "operator-override node without a canonical_key Index Cond ("+n.kind+" using "+n.index+")")
			}
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
// decide-first statement on the QA replica (custom plan and force_generic_plan, 2026-11-24T18:20Z,
// indexes 339, 340, 345 and 346 present) has a driving supports node that is an index or bitmap
// scan on the route's own key, no key-only filter, and no scope-driven probe. The fixtures are the
// supports-node projection of the real plans; the matching red plans are the *.red.txt files.
func TestKeyFirstPlansOnTheQAReplicaPassTheShapeCheck(t *testing.T) {
	t.Parallel()
	shapes := map[string]struct {
		key     explainDrivingKey
		columns []string
	}{
		"cve":                    {explainKeyCVE, []string{"cve_id"}},
		"advisory":               {explainKeyAdvisory, []string{"advisory_id", "cve_id"}},
		"cve_package":            {explainKeyCVE, []string{"cve_id"}},
		"identity_fact_id":       {explainKeyCVE, []string{"cve_id"}},
		"identity_finding_id":    {explainKeyCVE, []string{"cve_id"}},
		"identity_canonical_key": {explainKeyCVE, []string{"cve_id"}},
	}
	for shape, want := range shapes {
		for _, variant := range []string{"plain", "generic"} {
			fixture := "explain_item_f_" + shape + "_" + variant + "_qa_replica.green.txt"
			plan := readFixture(t, fixture)
			if nodes := supportsNodes(plan); len(nodes) == 0 {
				t.Fatalf("%s: no supports node parsed from the fixture", fixture)
			}
			if violations := planShapeViolations(plan, want.columns...); len(violations) != 0 {
				t.Fatalf("%s: violations = %v", fixture, violations)
			}
			// The fixture is the plan of exactly this statement: a changed statement
			// fails here until it is planned again on the replica.
			header := strings.SplitN(plan, "\n", 2)[0]
			sum := sha256.Sum256([]byte(ExplainFindingUngrantedByKeyQuery[want.key]))
			if wantStamp := "statement sha256=" + hex.EncodeToString(sum[:]); !strings.Contains(header, wantStamp) {
				t.Fatalf("%s: header %q does not carry %q: the %s statement changed since this plan was taken on the replica", fixture, header, wantStamp, want.key)
			}
		}
	}
}

// TestPlanShapeCheckerRejectsAFullPrimaryKeyOrOperatorScan seeds the two exemptions: a primary
// key or operator-index node that carries its column only as a Filter is a full index scan, not
// a fetch, and must fail like any other node.
func TestPlanShapeCheckerRejectsAFullPrimaryKeyOrOperatorScan(t *testing.T) {
	t.Parallel()
	pkeyFull := `
   ->  Index Scan using supply_chain_impact_finding_supports_pkey on supply_chain_impact_finding_supports support  (cost=0.4..9.0 rows=10 width=1)
         Filter: (fact_id = ANY ('{a}'::text[]))
`
	if v := planShapeViolations(pkeyFull, "cve_id"); !strings.Contains(strings.Join(v, "\n"), "primary-key node without a fact_id Index Cond") {
		t.Fatalf("violations = %v, want the full primary-key scan reported", v)
	}
	operatorFull := `
   ->  Index Scan using supply_chain_impact_finding_supports_operator_idx on supply_chain_impact_finding_supports operator  (cost=0.1..2.0 rows=1 width=1)
         Filter: (canonical_key = source.canonical_key)
`
	if v := planShapeViolations(operatorFull, "cve_id"); !strings.Contains(strings.Join(v, "\n"), "operator-override node without a canonical_key Index Cond") {
		t.Fatalf("violations = %v, want the full operator-index scan reported", v)
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

// explainStatementViolations checks one decide-first statement against the shape contract and
// returns every way it falls short (empty for a shipped statement).
func explainStatementViolations(key explainDrivingKey, query string, arms []string) []string {
	var out []string
	fail := func(format string, args ...any) {
		out = append(out, fmt.Sprintf("%s: "+format, append([]any{key}, args...)...))
	}
	fenceStart := strings.Index(query, "key_supports AS MATERIALIZED (")
	fenceEnd := strings.Index(query, "identity_keys AS (")
	streamEnd := strings.Index(query, "ambiguous_rows AS (")
	if fenceStart < 0 || fenceEnd < fenceStart || streamEnd < fenceEnd {
		return append(out, fmt.Sprintf("%s: the key leg is not a MATERIALIZED fence before the identity keys and the hits", key))
	}
	fence := query[fenceStart:fenceEnd]
	for _, arm := range arms {
		if !strings.Contains(fence, "AND "+arm+"\n") {
			fail("missing index leg %q in the fence", arm)
		}
	}
	if got := strings.Count(fence, "FROM supply_chain_impact_finding_supports AS fact"); got != len(arms) {
		fail("the fence reads supports %d times, want one per leg (%d)", got, len(arms))
	}
	// The fence is narrow: no payload and no source confidence cross it.
	if strings.Contains(fence, "fact.payload") || strings.Contains(fence, "source_confidence") {
		fail("the key fence carries the payload: it must stay a narrow tuple")
	}
	// Decide first, streaming: the first and second hit are LIMIT 1 reads of the lazy fence, the
	// second compares the canonical key before its authority probe, and nothing between the fence
	// and the second hit blocks (no sort, aggregate or distinct), so the scan can stop at two keys.
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
			fail("the statement lost %q", want)
		}
	}
	stream := query[fenceStart:streamEnd]
	for _, blocking := range []string{"ORDER BY", "GROUP BY", "DISTINCT", "array_agg", "count(", "unnest("} {
		if strings.Contains(stream, blocking) {
			fail("%q between the fence and the second hit blocks the early exit", blocking)
		}
	}
	if got := strings.Count(query, "LIMIT 1\n  ) AS authority"); got != 4 {
		fail("%d LIMITed authority probes, want 4 (identity keys, first hit, second hit, best ranked copy)", got)
	}
	// The probe is a lateral subquery, never a plain join the planner could order from a row
	// estimate, and the fence itself applies none.
	if strings.Contains(fence, "ingestion_scopes") || strings.Contains(fence, "scanner_image_result_fact") {
		fail("the key fence applies the per-candidate authority probes")
	}
	if strings.Contains(query, "JOIN ingestion_scopes AS scope\n    ON scope.scope_id = fact.scope_id") {
		fail("a plain join of the active scopes is back: the probe must stay a LIMITed lateral")
	}
	// The selector, grant, runtime-filter and identity predicates the legacy statement applies stay.
	for _, kept := range []string{
		"fact.subject_digest = $7", "generation.status = 'active'", "finding_association.finding_fact_id IS NULL",
		"fact.finding_id = $2", "fact.scope_id NOT LIKE 'image://derived/%'", "runtime_filter.filter_kind = 'service'",
	} {
		if !strings.Contains(query, kept) {
			fail("lost %q", kept)
		}
	}
	return out
}

func TestUngrantedExplainStatementsDriveFromTheirKey(t *testing.T) {
	t.Parallel()
	if len(ExplainFindingUngrantedByKeyQuery) != len(explainKeyLegs) || len(explainKeyLegs) != 2 {
		t.Fatalf("%d decide-first statements for %d key legs, want one per driving key (CVE and advisory only)", len(ExplainFindingUngrantedByKeyQuery), len(explainKeyLegs))
	}
	for _, refused := range []explainDrivingKey{"repository", "package"} {
		if _, ok := explainKeyLegs[refused]; ok {
			t.Fatalf("a %s-driven statement is back: it is reachable only as the identity fallback, which stays legacy", refused)
		}
	}
	for key, arms := range explainKeyLegs {
		if violations := explainStatementViolations(key, ExplainFindingUngrantedByKeyQuery[key], arms); len(violations) != 0 {
			t.Fatalf("violations: %v", violations)
		}
	}
}

// TestExplainStatementShapeCheckRejectsSeededViolations plants each way the streaming shape can
// be lost into the shipped CVE statement and requires the named shape check to report it.
func TestExplainStatementShapeCheckRejectsSeededViolations(t *testing.T) {
	t.Parallel()
	shipped := ExplainFindingUngrantedByKeyQuery[explainKeyCVE]
	arms := explainKeyLegs[explainKeyCVE]
	for name, tc := range map[string]struct {
		mutate func(string) string
		want   string
	}{
		"a blocking sort in the fence": {func(q string) string {
			return strings.Replace(q, "AND fact.cve_id <> ''\n", "AND fact.cve_id <> ''\n    ORDER BY fact.fact_id\n", 1)
		}, "blocks the early exit"},
		"no cheap compare before the second probe": {func(q string) string {
			return strings.Replace(q, "WHERE fact.canonical_key <> (SELECT first_key.canonical_key FROM first_hit AS first_key)\n    AND ", "WHERE ", 1)
		}, "lost"},
		"a plain join of the active scopes": {func(q string) string {
			return strings.Replace(q, "  CROSS JOIN LATERAL (\n    SELECT 1 AS authorized", "  JOIN ingestion_scopes AS scope\n    ON scope.scope_id = fact.scope_id\n  CROSS JOIN LATERAL (\n    SELECT 1 AS authorized", 1)
		}, "plain join"},
		"the payload in the fence": {func(q string) string {
			return strings.Replace(q, "fact.fact_id, fact.canonical_key, fact.scope_id,", "fact.payload, fact.fact_id, fact.canonical_key, fact.scope_id,", 1)
		}, "carries the payload"},
		"the runtime filter dropped": {func(q string) string {
			return strings.ReplaceAll(q, "runtime_filter.filter_kind = 'service'", "runtime_filter.filter_kind = 'none'")
		}, "lost"},
	} {
		mutated := tc.mutate(shipped)
		if mutated == shipped {
			t.Fatalf("%s: the seeded violation changed nothing", name)
		}
		violations := strings.Join(explainStatementViolations(explainKeyCVE, mutated, arms), "\n")
		if !strings.Contains(violations, tc.want) {
			t.Fatalf("%s: violations = %q, want one containing %q", name, violations, tc.want)
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
		"cve beats package":      {packet.ExplanationFilter{CVEID: "CVE-1", PackageID: "pkg:npm/a"}, explainKeyCVE},
		"advisory beats package": {packet.ExplanationFilter{AdvisoryID: "GHSA-1", PackageID: "pkg:npm/a", SubjectDigest: "sha256:x"}, explainKeyAdvisory},
		"identity with a cve":    {packet.ExplanationFilter{FindingID: "fact:x", CVEID: "CVE-1", PackageID: "pkg:npm/a"}, explainKeyCVE},
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
		"finding id and a package":   {FindingID: "finding:x", PackageID: "pkg:npm/a"},
		"finding id and repository":  {FindingID: "finding:x", RepositoryID: "repository:a"},
		"finding id and digest":      {FindingID: "finding:x", SubjectDigest: "sha256:x"},
		"package alone":              {PackageID: "pkg:npm/a"},
		"repository alone":           {RepositoryID: "repository:a"},
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
