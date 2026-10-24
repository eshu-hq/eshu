// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// grammarFuzzCases is the default number of seeds. SBT_GRAMMAR_FUZZ_CASES
// raises it for a deeper local run (the review run used 30,000).
const grammarFuzzCases = 4_000

// fuzzReport tallies invariant violations across seeds.
type fuzzReport struct {
	cases, rows                    int
	i1, i2, i3, missing, fabricate int
	shown                          int
	classes                        map[string]int
}

// TestGrammarSeededFuzzInvariants runs the seeded build.sbt generator and
// checks every emitted row against three invariants:
//
//	I1: a dependency with an unknown tail, a non-compile configuration, or a
//	    scoped axis is never a resolved compile row.
//	I2: a resolved row carries the group, artifact, and version as written,
//	    and no coordinate-rewriting tail touched it.
//	I3: a dependency with an unknown tail is never resolved.
//
// Every generated coordinate must also be emitted (partial at worst), and no
// resolved row may appear that the generator did not write. The indentation
// arm (fuzz_indent_test.go) runs the same checks over Scala 3 braceless
// blocks, whose coordinates are all unknown. The member arm
// (fuzz_member_test.go) checks the exact row states, unresolved count and
// scalaVersion envelope around member calls, legacy and unknown assignment
// operators, and nested scalaVersion literals.
func TestGrammarSeededFuzzInvariants(t *testing.T) {
	t.Parallel()

	cases := grammarFuzzCases
	if raw := os.Getenv("SBT_GRAMMAR_FUZZ_CASES"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			t.Fatalf("SBT_GRAMMAR_FUZZ_CASES = %q, want a positive integer", raw)
		}
		cases = parsed
	}
	report := fuzzReport{classes: map[string]int{}}
	indent := fuzzReport{classes: map[string]int{}}
	member := memberReport{}
	for seed := int64(1); seed <= int64(cases); seed++ {
		report.check(t, seed, fzBuild(seed))
		indent.check(t, seed, fzIndentBuild(seed))
		member.check(t, seed, fzMemberBuild(seed))
	}
	member.cases = cases
	report.cases, indent.cases = cases, cases
	for _, arm := range []struct {
		name   string
		report *fuzzReport
	}{{"grammar", &report}, {"indentation", &indent}} {
		r := arm.report
		t.Logf("%s: cases=%d rows=%d I1=%d I2=%d I3=%d missing=%d fabricated=%d classes=[%s]",
			arm.name, r.cases, r.rows, r.i1, r.i2, r.i3, r.missing, r.fabricate, r.classList())
		if r.i1+r.i2+r.i3+r.missing+r.fabricate > 0 {
			t.Errorf("%s fuzz found violations: I1=%d I2=%d I3=%d missing=%d fabricated=%d",
				arm.name, r.i1, r.i2, r.i3, r.missing, r.fabricate)
		}
	}
	t.Logf("member: cases=%d rows=%d state=%d extra=%d count=%d scala=%d hint=%d",
		member.cases, member.rows, member.state, member.extra, member.count, member.scala, member.hint)
	if member.failures() > 0 {
		t.Errorf("member fuzz found violations: state=%d extra=%d count=%d scala=%d hint=%d",
			member.state, member.extra, member.count, member.scala, member.hint)
	}
}

// classList renders the violation classes in a stable order.
func (r *fuzzReport) classList() string {
	classes := make([]string, 0, len(r.classes))
	for class, count := range r.classes {
		classes = append(classes, class+"="+strconv.Itoa(count))
	}
	sort.Strings(classes)
	return strings.Join(classes, " ")
}

// check parses one generated case and records its violations.
func (r *fuzzReport) check(t *testing.T, seed int64, c fzCase) {
	t.Helper()
	rows := dependencyRows(t, parseSource(t, c.text))
	r.rows += len(rows)
	byName := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		byName[row["name"].(string)] = row
	}
	for _, dep := range c.deps {
		name := dep.org + ":" + dep.name
		row, ok := byName[name]
		if !ok {
			r.missing++
			r.log(t, "missing", seed, name, nil, c.text)
			continue
		}
		delete(byName, name)
		resolved := row["dependency_resolution_state"] == "resolved"
		compile := row["dependency_scope"] == "compile"
		if (dep.unknown || dep.nonCompile) && resolved && compile {
			r.i1++
			r.classify(dep, "I1")
			r.log(t, "I1", seed, name, row, c.text)
		}
		if resolved && (row["value"] != dep.ver || dep.rewrite) {
			r.i2++
			r.classify(dep, "I2")
			r.log(t, "I2", seed, name, row, c.text)
		}
		if dep.unknown && resolved {
			r.i3++
			r.classify(dep, "I3")
			r.log(t, "I3", seed, name, row, c.text)
		}
	}
	for name, row := range byName {
		if row["dependency_resolution_state"] == "resolved" {
			r.fabricate++
			r.log(t, "fabricated", seed, name, row, c.text)
		}
	}
}

// classify counts a violation under the generator tags of the dependency.
func (r *fuzzReport) classify(dep fzDep, invariant string) {
	tags := make([]string, 0, len(dep.tags))
	for tag := range dep.tags {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	r.classes[invariant+":"+strings.Join(tags, "+")]++
}

// log prints the first few violations with their source so a failure is
// reproducible from the seed alone.
func (r *fuzzReport) log(t *testing.T, kind string, seed int64, name string, row map[string]any, text string) {
	t.Helper()
	if r.shown >= 5 {
		return
	}
	r.shown++
	t.Logf("%s seed=%d dep=%s scope=%v state=%v value=%v\n%s", kind, seed, name, row["dependency_scope"], row["dependency_resolution_state"], row["value"], text)
}
