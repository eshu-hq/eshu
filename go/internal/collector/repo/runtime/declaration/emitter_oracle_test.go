// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
)

// oracleFact is what the generator expects one emitted fact to be. The
// generator builds each line from atoms it chose, so it knows the answer
// without parsing the text: this is an independent model of the value rule
// (values.go), the "=" pin rule (classify.go) and the .swift-version grammar,
// not a copy of their code.
type oracleFact struct {
	raw, kind                  string
	line, order                int
	major, lower, upper        int
	lowerIncl, upperIncl       int
	placeholder, breaksTheLine bool
}

type oracleGen struct{ r *rand.Rand }

func (g oracleGen) spaces() string {
	return []string{" ", "  ", "\t", " \t "}[g.r.Intn(4)]
}

// version returns a version text, its major and its numeric component count.
func (g oracleGen) version(minMajor int) (string, int, int) {
	major := minMajor + g.r.Intn(30)
	comps := 1 + g.r.Intn(3)
	text := strconv.Itoa(major)
	for i := 1; i < comps; i++ {
		text += "." + strconv.Itoa(g.r.Intn(20))
	}
	return text, major, comps
}

func pinKind(comps int) string {
	if comps >= 3 {
		return "exact"
	}
	return "partial"
}

// atom returns one value atom and its expected fact (order and line unset).
func (g oracleGen) atom() oracleFact {
	none := oracleFact{major: -1, lower: -1, upper: -1, lowerIncl: -1, upperIncl: -1}
	switch g.r.Intn(7) {
	case 0, 1: // a plain version, sometimes with a v prefix
		text, major, comps := g.version(0)
		if g.r.Intn(4) == 0 {
			text = "v" + text
		}
		f := none
		f.raw, f.kind, f.major = text, pinKind(comps), major
		return f
	case 2, 3: // a comparator, spaced or not
		ops := []string{">=", ">", "<", "<=", "=", "==", "^", "~", "~>"}
		op := ops[g.r.Intn(len(ops))]
		text, major, comps := g.version(1)
		sep := ""
		if g.r.Intn(2) == 0 {
			sep = g.spaces()
		}
		f := none
		f.raw = op + sep + text
		switch op {
		case "=", "==":
			f.kind, f.major = pinKind(comps), major
			return f
		case ">=", ">":
			f.lower, f.lowerIncl = major, boolInt(op == ">=")
		case "<", "<=":
			f.upper, f.upperIncl = major, boolInt(op == "<=")
		case "^":
			f.lower, f.lowerIncl, f.upper, f.upperIncl = major, 1, major+1, 0
		case "~", "~>":
			f.lower, f.lowerIncl = major, 1
			if comps <= 1 || (op == "~>" && comps == 2) {
				f.upper, f.upperIncl = major+1, 0
			} else {
				f.upper, f.upperIncl = major, 1
			}
		}
		f.kind = "range"
		return f
	case 4: // a named reference
		f := none
		f.raw = []string{"lts/iron", "system", "stable", "latest", "node", "lts/*"}[g.r.Intn(6)]
		f.kind = "alias"
		return f
	case 5: // a placeholder, which may hold spaces
		f := none
		f.raw = []string{"${X}", "$NODE_VERSION", "{{ v }}", "${{ inputs.node-version }}"}[g.r.Intn(4)]
		f.kind, f.placeholder = "unresolved", true
		return f
	default: // an operator-only token that breaks the value into one declaration
		f := none
		// A comparator before a name is not a version either.
		f.raw = []string{"-", "||", "--", "!", ">= lts/iron", "<" + g.spaces() + "system", "= latest"}[g.r.Intn(7)]
		f.breaksTheLine = true
		return f
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// value builds a whitespace-split value of n atoms and the facts it emits.
func (g oracleGen) value(n int, line int) (string, []oracleFact) {
	atoms := make([]oracleFact, 0, n+1)
	for i := 0; i < n; i++ {
		atoms = append(atoms, g.atom())
	}
	if g.r.Intn(10) == 0 { // a comparator with no version after it
		f := oracleFact{raw: []string{">=", "=", "<", "^"}[g.r.Intn(4)], breaksTheLine: true}
		atoms = append(atoms, f)
	}
	parts := make([]string, len(atoms))
	whole, placeholder := false, false
	for i, a := range atoms {
		parts[i] = a.raw
		whole = whole || a.breaksTheLine
		placeholder = placeholder || a.placeholder
	}
	text := ""
	for i, p := range parts {
		if i > 0 {
			text += g.spaces()
		}
		text += p
	}
	if whole {
		kind := "alias"
		if placeholder {
			kind = "unresolved"
		}
		return text, []oracleFact{{raw: text, kind: kind, line: line, major: -1, lower: -1, upper: -1, lowerIncl: -1, upperIncl: -1}}
	}
	for i := range atoms {
		atoms[i].line, atoms[i].order = line, i
	}
	return text, atoms
}

// swift returns a .swift-version value and its expected fact.
func (g oracleGen) swift() oracleFact {
	f := oracleFact{line: 1, major: -1, lower: -1, upper: -1, lowerIncl: -1, upperIncl: -1, kind: "alias"}
	a, b, c := g.r.Intn(7), g.r.Intn(11), g.r.Intn(5)
	switch g.r.Intn(8) {
	case 0:
		f.raw, f.kind, f.major = strconv.Itoa(a), "partial", a
	case 1:
		f.raw, f.kind, f.major = fmt.Sprintf("%d.%d", a, b), "partial", a
	case 2:
		f.raw, f.kind, f.major = fmt.Sprintf("%d.%d.%d", a, b, c), "exact", a
	case 3:
		f.raw = fmt.Sprintf("swift-%d.%d-RELEASE", a, b)
	case 4:
		f.raw = fmt.Sprintf("main-snapshot-2024-%02d-%02d", 1+b, 1+c)
	case 5:
		f.raw = fmt.Sprintf("%d.%d-DEVELOPMENT-SNAPSHOT-2024-01-01-a", a, b)
	case 6:
		f.raw = []string{fmt.Sprintf("v%d.%d", a, b), fmt.Sprintf("%d.x", a), fmt.Sprintf(">=%d.%d", a, b), fmt.Sprintf("%d.%d.%d.%d", a, b, c, c)}[g.r.Intn(4)]
	default:
		f.raw, f.kind = "${SWIFT_VERSION}", "unresolved"
	}
	return f
}

// oracleCase returns one pin file and the facts the oracle expects from it.
func (g oracleGen) oracleCase() (string, string, []oracleFact) {
	eol := []string{"\n", "\r\n", " # note\n"}[g.r.Intn(3)]
	switch g.r.Intn(4) {
	case 0:
		text, want := g.value(1+g.r.Intn(3), 1)
		return ".nvmrc", text + eol + "18\n", want
	case 1:
		first, want := g.value(1+g.r.Intn(3), 1)
		second, more := g.value(1+g.r.Intn(2), 2)
		return ".tool-versions", "nodejs " + first + eol + "python" + g.spaces() + second + "\n", append(want, more...)
	case 2:
		// .sdkmanrc classifies its whole value, so one atom (plus an optional
		// dangling comparator) keeps the oracle exact.
		text, want := g.value(1, 1)
		return ".sdkmanrc", "java=" + text + eol, want
	default:
		f := g.swift()
		return ".swift-version", f.raw + eol + "6.0\n", []oracleFact{f}
	}
}

// TestEmitMatchesTheValueOracle drives Emit over generated pin files and
// compares every fact with the generator's model. The seed is fixed so a
// failure reproduces; ESHU_RUNTIME_DECLARATION_ORACLE_CASES raises the count.
func TestEmitMatchesTheValueOracle(t *testing.T) {
	t.Parallel()

	cases := 4000
	if raw := os.Getenv("ESHU_RUNTIME_DECLARATION_ORACLE_CASES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			t.Fatalf("ESHU_RUNTIME_DECLARATION_ORACLE_CASES = %q, want a positive integer", raw)
		}
		cases = n
	}
	g := oracleGen{r: rand.New(rand.NewSource(8969))}
	violations := 0
	for i := 0; i < cases && violations < 20; i++ {
		path, body, want := g.oracleCase()
		got := emitDecoded(t, path, body)
		if len(got) != len(want) {
			violations++
			t.Errorf("case %d Emit(%s, %q) = %d facts %s, want %d", i, path, body, len(got), describeVersions(got), len(want))
			continue
		}
		for j, w := range want {
			v := got[j]
			if v.RawSpec != w.raw || v.SpecKind != w.kind || v.LineNumber != w.line || v.OrderIndex != w.order ||
				deref(v.VersionMajor) != w.major || deref(v.RangeLowerMajor) != w.lower || deref(v.RangeUpperMajor) != w.upper ||
				derefBool(v.RangeLowerInclusive) != w.lowerIncl || derefBool(v.RangeUpperInclusive) != w.upperIncl {
				violations++
				t.Errorf("case %d Emit(%s, %q) fact[%d] = raw %q kind %s line %d order %d major %d lower %d/%d upper %d/%d, want %+v",
					i, path, body, j, v.RawSpec, v.SpecKind, v.LineNumber, v.OrderIndex, deref(v.VersionMajor),
					deref(v.RangeLowerMajor), derefBool(v.RangeLowerInclusive), deref(v.RangeUpperMajor), derefBool(v.RangeUpperInclusive), w)
			}
		}
	}
	t.Logf("oracle cases=%d violations=%d", cases, violations)
}
