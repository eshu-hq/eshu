// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "testing"

// TestAssertCypherHasNoIgnoredLabelPredicateSeededViolations is the
// seeded-violation RED/GREEN pair for the #6786 X11 guard. Every RED case is a
// shape measured live to be ignored or mis-evaluated on NornicDB v1.3.3, and
// every GREEN case is a shape measured live to be evaluated correctly there
// and on Neo4j (docs/internal/evidence/6786-nornicdb-label-predicates.md).
func TestAssertCypherHasNoIgnoredLabelPredicateSeededViolations(t *testing.T) {
	t.Parallel()

	redCases := map[string]string{
		"label test after a one-hop pattern":  "MATCH (s)-[:DEPENDS_ON]->(t) WHERE s:Repository AND t:Repository RETURN s.id",
		"label test after a labelled pattern": "MATCH (s:Repository)-[:DEPENDS_ON]->(t) WHERE t:Workload RETURN t.id",
		"label OR after a two-hop pattern": "MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(infra)\n" +
			"\t\tWHERE infra:K8sResource OR infra:TerraformModule\n\t\t      OR infra:HelmChart\n\t\tRETURN infra.id",
		"parenthesised label OR":                 "MATCH (s)-[:DEPENDS_ON]->(t) WHERE (t:Repository OR t:Workload) AND s.id <> $x RETURN t.id",
		"negated label after a pattern":          "MATCH (s)-[:DEPENDS_ON]->(t) WHERE NOT t:Workload RETURN t.id",
		"label test in OPTIONAL MATCH":           "MATCH (s:Repository {id: $id}) OPTIONAL MATCH (s)-[:DEPENDS_ON]->(t) WHERE t:Repository RETURN t.id",
		"label test after incoming arrow":        "MATCH (t:Repository {id: $id})<-[:DEPENDS_ON]-(s) WHERE s:Workload RETURN s.id",
		"any over labels after a pattern":        "MATCH path = (a:Repository {id: $id})-[*1..3]->(i) WHERE i.id <> $id AND any(label IN labels(i) WHERE label IN ['Workload']) RETURN i.id",
		"any over labels on a single node":       "MATCH (n) WHERE any(l IN labels(n) WHERE l IN $labels) RETURN n.id",
		"none over labels":                       "MATCH (n) WHERE none(l IN labels(n) WHERE l = 'File') RETURN n.id",
		"any over a list testing labels":         "MATCH (s)-[:DEPENDS_ON]->(t) WHERE any(l IN $labels WHERE l IN labels(t)) RETURN t.id",
		"list comprehension over labels":         "MATCH (s)-[:DEPENDS_ON]->(t) WHERE size([l IN labels(t) WHERE l IN $labels]) > 0 RETURN t.id",
		"IN labels after WITH":                   "MATCH ()-[r:DEPENDS_ON]->() WITH startNode(r) AS s WHERE 'Repository' IN labels(s) RETURN s.id",
		"negated label after WITH":               "MATCH (s)-[:DEPENDS_ON]->(t) WITH s, t WHERE NOT t:Workload RETURN t.id",
		"label test inside CALL arm":             "CALL {\n  MATCH (s)-[:DEPENDS_ON]->(t) WHERE t:Repository RETURN t.id AS id\n}\nRETURN id",
		"line comment holding a clause keyword":  "MATCH (s)-[:DEPENDS_ON]->(t) // RETURN output\nWHERE t:Repository RETURN t.id",
		"line comment holding an apostrophe":     "MATCH (s)-[:DEPENDS_ON]->(t) // don't drop t:Repository rows\nWHERE t:Repository RETURN t.id",
		"block comment holding a clause keyword": "MATCH (s)-[:DEPENDS_ON]->(t) /* WITH t AS t */ WHERE t:Repository RETURN t.id",
	}
	for name, cypher := range redCases {
		t.Run("red/"+name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingT{}
			AssertCypherHasNoIgnoredLabelPredicate(rec, cypher)
			if !rec.failed {
				t.Fatalf("AssertCypherHasNoIgnoredLabelPredicate did not fail for an X11 shape: %q", cypher)
			}
		})
	}

	greenCases := map[string]string{
		"label in the pattern":                "MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository) RETURN s.id, t.id",
		"IN labels after a pattern":           "MATCH (s)-[r:DEPENDS_ON]->(t) WHERE 'Repository' IN labels(s) AND $type IN labels(t) RETURN s.id",
		"IN labels OR chain after var-length": "MATCH path = (a:Repository {id: $id})-[*1..3]->(i) WHERE i.id <> $id AND ('Repository' IN labels(i) OR 'Workload' IN labels(i)) RETURN i.id",
		"single-node label OR":                "MATCH (n)\nWHERE n:Function OR n:Class OR n:File\nRETURN true LIMIT 1",
		"single-node negated label":           "MATCH (n:ArgoCDApplicationSet)\nWHERE true AND NOT n:ArgoCDApplication RETURN n.id",
		"single-node label inside OR":         "MATCH (n:TerraformResource) WHERE (n.provider = $provider OR (n:CloudResource AND n.source_system = $provider)) RETURN n.id",
		"positive label test after WITH":      "MATCH path = (a:Repository {id: $id})-[*1..3]->(i)\n  WHERE i.id <> $id\n  WITH path, i\n  WHERE i:Workload OR i:CloudResource\n  RETURN i.id",
		"label in an EXISTS pattern":          "MATCH (s)-[:DEPENDS_ON]->(t) WHERE EXISTS { MATCH (t)<-[:DEFINES]-(:Repository {id: $id}) } RETURN t.id",
		"label in a WHERE pattern predicate":  "MATCH (s)-[:DEPENDS_ON]->(t) WHERE (t)<-[:DEFINES]-(r:Repository) RETURN t.id",
		"map literal key":                     "MATCH (s:Repository {id: $id})-[:DEPENDS_ON]->(t) WHERE t.kind = 'Deployment:Apps' RETURN t.id",
		"labels projected, not filtered":      "MATCH (s)-[:DEPENDS_ON]->(t) WHERE s.id = $id RETURN labels(t) AS labels, head(labels(t)) AS kind",
		"any over a property list":            "MATCH (s)-[:DEPENDS_ON]->(t) WHERE any(tag IN t.tags WHERE tag IN $tags) RETURN t.id",
		"label test inside a line comment":    "MATCH (s)-[:DEPENDS_ON]->(t) WHERE t.id = $id // was t:Repository\nRETURN t.id",
		"slashes inside a string literal":     "MATCH (n:Repository) WHERE n.url = 'https://example.invalid/x' RETURN n.id",
		"unterminated block comment":          "MATCH (s)-[:DEPENDS_ON]->(t) WHERE t.id = $id RETURN t.id /* was t:Repository WHERE",
	}
	for name, cypher := range greenCases {
		t.Run("green/"+name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingT{}
			AssertCypherHasNoIgnoredLabelPredicate(rec, cypher)
			if rec.failed {
				t.Fatalf("AssertCypherHasNoIgnoredLabelPredicate failed for a safe shape: %q (message: %s)", cypher, rec.message)
			}
		})
	}
}

