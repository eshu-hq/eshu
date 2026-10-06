// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestChangeSurfaceScopedDropsLabelsTheServerFailedToFilter pins the Go-side
// enforcement of the impacted-label whitelist on the scoped traversal.
//
// The scoped Cypher expresses that whitelist as a WHERE attached to a WITH
// (change_surface_traversal.go), which the pinned NornicDB build does not
// evaluate as a filter: label tests in that clause position are silently
// dropped, so the backend returns every reachable node. The rows below are what
// that backend actually hands back — File and Function are reachable one hop
// from a Repository and are not in the whitelist.
func TestChangeSurfaceScopedDropsLabelsTheServerFailedToFilter(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:owner"}, nil)
	handler := &Handler{Neo4j: graph.FakeGraphReader{RunFn: func(
		_ context.Context,
		cypher string,
		_ map[string]any,
	) ([]map[string]any, error) {
		if !strings.Contains(cypher, "WITH path, impacted") {
			t.Fatalf("expected the scoped traversal, got:\n%s", cypher)
		}
		return []map[string]any{
			changeSurfaceTestRow("workload:checkout", "checkout",
				[]any{"Workload"}, "repository:owner", "DEFINES"),
			changeSurfaceTestRow("file:main.go", "main.go",
				[]any{"File"}, "repository:owner", "DEFINES"),
			changeSurfaceTestRow("function:handler", "handler",
				[]any{"Function"}, "repository:owner", "DEFINES"),
			changeSurfaceTestRow("cloudresource:bucket", "bucket",
				[]any{"CloudResource"}, "repository:owner", "DEFINES"),
		}, nil
	}}}

	rows, _, err := handler.changeSurfaceTraversalRows(
		context.Background(),
		ChangeSurfaceTargetCandidate{ID: "workload:changed", Labels: []string{"Workload"}},
		"",
		4,
		10,
		access,
	)
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}

	want := []string{"cloudresource:bucket", "workload:checkout"}
	if got := testutil.RowIDs(rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("row ids = %#v, want %#v -- a node outside the impacted-label "+
			"whitelist reached the caller", got, want)
	}
}

