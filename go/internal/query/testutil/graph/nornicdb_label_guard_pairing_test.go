// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

// TestAssertCypherHasNoIgnoredLabelPredicatePairedConjunct is the seeded
// RED/GREEN pair for the #7246 exemption. A positive label test conjunct in
// the WHERE of a relationship MATCH is allowed only beside an
// `'L' IN labels(x)` disjunction over the same labels, because NornicDB v1.3.3
// ignores the former and evaluates the latter. Every RED case is a way the
// label test could stop being a harmless extra conjunct, or the two filters
// could disagree between backends.
func TestAssertCypherHasNoIgnoredLabelPredicatePairedConjunct(t *testing.T) {
	t.Parallel()

	const head = "MATCH path = (a:Repository {id: $id})-[*1..4]->(i)\nWHERE i.id <> $id\n"
	const tail = "\nRETURN i.id"

	greenCases := map[string]string{
		"paired label test and IN labels guard": head +
			"  AND (i:Repository OR i:Workload)\n  AND ('Repository' IN labels(i) OR 'Workload' IN labels(i))" + tail,
		"guard first": head +
			"  AND ('Repository' IN labels(i) OR 'Workload' IN labels(i))\n  AND (i:Workload OR i:Repository)" + tail,
		"wrapped lines and an extra property conjunct": head +
			"  AND (i:Repository OR i:Workload\n    OR i:DataAsset)\n  AND ('Repository' IN labels(i) OR 'Workload' IN labels(i)\n    OR 'DataAsset' IN labels(i))\n" +
			"  AND (i.environment = $environment OR coalesce(i.environment, '') = '')" + tail,
		"trailing format verb for an appended clause": head +
			"  AND (i:Repository OR i:Workload)\n  AND ('Repository' IN labels(i) OR 'Workload' IN labels(i))%s" + tail,
		"single label":               head + "  AND i:Workload AND 'Workload' IN labels(i)" + tail,
		"two variables, each paired": "MATCH (s)-[:DEPENDS_ON]->(t)\nWHERE (s:Repository) AND (t:Workload OR t:DataAsset) AND ('Repository' IN labels(s)) AND ('Workload' IN labels(t) OR 'DataAsset' IN labels(t))\nRETURN t.id",
	}
	for name, cypher := range greenCases {
		t.Run("green/"+name, func(t *testing.T) {
			t.Parallel()
			if problem := IgnoredLabelPredicate(cypher); problem != "" {
				t.Fatalf("a paired label-test conjunct was rejected: %s\n%s", problem, cypher)
			}
		})
	}

	redCases := map[string]string{
		"label test with no guard": head + "  AND (i:Repository OR i:Workload)" + tail,
		"guard narrower than the label test": head +
			"  AND (i:Repository OR i:Workload)\n  AND ('Repository' IN labels(i))" + tail,
		"guard wider than the label test": head +
			"  AND (i:Repository)\n  AND ('Repository' IN labels(i) OR 'Workload' IN labels(i))" + tail,
		"guard over a different variable": "MATCH (s)-[:DEPENDS_ON]->(t)\nWHERE (s:Repository) AND ('Repository' IN labels(t))\nRETURN t.id",
		"top-level OR widens the filter": head +
			"  AND (i:Repository) AND ('Repository' IN labels(i)) OR i.kind = 'x'" + tail,
		"label test OR-ed with the guard": head +
			"  AND ((i:Repository) OR ('Repository' IN labels(i)))" + tail,
		"negated label test beside a guard": head +
			"  AND NOT i:Workload AND ('Workload' IN labels(i))" + tail,
		"label test nested in another conjunct": head +
			"  AND (i:Repository AND i.name <> 'x') AND ('Repository' IN labels(i))" + tail,
		"duplicate label-test conjuncts": head +
			"  AND (i:Repository) AND (i:Repository) AND ('Repository' IN labels(i))" + tail,
		"parameter guard rather than a literal label": head +
			"  AND (i:Repository) AND ($label IN labels(i))" + tail,
		"extra label test on an unguarded variable": "MATCH (s)-[:DEPENDS_ON]->(t)\nWHERE (t:Workload) AND (s:Repository) AND ('Workload' IN labels(t))\nRETURN t.id",
	}
	for name, cypher := range redCases {
		t.Run("red/"+name, func(t *testing.T) {
			t.Parallel()
			if problem := IgnoredLabelPredicate(cypher); problem == "" {
				t.Fatalf("an unpaired label-test conjunct was accepted\n%s", cypher)
			}
		})
	}
}
