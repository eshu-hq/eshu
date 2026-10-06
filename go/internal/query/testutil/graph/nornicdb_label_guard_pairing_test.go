// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

// pairedHead and pairedTail frame the WHERE under test the way the
// change-surface outgoing read does: one variable-length relationship MATCH
// that opens its frame.
const (
	pairedHead = "MATCH path = (a:Repository {id: $id})-[*1..4]->(impacted)\nWHERE impacted.id <> $id\n"
	pairedTail = "\nRETURN impacted.id"

	pairedLabelTest = "  AND (impacted:Repository OR impacted:Workload OR impacted:WorkloadInstance\n" +
		"    OR impacted:CloudResource OR impacted:TerraformModule OR impacted:DataAsset)"
	pairedInLabels = "\n  AND ('Repository' IN labels(impacted) OR 'Workload' IN labels(impacted) OR 'WorkloadInstance' IN labels(impacted)\n" +
		"    OR 'CloudResource' IN labels(impacted) OR 'TerraformModule' IN labels(impacted) OR 'DataAsset' IN labels(impacted))"
)

// TestAssertCypherHasNoIgnoredLabelPredicatePairedConjunct is the seeded
// RED/GREEN pair for the #7246 exemption. A positive label test conjunct in
// the WHERE of a relationship MATCH is allowed only in the exact shape proven
// live on NornicDB and Neo4j (TestLiveChangeSurfaceLabelPredicate and
// TestLiveChangeSurfaceLabelConjunctDeepTraversal): a plain MATCH that opens
// its frame, one variable-length relationship, one tested variable, the label
// test AND-ed beside an `'L' IN labels(x)` disjunction over the same labels.
// Every RED case is a way the label test could stop being a harmless extra
// conjunct, or a position 6786-nornicdb-label-predicates.md measures as
// evaluated and row-dropping on NornicDB.
func TestAssertCypherHasNoIgnoredLabelPredicatePairedConjunct(t *testing.T) {
	t.Parallel()

	// Both GREEN cases are the production statement text, run live on the
	// pinned NornicDB image and on Neo4j: the unscoped read, and the read with
	// the trailing environment clause appended through the format verb.
	greenCases := map[string]string{
		"production shape": pairedHead + pairedLabelTest + pairedInLabels + pairedTail,
		"production shape with the appended environment clause": pairedHead + pairedLabelTest + pairedInLabels +
			"%s" + pairedTail,
	}
	for name, cypher := range greenCases {
		t.Run("green/"+name, func(t *testing.T) {
			t.Parallel()
			if problem := IgnoredLabelPredicate(cypher); problem != "" {
				t.Fatalf("a paired label-test conjunct was rejected: %s\n%s", problem, cypher)
			}
		})
	}

	const tests = "  AND (impacted:Repository OR impacted:Workload)"
	const guards = "\n  AND ('Repository' IN labels(impacted) OR 'Workload' IN labels(impacted))"
	redCases := map[string]string{
		// 6786 row H02: the WHERE of a second MATCH evaluates the label test and
		// returns 0 rows on NornicDB.
		"second MATCH, 6786 row H02": "MATCH (a:Repository {id: $id})\nMATCH (a)-[*1..4]->(impacted)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		// 6786 row H02, a WITH between the two clauses.
		"MATCH after a WITH": "MATCH (a:Repository {id: $id})\nWITH a\nMATCH (a)-[*1..4]->(impacted)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		// 6786 row H05: an OPTIONAL MATCH evaluates the label test and nulls the
		// row (r1 to null).
		"OPTIONAL MATCH, 6786 row H05": "OPTIONAL MATCH (a:Repository {id: $id})-[*1..4]->(impacted)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		// Outside the live-proven shape: fixed-length relationship, and two
		// relationships. The pairing was never measured there.
		"fixed-length relationship": "MATCH (a:Repository {id: $id})-[:DEPENDS_ON]->(impacted)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		"two relationships": "MATCH (a:Repository {id: $id})-[*1..4]->(impacted)-[:OWNS]->(c)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		// Shapes the bracket count alone missed: a bracketless second relationship,
		// a leading relationship, a comma pattern, and MATCH frames inside a
		// correlated subquery over an already bound variable (the H02 situation).
		"bracketless second relationship": "MATCH (a:Repository {id: $id})-[*1..4]->(impacted)<--(c)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		"leading relationship": "MATCH (c)-->(a:Repository {id: $id})-[*1..4]->(impacted)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		"comma pattern": "MATCH (b:Repository), (a:Repository {id: $id})-[*1..4]->(impacted)\nWHERE impacted.id <> $id\n" +
			tests + guards + pairedTail,
		"CALL subquery over a bound variable": "MATCH (a:Repository {id: $id})\nCALL (a) {\n  MATCH (a)-[*1..4]->(impacted)\n  WHERE impacted.id <> $id\n" +
			tests + guards + "\n  RETURN impacted\n}\nRETURN impacted.id",
		"EXISTS subquery over a bound variable": "MATCH (a:Repository {id: $id})\nWHERE EXISTS {\n  MATCH (a)-[*1..4]->(impacted)\n  WHERE impacted.id <> $id\n" +
			tests + guards + "\n}\nRETURN a.id",
		"two tested variables, each paired":  "MATCH (a:Repository {id: $id})-[*1..4]->(impacted)\nWHERE (a:Repository) AND (impacted:Workload) AND ('Repository' IN labels(a)) AND ('Workload' IN labels(impacted))\nRETURN impacted.id",
		"label test with no guard":           pairedHead + tests + pairedTail,
		"guard narrower than the label test": pairedHead + tests + "\n  AND ('Repository' IN labels(impacted))" + pairedTail,
		"guard wider than the label test": pairedHead + "  AND (impacted:Repository)" +
			guards + pairedTail,
		"guard over a different variable": "MATCH (a:Repository {id: $id})-[*1..4]->(impacted)\nWHERE (a:Repository) AND ('Repository' IN labels(impacted))\nRETURN impacted.id",
		"top-level OR widens the filter": pairedHead + "  AND (impacted:Repository) AND ('Repository' IN labels(impacted)) OR impacted.kind = 'x'" +
			pairedTail,
		"label test OR-ed with the guard": pairedHead + "  AND ((impacted:Repository) OR ('Repository' IN labels(impacted)))" +
			pairedTail,
		"negated label test beside a guard": pairedHead + "  AND NOT impacted:Workload AND ('Workload' IN labels(impacted))" +
			pairedTail,
		"label test nested in another conjunct": pairedHead + "  AND (impacted:Repository AND impacted.name <> 'x') AND ('Repository' IN labels(impacted))" +
			pairedTail,
		"duplicate label-test conjuncts": pairedHead + "  AND (impacted:Repository) AND (impacted:Repository) AND ('Repository' IN labels(impacted))" +
			pairedTail,
		"parameter guard rather than a literal label": pairedHead + "  AND (impacted:Repository) AND ($label IN labels(impacted))" +
			pairedTail,
	}
	for name, cypher := range redCases {
		t.Run("red/"+name, func(t *testing.T) {
			t.Parallel()
			if problem := IgnoredLabelPredicate(cypher); problem == "" {
				t.Fatalf("an unpaired or out-of-position label-test conjunct was accepted\n%s", cypher)
			}
		})
	}
}
