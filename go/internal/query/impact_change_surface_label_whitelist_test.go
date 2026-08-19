// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestChangeSurfaceScopedDropsLabelsTheServerFailedToFilter pins the Go-side
// enforcement of the impacted-label whitelist on the scoped traversal.
//
// The scoped Cypher expresses that whitelist as a WHERE attached to a WITH
// (impact_change_surface_traversal.go), which the pinned NornicDB build does not
// evaluate as a filter: label tests in that clause position are silently
// dropped, so the backend returns every reachable node. The rows below are what
// that backend actually hands back — File and Function are reachable one hop
// from a Repository and are not in the whitelist.
func TestChangeSurfaceScopedDropsLabelsTheServerFailedToFilter(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:owner"}, nil)
	handler := &ImpactHandler{Neo4j: fakeGraphReader{run: func(
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
		changeSurfaceTargetCandidate{ID: "workload:changed", Labels: []string{"Workload"}},
		"",
		4,
		10,
		access,
	)
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}

	want := []string{"cloudresource:bucket", "workload:checkout"}
	if got := changeSurfaceTestIDs(rows); !reflect.DeepEqual(got, want) {
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
			got, want, changeSurfaceTestIDs(filtered))
	}
}

// TestChangeSurfaceImpactedLabelsMatchTheLegacyCypher keeps the Go whitelist and
// the legacy server-side whitelist from drifting apart. The legacy query filters
// in a MATCH-attached WHERE, the clause position the pinned backend evaluates
// correctly, so its list is the contract both paths owe callers.
func TestChangeSurfaceImpactedLabelsMatchTheLegacyCypher(t *testing.T) {
	t.Parallel()

	// Pin the quantifier as well as the list. An earlier revision parsed only the
	// bracketed labels, so rewriting any() to all() passed -- and all() drops every
	// node carrying a label outside the list, which would gut the unscoped path
	// while this guard stayed green.
	if !strings.Contains(changeSurfaceLegacyCypher, "any(label IN labels(impacted) WHERE label IN [") {
		t.Error("legacy cypher no longer uses any(label IN labels(impacted) WHERE label IN [...]); " +
			"all() would drop every node with a label outside the whitelist")
	}

	clause := changeSurfaceLegacyCypher
	start := strings.Index(clause, "label IN [")
	if start < 0 {
		t.Fatal("legacy cypher no longer carries a label whitelist; update this guard")
	}
	end := strings.Index(clause[start:], "]")

	// Both directions run over the PARSED bracket contents. An earlier revision
	// checked the Go-to-Cypher direction with strings.Contains over the whole
	// constant, so dropping a label from the whitelist while leaving it quoted
	// anywhere else -- an unrelated `<> 'DataAsset'` predicate, say -- satisfied
	// the mirror with an incidental occurrence.
	cypherLabels := map[string]struct{}{}
	for _, quoted := range strings.Split(clause[start+len("label IN ["):start+end], ",") {
		cypherLabels[strings.Trim(strings.TrimSpace(quoted), "'")] = struct{}{}
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
	if armTwo := strings.Index(changeSurfaceScopedOutgoingCypher, "(impacted:Repository)"); armTwo >= 0 && start > armTwo {
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