// TestIgnoredLabelPredicateDocumentedBlindSpots pins the label tests the
// guard does not inspect, which IgnoredLabelPredicate's doc comment lists.
// The Sprintf and lowercase cases are the measured X11 shape in a form the
// guard cannot parse; the comprehension cases have not been measured live on
// v1.3.3. The guard returns "" for all of them. When it learns to catch one,
// this test fails: move the case into the RED set above and drop it from the
// doc comment, so a green scan never reads as wider coverage than it has.
func TestIgnoredLabelPredicateDocumentedBlindSpots(t *testing.T) {
	t.Parallel()

	blindSpots := map[string]string{
		"Sprintf label template":    "MATCH (s)-[:DEPENDS_ON]->(t) WHERE t:%s RETURN t.id",
		"lowercase keywords":        "match (s)-[:DEPENDS_ON]->(t) where t:Repository return t.id",
		"pattern comprehension":     "MATCH (s:Repository {id: $id}) RETURN [(s)-[:DEPENDS_ON]->(t) WHERE t:Workload | t.id] AS ids",
		"list comprehension filter": "MATCH p = (s:Repository {id: $id})-[:DEPENDS_ON*1..2]->(t) RETURN [n IN nodes(p) WHERE n:Workload | n.id] AS ids",
	}
	for name, cypher := range blindSpots {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if problem := IgnoredLabelPredicate(cypher); problem != "" {
				t.Fatalf("the guard now reports this documented blind spot (%s); move it to the RED cases and update IgnoredLabelPredicate's doc comment: %q", problem, cypher)
			}
		})
	}
}