// TestChangeSurfaceKeepsEveryWhitelistedLabel guards the opposite failure: a
// filter tight enough to drop legitimate impacted kinds. Every label the legacy
// server-side whitelist admits must survive the Go filter, including Repository,
// which the scoped Cypher admits through its second CALL arm rather than through
// the WITH clause.
func TestChangeSurfaceKeepsEveryWhitelistedLabel(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:owner"}, nil)
	rows := []map[string]any{
		changeSurfaceTestRow("workload:a", "a", []any{"Workload"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("instance:b", "b", []any{"WorkloadInstance"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("cloud:c", "c", []any{"CloudResource"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("tf:d", "d", []any{"TerraformModule"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("data:e", "e", []any{"DataAsset"}, "repository:owner", "DEFINES"),
		changeSurfaceTestRow("repository:owner", "f", []any{"Repository"}, "", "DEFINES"),
	}

	filtered := changeSurfaceFilterTraversalRows(rows, "", access, false)
	if got, want := len(filtered), len(rows); got != want {
		t.Fatalf("kept %d of %d whitelisted rows: %#v",
			got, want, testutil.RowIDs(filtered))
	}
}

// TestChangeSurfaceImpactedLabelsMatchTheLegacyCypher keeps the Go whitelist and
// the legacy server-side whitelist from drifting apart, and pins the legacy
// whitelist to the `'Label' IN labels(impacted)` shape NornicDB v1.3.3
// evaluates in the WHERE of a relationship MATCH (#6786 X11), beside the
// ignored-there `impacted:Label` fast-path conjunct (#7246). The earlier
// any(label IN labels(impacted) ...) form was ignored there, so LIMIT ran over
// every reachable node.
func TestChangeSurfaceImpactedLabelsMatchTheLegacyCypher(t *testing.T) {
	t.Parallel()

	rendered := fmt.Sprintf(changeSurfaceLegacyCypher, "(start:Repository {id: $target_id})", 4, changeSurfaceEnvironmentClause("prod"))
	graph.AssertCypherHasNoIgnoredLabelPredicate(t, rendered)
	graph.AssertCypherHasNoBrokenAndOr(t, rendered)

	// Every label read in the WHERE must be one whitelist term. A stray
	// `impacted:File` or a second any() would be a filter this guard cannot
	// compare, so count every labels(impacted) read and require all but the
	// RETURN projection to be `'Label' IN labels(impacted)` terms.
	terms := regexp.MustCompile(`'(\w+)' IN labels\(impacted\)`).FindAllStringSubmatch(changeSurfaceLegacyCypher, -1)
	if n := strings.Count(changeSurfaceLegacyCypher, "labels(impacted)"); n != len(terms)+1 {
		t.Fatalf("legacy cypher reads labels(impacted) %d times, want the %d whitelist terms plus the RETURN projection", n, len(terms))
	}
	// The `impacted:Label` conjunct is the Neo4j fast path (#7246) and is
	// ignored by NornicDB v1.3.3 in this clause position, so it may only name
	// whitelisted labels; TestChangeSurfaceLegacyCypherGuardsWhitelistWithLabelTest
	// pins its position and that it never stands alone.
	for _, m := range regexp.MustCompile(`impacted:(\w+)`).FindAllStringSubmatch(changeSurfaceLegacyCypher, -1) {
		if _, ok := changeSurfaceImpactedLabels[m[1]]; !ok {
			t.Errorf("legacy cypher label test names %q, which is outside the whitelist", m[1])
		}
	}
	cypherLabels := map[string]struct{}{}
	for _, term := range terms {
		cypherLabels[term[1]] = struct{}{}
	}
	for label := range changeSurfaceImpactedLabels {
		if _, ok := cypherLabels[label]; !ok {
			t.Errorf("label %q is admitted in Go but absent from the legacy Cypher whitelist", label)
		}
	}
	for label := range cypherLabels {
		if _, ok := changeSurfaceImpactedLabels[label]; !ok {
			t.Errorf("label %q is admitted by the legacy Cypher but dropped by the Go filter", label)
		}
	}
}

// TestChangeSurfaceScopedCypherWhitelistMatchesGoMap guards the third copy of
// the impacted-label whitelist: the WITH-attached WHERE in
// changeSurfaceScopedOutgoingCypher. That clause is inert today -- the pinned
// NornicDB build does not evaluate a label test attached to a WITH, so
// changeSurfaceImpactedLabels is where the whitelist actually runs -- but it
// stops being inert the moment upstream fixes clause evaluation. If it has
// drifted from the Go map by then (a label added to one and not the other),
// the server filter and the Go filter disagree: a label the Go map admits but
// the scoped Cypher does not would be dropped server-side before LIMIT,
// silently under-reporting impacted nodes. This guard keeps that from
// happening unnoticed.
//
// The scoped Cypher's list omits Repository, which the second CALL arm admits
// through its own id-based branch rather than through this label test, so the
// comparison is against changeSurfaceImpactedLabels minus Repository.
func TestChangeSurfaceScopedCypherWhitelistMatchesGoMap(t *testing.T) {
	t.Parallel()

	marker := "WITH path, impacted\n  WHERE impacted:"
	// Exactly one. Nothing asserted uniqueness before, so appending a SECOND
	// WITH/WHERE block later in the constant passed: this guard parsed the first
	// and never saw the second, which could admit any label it liked.
	if n := strings.Count(changeSurfaceScopedOutgoingCypher, marker); n != 1 {
		t.Fatalf("scoped cypher carries %d WITH-attached label whitelists, want exactly 1; "+
			"this guard parses one and would not see the others", n)
	}
	start := strings.Index(changeSurfaceScopedOutgoingCypher, marker)
	if start < 0 {
		t.Fatal("scoped cypher no longer carries a WITH-attached label whitelist; update this guard")
	}

	// The whitelist must live in arm 1. An earlier revision of this guard located
	// the marker anywhere in the constant and never checked which arm held it, so
	// moving the whole WITH/WHERE block into the Repository arm passed: arm 1 lost
	// its whitelist entirely and arm 2 gained a label test no Repository node can
	// satisfy, which would return nothing the moment upstream fixes clause
	// evaluation.
	// Fail, do not skip, when the anchor is gone. An earlier revision guarded this
	// with `armTwo >= 0 &&`, so a single whitespace edit -- `(impacted :Repository)`
	// -- set the index to -1 and silently disarmed the arm check for every edit
	// after it, with nothing red.
	armTwo := strings.Index(changeSurfaceScopedOutgoingCypher, "(impacted:Repository)")
	if armTwo < 0 {
		t.Fatal("arm 2 no longer matches (impacted:Repository); update this guard rather than leaving it inert")
	}
	if start > armTwo {
		t.Fatal("the WITH-attached label whitelist has moved into the Repository arm; it belongs in arm 1")
	}

	start += len(marker)
	// Read to the end of the WHERE clause, not to the end of its first line. An
	// earlier revision stopped at the first "\n", so a label added on a wrapped
	// continuation line was invisible to this guard -- the exact drift it exists
	// to catch. The clause ends where the next Cypher keyword begins.
	rest := changeSurfaceScopedOutgoingCypher[start:]
	end := len(rest)
	for _, keyword := range []string{"\n  RETURN", "\n  WITH", "\n  MATCH", "\n  CALL", "\nRETURN", "\nWITH", "\nMATCH", "\nCALL", "\n}"} {
		if i := strings.Index(rest, keyword); i >= 0 && i < end {
			end = i
		}
	}
	clause := rest[:end]

	got := map[string]struct{}{}
	for _, label := range strings.Split(clause, " OR impacted:") {
		got[strings.TrimSpace(label)] = struct{}{}
	}

	want := map[string]struct{}{}
	for label := range changeSurfaceImpactedLabels {
		if label == "Repository" {
			continue
		}
		want[label] = struct{}{}
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped cypher WITH-attached whitelist = %v, want %v (changeSurfaceImpactedLabels minus "+
			"Repository) -- keep the inert Cypher list in sync with the Go filter it stands in for", got, want)
	}
}

// TestChangeSurfaceRowLabelAdmittedFailsClosed pins the behaviour
// changeSurfaceRowLabelAdmitted's doc comment states as a design claim: a row
// whose labels cannot be read is refused rather than admitted.
//
// Nothing pinned this before, and a mutation making it fail OPEN on an empty
// label set passed the entire package. That direction is the dangerous one: the
// filter exists precisely because the scoped traversal's server-side label test
// is inert on the pinned backend, so admitting an unlabelled row would return
// exactly the rows the filter was added to exclude.
func TestChangeSurfaceRowLabelAdmittedFailsClosed(t *testing.T) {
	t.Parallel()

	for name, row := range map[string]map[string]any{
		"labels key absent":  {"id": "x"},
		"labels empty slice": {"labels": []string{}},
		"labels empty any":   {"labels": []any{}},
		"labels wrong type":  {"labels": "Workload"},
		"labels nil":         {"labels": nil},
	} {
		if changeSurfaceRowLabelAdmitted(row) {
			t.Errorf("%s: row was admitted; an unreadable label set must fail closed", name)
		}
	}

	if !changeSurfaceRowLabelAdmitted(map[string]any{"labels": []string{"Workload"}}) {
		t.Error("a row carrying a whitelisted label must still be admitted")
	}
}

// changeSurfaceWhereConjuncts returns the top-level AND conjuncts of the WHERE
// clause of the outgoing traversal MATCH, split only at parenthesis depth zero
// so a parenthesised OR group stays one conjunct.
func changeSurfaceWhereConjuncts(t *testing.T, cypher string) []string {
	t.Helper()
	start := strings.Index(cypher, "\nWHERE ")
	end := strings.Index(cypher, "\nRETURN ")
	if start < 0 || end < start {
		t.Fatalf("cypher has no WHERE ... RETURN window:\n%s", cypher)
	}
	body := cypher[start+len("\nWHERE ") : end]
	var conjuncts []string
	depth, from := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 {
			continue
		}
		for _, sep := range []string{" AND ", "\n  AND "} {
			if strings.HasPrefix(body[i:], sep) {
				conjuncts = append(conjuncts, strings.TrimSpace(body[from:i]))
				from = i + len(sep)
				i = from - 1
				break
			}
		}
	}
	return append(conjuncts, strings.TrimSpace(body[from:]))
}

// TestChangeSurfaceLegacyCypherGuardsWhitelistWithLabelTest is the #7246 shape
// proof. On Neo4j, six `'X' IN labels(impacted)` terms rebuild the label list
// for every one of the 263,186 four-hop paths of a 12,403-file repository and
// that Filter was 1.84M of 2.25M DB hits for a 0-row answer. A label test
// (`impacted:Label`) is a cheap node-label check, and placed in front of the
// IN labels() terms it removes every non-whitelisted path before they run.
// The IN labels() terms stay as the NornicDB v1.3.3 guard: that backend ignores
// a label test in this clause position (#6786 X11), so the whitelist it
// enforces is still the IN labels() form.
func TestChangeSurfaceLegacyCypherGuardsWhitelistWithLabelTest(t *testing.T) {
	t.Parallel()

	rendered := fmt.Sprintf(changeSurfaceLegacyCypher, "(start:Repository {id: $target_id})", 4, "")
	conjuncts := changeSurfaceWhereConjuncts(t, rendered)
	if len(conjuncts) != 3 {
		t.Fatalf("unscoped outgoing WHERE has %d conjuncts, want 3 (id guard, label test, IN labels() guard): %q", len(conjuncts), conjuncts)
	}
	if conjuncts[0] != "impacted.id <> $target_id" {
		t.Errorf("first conjunct = %q, want the target id guard", conjuncts[0])
	}

	labelTest := regexp.MustCompile(`^\(impacted:\w+(?:\s+OR\s+impacted:\w+)*\)$`)
	if !labelTest.MatchString(conjuncts[1]) {
		t.Fatalf("second conjunct = %q, want a parenthesised `impacted:Label OR ...` label test "+
			"ahead of the IN labels() disjunction", conjuncts[1])
	}
	tested := map[string]struct{}{}
	for _, m := range regexp.MustCompile(`impacted:(\w+)`).FindAllStringSubmatch(conjuncts[1], -1) {
		tested[m[1]] = struct{}{}
	}
	if !reflect.DeepEqual(tested, changeSurfaceImpactedLabels) {
		t.Errorf("label-test conjunct admits %v, want exactly changeSurfaceImpactedLabels %v", tested, changeSurfaceImpactedLabels)
	}

	guard := map[string]struct{}{}
	for _, m := range regexp.MustCompile(`'(\w+)' IN labels\(impacted\)`).FindAllStringSubmatch(conjuncts[2], -1) {
		guard[m[1]] = struct{}{}
	}
	if strings.Contains(conjuncts[2], "impacted:") || !reflect.DeepEqual(guard, changeSurfaceImpactedLabels) {
		t.Errorf("third conjunct = %q, want the IN labels() NornicDB guard over exactly the whitelist", conjuncts[2])
	}
}

// TestChangeSurfaceNonLegacyCypherIsPinned holds the statements this change
// must not touch: the scoped outgoing traversal and the repository-consumers
// read. A deliberate edit to either re-pins the digest in the same commit.
func TestChangeSurfaceNonLegacyCypherIsPinned(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ cypher, sha string }{
		"scoped outgoing":      {changeSurfaceScopedOutgoingCypher, "fc148711bafaa2eb4cf1f25db2a6cbc3ee1215e353f041841011b03479fa7441"},
		"repository consumers": {changeSurfaceRepositoryConsumersCypher, "d42bfe62558f2106f2ea952311303581ba45326c2c35d757538b66e9d7d776d7"},
	} {
		sum := sha256.Sum256([]byte(tc.cypher))
		if got := hex.EncodeToString(sum[:]); got != tc.sha {
			t.Errorf("%s cypher digest = %s, want %s", name, got, tc.sha)
		}
	}
}

// TestChangeSurfaceEnvironmentScopedCypherKeepsLabelTestFirst pins the same
// conjunct order on the statement an environment-scoped request actually runs
// (#7246). The no-environment test above sees three conjuncts; with the
// environment clause appended there are four, and a later reorder or a change
// to that clause must not move the label test behind the IN labels() terms, or
// the paths they are meant to prune would pay for them again.
func TestChangeSurfaceEnvironmentScopedCypherKeepsLabelTestFirst(t *testing.T) {
	t.Parallel()

	rendered := fmt.Sprintf(changeSurfaceLegacyCypher, "(start:Repository {id: $target_id})", 4, changeSurfaceEnvironmentClause("prod"))
	conjuncts := changeSurfaceWhereConjuncts(t, rendered)
	if len(conjuncts) != 4 {
		t.Fatalf("environment-scoped WHERE has %d conjuncts, want 4 (id guard, label test, IN labels() guard, environment): %q", len(conjuncts), conjuncts)
	}
	if conjuncts[0] != "impacted.id <> $target_id" {
		t.Errorf("first conjunct = %q, want the target id guard", conjuncts[0])
	}
	if !regexp.MustCompile(`^\(impacted:\w+(?:\s+OR\s+impacted:\w+)*\)$`).MatchString(conjuncts[1]) {
		t.Errorf("second conjunct = %q, want the `impacted:Label OR ...` label test ahead of the IN labels() guard", conjuncts[1])
	}
	if !strings.Contains(conjuncts[2], "IN labels(impacted)") || strings.Contains(conjuncts[2], "impacted:") {
		t.Errorf("third conjunct = %q, want the IN labels() guard", conjuncts[2])
	}
	if !strings.Contains(conjuncts[3], "impacted.environment = $environment") {
		t.Errorf("fourth conjunct = %q, want the environment predicate last", conjuncts[3])
	}
}
