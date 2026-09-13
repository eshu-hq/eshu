// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// TestHasNodeMergeThenCreate is the unit-level proof of hasNodeMergeThenCreate,
// including the two mandatory false-positive exclusions ("ON CREATE SET" and
// a CREATE in a different statement) and keyword whitespace/case tolerance.
func TestHasNodeMergeThenCreate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "canonical repro: node MERGE, node MERGE, relationship CREATE",
			value: `MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name: "SET between the MERGEs and the CREATE does not change the result",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
SET s.updated_at = $now
CREATE (s)-[:DEPENDS_ON]->(t)`,
			want: true,
		},
		{
			name: "a second CREATE after the first still counts",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
CREATE (s)-[:DEPENDS_ON]->(t)
CREATE (t)-[:USED_BY]->(s)`,
			want: true,
		},
		{
			name:  "single MERGE followed directly by CREATE",
			value: `MERGE (n:Repository {id:$id}) CREATE (n)-[:HAS_TAG]->(:Tag {name:$tag})`,
			want:  true,
		},
		{
			name:  "ON CREATE SET is a normal MERGE action, not a CREATE clause",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = $now`,
			want:  false,
		},
		{
			name:  "ON MATCH SET is a normal MERGE action too",
			value: `MERGE (n:Repository {id:$id}) ON MATCH SET n.last_seen = $now`,
			want:  false,
		},
		{
			name: "ON CREATE SET beside a real CREATE clause still flags the real clause",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = $now
CREATE (n)-[:HAS_TAG]->(:Tag {name:$tag})`,
			want: true,
		},
		{
			name:  "named-path CREATE with spaces around the equals sign still matches",
			value: `MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE p = (s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name:  "named-path CREATE with no spaces around the equals sign still matches",
			value: `MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE p=(s)-[:R]->(t)`,
			want:  true,
		},
		{
			name:  "lowercase named-path CREATE still matches",
			value: `merge (s:Workload {id:$s}) merge (t:Workload {id:$t}) create path = (s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name:  "ON CREATE SET assigning a map is not a named-path CREATE",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n = $props`,
			want:  false,
		},
		{
			name:  "ON CREATE SET with a parenthesized arithmetic expression is not a named-path CREATE",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n.x = (1 + 2)`,
			want:  false,
		},
		{
			name:  "ON CREATE SET with += is not a named-path CREATE",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n += $p`,
			want:  false,
		},
		{
			name:  "ON MATCH SET with a parenthesized expression has no CREATE at all",
			value: `MERGE (n:Repository {id:$id}) ON MATCH SET n.y = (2)`,
			want:  false,
		},
		{
			name:  "a lone named-path MERGE followed by CREATE flags",
			value: `MERGE p = (s:Workload {id:$s})-[:R]->(t:Workload {id:$t}) CREATE (x:AuditEvent {id:$eventId})`,
			want:  true,
		},
		{
			name:  "named-path MERGE followed by named-path CREATE flags",
			value: `MERGE p=(s:Workload {id:$s})-[:R]->(t:Workload {id:$t}) MATCH (x:AuditEvent {id:$eventId}) CREATE p2 = (x)-[:LOGGED]->(t)`,
			want:  true,
		},
		{
			name:  "ON CREATE SET assigning a parenthesized expression after a plain MERGE stays clean",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET x = (1 + 2)`,
			want:  false,
		},
		{
			name:  "ON MATCH SET assigning a parenthesized expression after a plain MERGE stays clean",
			value: `MERGE (n:Repository {id:$id}) ON MATCH SET x = (1 + 2)`,
			want:  false,
		},
		{
			name:  "CREATE in a different statement (after a semicolon) does not count",
			value: `MERGE (n:Repository {id:$id}) SET n.last_seen = $now; CREATE (:AuditEvent {id:$eventId})`,
			want:  false,
		},
		{
			name:  "CREATE before the MERGE, same statement, does not count",
			value: `CREATE (:AuditEvent {id:$eventId}) MERGE (n:Repository {id:$id})`,
			want:  false,
		},
		{
			name: "relationship MERGE instead of CREATE is the safe shape",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
MERGE (s)-[:DEPENDS_ON]->(t)`,
			want: false,
		},
		{
			name:  "MATCH ... MATCH ... CREATE (no MERGE at all) is the safe shape",
			value: `MATCH (s:Workload {id:$s}) MATCH (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)`,
			want:  false,
		},
		{
			name:  "comma-pattern CREATE with no MERGE is the safe shape",
			value: `MATCH (s:Workload {id:$s}) MATCH (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t), (t)-[:USED_BY]->(s)`,
			want:  false,
		},
		{
			name:  "no MERGE at all",
			value: `MATCH (n:Repository {id:$id}) RETURN n`,
			want:  false,
		},
		{
			name:  "MERGE with no CREATE anywhere",
			value: `MERGE (n:Repository {id:$id}) RETURN n`,
			want:  false,
		},
		{
			name:  "keyword-like substrings never match (UNMERGED / RECREATE)",
			value: `// UNMERGED (n) RECREATE (m)`,
			want:  false,
		},
		{
			name:  "lowercase keywords with no space before the paren still match",
			value: `merge(s:Workload {id:$s}) merge(t:Workload {id:$t}) create(s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name:  "mixed-case keywords with extra internal spaces still match",
			value: `Merge  (s:Workload {id:$s}) MeRgE  (t:Workload {id:$t}) CrEaTe  (s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name: "a newline between the keyword and its opening paren still matches",
			value: `MERGE
(s:Workload {id:$s}) MERGE
(t:Workload {id:$t}) CREATE
(s)-[:DEPENDS_ON]->(t)`,
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasNodeMergeThenCreate(tc.value); got != tc.want {
				t.Fatalf("hasNodeMergeThenCreate(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestScanFileHitsFoldsStringConcatenation proves scanFileHits closes the
// concatenation gap a plain per-literal scan has: a MERGE-then-CREATE
// statement built as `mergePart + "CREATE (...)"` (a package-level identifier
// resolved by buildFileConstIdentMap) or as two adjacent literals joined by
// "+" is caught even though neither half alone contains the banned shape. It
// also proves several things must NOT be flagged: a split literal whose
// folded text is the safe "ON CREATE SET" shape, a statement assembled by
// fmt.Sprintf, a function-local const of the identical shape as the caught
// package-level one, and an unresolvable leaf (a package selector) that DOES
// carry the full MERGE/CREATE text but sits to the left of it in the "+"
// chain -- see foldStringExpr's doc comment for why left-position matters.
// Asserts the exact set of hit lines, not just a count, so a false hit
// silently replacing a missed real one cannot pass unnoticed.
func TestScanFileHitsFoldsStringConcatenation(t *testing.T) {
	t.Parallel()

	const src = `package fixture

import "fmt"

const mergePart = "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) "

var viaSameFileConstIdent = mergePart + "CREATE (s)-[:DEPENDS_ON]->(t)"

var viaTwoLiterals = "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) " + "CREATE (s)-[:DEPENDS_ON]->(t)"

var viaSplitOnCreateSetIsSafe = "MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = " + "$now"

var viaSprintfIsInvisible = fmt.Sprintf("MERGE (s:Workload {id:%s}) MERGE (t:Workload {id:%s}) %s (s)-[:DEPENDS_ON]->(t)", "a", "b", "CREATE")

func viaFunctionLocalConstIsInvisible() string {
	const localMergePart = "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) "
	return localMergePart + "CREATE (s)-[:DEPENDS_ON]->(t)"
}

var viaUnresolvedLeafBeforeTheLiteralsIsInvisible = unknownPkg.Fragment + "MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) " + "CREATE (s)-[:DEPENDS_ON]->(t)"
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture source: %v", err)
	}

	hits := scanFileHits(fset, "fixture.go", file)
	want := []string{"fixture.go:7", "fixture.go:9"} // viaSameFileConstIdent, viaTwoLiterals
	if !slices.Equal(hits, want) {
		t.Fatalf(
			"scanFileHits found %v, want exactly %v: viaSameFileConstIdent (line 7) and "+
				"viaTwoLiterals (line 9) must be caught by folding; every other candidate must "+
				"NOT be -- viaSplitOnCreateSetIsSafe (folded text is a normal MERGE action, not "+
				"a CREATE clause), viaSprintfIsInvisible (runtime template assembly), "+
				"viaFunctionLocalConstIsInvisible (only package-level consts resolve), and "+
				"viaUnresolvedLeafBeforeTheLiteralsIsInvisible (an unresolvable leaf before the "+
				"literal pair prevents the pair from ever forming its own foldable subtree)",
			hits, want,
		)
	}
}
