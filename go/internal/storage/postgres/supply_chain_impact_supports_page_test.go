// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// supportsPageArmsForTest is the four-arm reconciliation shape, the widest the
// builder renders.
var supportsPageArmsForTest = securityAlertReconciliationSupportsArms

// lateralBodies returns the text of every `CROSS JOIN LATERAL ( ... ) x` fence of
// a rendered page statement.
func lateralBodies(statement string) []string {
	var bodies []string
	rest := statement
	for {
		i := strings.Index(rest, "CROSS JOIN LATERAL (")
		if i < 0 {
			return bodies
		}
		rest = rest[i+len("CROSS JOIN LATERAL ("):]
		j := strings.Index(rest, ") x")
		if j < 0 {
			return bodies
		}
		bodies = append(bodies, rest[:j])
		rest = rest[j:]
	}
}

// TestSupplyChainImpactSupportsPageQueryFencesEveryArm pins the statement
// contract of the hot-key ruling: each arm is a per-key ordered LIMIT scan with
// nothing about the scope tables, no join, no EXISTS and no subquery inside it,
// so the planner cannot drive the read from the active scopes.
func TestSupplyChainImpactSupportsPageQueryFencesEveryArm(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"first page": supplyChainImpactSupportsPageQuery(supportsPageArmsForTest, 0),
		"next page":  supplyChainImpactSupportsPageQuery(supportsPageArmsForTest, 5),
	} {
		bodies := lateralBodies(statement)
		if len(bodies) != len(supportsPageArmsForTest) {
			t.Fatalf("%s: %d lateral fences, want %d", name, len(bodies), len(supportsPageArmsForTest))
		}
		for _, body := range bodies {
			for _, banned := range []string{"ingestion_scopes", "scope_generations", "EXISTS", "JOIN", "(SELECT"} {
				if strings.Contains(body, banned) {
					t.Errorf("%s: a lateral fence contains %q; the LIMIT is the fence and the active check lives outside it:\n%s", name, banned, body)
				}
			}
			if !strings.Contains(body, "ORDER BY s.fact_id LIMIT 500") {
				t.Errorf("%s: a lateral fence lacks the literal ordered LIMIT 500:\n%s", name, body)
			}
		}
		if !strings.Contains(statement, "ORDER BY a.fact_id LIMIT 2000") {
			t.Errorf("%s: the arms are not cut to 4N = 2000 ids", name)
		}
		if strings.Contains(statement, "= '' OR") {
			t.Errorf("%s writes the cursor as `$n = '' OR ...`, a filter under a generic plan", name)
		}
	}
}

// TestSupplyChainImpactSupportsPageQueryCursorForms pins that only the
// next-page text carries the cursor, as a plain `s.fact_id > $n` inside each
// fence, and the first-page text carries none.
func TestSupplyChainImpactSupportsPageQueryCursorForms(t *testing.T) {
	t.Parallel()

	first := supplyChainImpactSupportsPageQuery(supportsPageArmsForTest, 0)
	next := supplyChainImpactSupportsPageQuery(supportsPageArmsForTest, 5)
	if strings.Contains(first, "fact_id > $") {
		t.Errorf("first page carries a cursor predicate:\n%s", first)
	}
	if got := strings.Count(next, "AND s.fact_id > $5"); got != len(supportsPageArmsForTest) {
		t.Errorf("next page carries %d cursor predicates, want one per arm (%d):\n%s", got, len(supportsPageArmsForTest), next)
	}
	for _, bodies := range [][]string{lateralBodies(next)} {
		for _, body := range bodies {
			if !strings.Contains(body, "AND s.fact_id > $5") {
				t.Errorf("a next-page fence lacks the cursor inside the LIMIT:\n%s", body)
			}
		}
	}
}

// TestSupplyChainImpactSupportsPageQueryOuterReadsRawRows pins the outer query:
// raw ids joined to the supports by primary key, both scope tables LEFT JOINed on
// their keys, every raw row returned with is_active, and the payload only when
// active.
func TestSupplyChainImpactSupportsPageQueryOuterReadsRawRows(t *testing.T) {
	t.Parallel()

	statement := supplyChainImpactSupportsPageQuery(supportsPageArmsForTest, 0)
	for _, want := range []string{
		"JOIN supply_chain_impact_finding_supports AS s ON s.fact_id = r.fact_id",
		"LEFT JOIN ingestion_scopes AS sc ON sc.scope_id = s.scope_id AND sc.active_generation_id = s.generation_id",
		"LEFT JOIN scope_generations AS g ON g.scope_id = s.scope_id AND g.generation_id = s.generation_id AND g.status = 'active'",
		"(sc.scope_id IS NOT NULL AND g.generation_id IS NOT NULL) AS is_active",
		"CASE WHEN sc.scope_id IS NOT NULL AND g.generation_id IS NOT NULL THEN s.payload END AS payload",
		"ORDER BY r.fact_id",
		"SELECT DISTINCT t.fact_id",
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("page statement missing %q:\n%s", want, statement)
		}
	}
	if strings.Contains(statement, "WHERE sc.") || strings.Contains(statement, "WHERE g.") {
		t.Errorf("the outer query filters on the scope tables; it must return every raw row:\n%s", statement)
	}
}

// TestSupplyChainImpactSupportsPageQueryRendersTheGuardsOfPartialIndexes pins
// that the package, cve and advisory arms repeat the literal of their partial
// btrees and the repository arm carries none.
func TestSupplyChainImpactSupportsPageQueryRendersTheGuardsOfPartialIndexes(t *testing.T) {
	t.Parallel()

	statement := supplyChainImpactSupportsPageQuery(supportsPageArmsForTest, 0)
	for _, want := range []string{
		"WHERE s.repository_id = k.v\n",
		"WHERE s.package_id = k.v AND s.package_id <> ''",
		"WHERE s.cve_id = k.v AND s.cve_id <> ''",
		"WHERE s.advisory_id = k.v AND s.advisory_id <> ''",
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("page statement missing %q:\n%s", want, statement)
		}
	}
}
