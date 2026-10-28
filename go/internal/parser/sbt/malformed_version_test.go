// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// TestLiteralRevisionRejectedByExactRuleIsUnresolved pins issue #9088: a
// literal revision the shared exact-version rule rejects ("1.0!", "1.0:jdk8")
// is not a version, so its row is unresolved and keeps the literal text, the
// way the Gradle readers treat a selector. A malformed revision names no
// interpolation key, so none is recorded. Legitimate revisions keep resolving.
func TestLiteralRevisionRejectedByExactRuleIsUnresolved(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"1.0!", "1.0:jdk8", "1.0!!", "1.0@jar"} {
		payload := parseSource(t, `libraryDependencies += "a" % "b" % "`+version+`"`+"\n")
		row := requireRow(t, payload, "a:b")
		requireField(t, row, "dependency_resolution_state", "unresolved")
		requireField(t, row, "value", version)
		if _, ok := row["dependency_unresolved_keys"]; ok {
			t.Fatalf("version %s: row = %#v, a malformed literal names no key", version, row)
		}
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)
	}

	for _, version := range []string{"1.0", "1.2.3", "2.13.12", "1.0-SNAPSHOT", "5.0.0-M1", "1.0.0.Final", "33.0.0-jre", "1.2.3_beta"} {
		payload := parseSource(t, `libraryDependencies += "a" % "b" % "`+version+`"`+"\n")
		row := requireRow(t, payload, "a:b")
		requireField(t, row, "dependency_resolution_state", "resolved")
		requireField(t, row, "value", version)
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
	}
}

// TestMalformedVersionThroughValIsUnresolved pins the val-indirection variant:
// a top-level val holding a malformed revision resolves to that text, so the
// row is unresolved with the text, not resolved through the val.
func TestMalformedVersionThroughValIsUnresolved(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, "val v = \"1.0!\"\nlibraryDependencies += \"a\" % \"b\" % v\n")
	row := requireRow(t, payload, "a:b")
	requireField(t, row, "dependency_resolution_state", "unresolved")
	requireField(t, row, "value", "1.0!")

	legit := parseSource(t, "val v = \"1.0\"\nlibraryDependencies += \"a\" % \"b\" % v\n")
	legitRow := requireRow(t, legit, "a:b")
	requireField(t, legitRow, "dependency_resolution_state", "resolved")
	requireField(t, legitRow, "value", "1.0")
}

// malformedQualifiers are suffixes no Ivy revision carries: '!' is not an Ivy
// operator, ':classifier' leaks string-notation text into the version, '@ext'
// is artifact-only notation, and '!!' is the strict short-hand.
var malformedQualifiers = []string{"!", ":jdk8", "@jar", "!!"}

// exactQualifiers are revision suffixes Maven Central carries on numeric
// versions.
var exactQualifiers = []string{"", "", "-SNAPSHOT", "-rc1", "-M1", ".Final", "-jre", "_beta"}

// corruptVersion applies one malformed shape to an exact base version.
func corruptVersion(rng *rand.Rand, base string) string {
	return base + malformedQualifiers[rng.Intn(len(malformedQualifiers))]
}

// exactVersion draws a numeric version with a legitimate qualifier.
func exactVersion(rng *rand.Rand) string {
	return fmt.Sprintf("%d.%d.%d%s", rng.Intn(30), rng.Intn(30), rng.Intn(30),
		exactQualifiers[rng.Intn(len(exactQualifiers))])
}

// renderRevision renders a revision across the notations the reader exposes:
// the three cross-version operators and a top-level val reference. A val
// literal cannot hold ':' (validField rejects it, so the val never records),
// so a colon-bearing revision only renders as a direct literal: the only
// spelling that reaches a row.
func renderRevision(rng *rand.Rand, version string, i int) (statement, decl string) {
	ops := []string{"%", "%%", "%%%"}
	op := ops[rng.Intn(len(ops))]
	if rng.Intn(2) == 0 || strings.Contains(version, ":") {
		return fmt.Sprintf("libraryDependencies += \"g%d\" %s \"a%d\" %% \"%s\"\n", i, op, i, version), ""
	}
	prop := fmt.Sprintf("v%d", i)
	return fmt.Sprintf("libraryDependencies += \"g%d\" %s \"a%d\" %% %s\n", i, op, i, prop),
		fmt.Sprintf("val %s = \"%s\"\n", prop, version)
}

// TestGeneratedRevisionsMatchTheExactOracle drives seeded build files (one
// per round, several revisions each) through Parse and checks every row
// against the generator's family label, which comes from Ivy/Maven revision
// shapes, not from the implementation: an exact revision resolves to the text
// as written, a malformed one stays unresolved keeping the literal. The proof
// is that no exact revision flips.
func TestGeneratedRevisionsMatchTheExactOracle(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(9088))
	for round := 0; round < 300; round++ {
		var decls, body strings.Builder
		type want struct {
			exact bool
			value string
		}
		wants := map[string]want{}
		for i, n := 0, 1+rng.Intn(4); i < n; i++ {
			version := exactVersion(rng)
			exact := true
			if rng.Intn(2) == 0 {
				version = corruptVersion(rng, version)
				exact = false
			}
			statement, decl := renderRevision(rng, version, 10*round+i)
			decls.WriteString(decl)
			body.WriteString(statement)
			wants[fmt.Sprintf("g%d:a%d", 10*round+i, 10*round+i)] = want{exact: exact, value: version}
		}
		payload := parseSource(t, decls.String()+body.String())
		rows := dependencyRows(t, payload)
		if len(rows) != len(wants) {
			t.Fatalf("round %d: %d rows, want %d", round, len(rows), len(wants))
		}
		for _, row := range rows {
			name, _ := row["name"].(string)
			exp, ok := wants[name]
			if !ok {
				t.Fatalf("round %d: unexpected row %q", round, name)
			}
			state := row["dependency_resolution_state"]
			if exp.exact && (state != "resolved" || row["value"] != exp.value) {
				t.Fatalf("round %d: %s = %#v, want resolved %q", round, name, row, exp.value)
			}
			if !exp.exact && (state != "unresolved" || row["value"] != exp.value) {
				t.Fatalf("round %d: %s = %#v, want unresolved keeping %q", round, name, row, exp.value)
			}
			if !exp.exact {
				if _, ok := row["dependency_unresolved_keys"]; ok {
					t.Fatalf("round %d: %s = %#v, a malformed literal names no key", round, name, row)
				}
			}
		}
	}
}
