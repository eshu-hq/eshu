// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// This file holds the seeded build.sbt generator for the grammar fuzz in
// fuzz_test.go. Each generated dependency records what the generator knows
// about it (an unknown tail, a coordinate rewrite, a non-compile configuration
// or scoped axis), so the invariants can check every emitted row.

import (
	"fmt"
	"math/rand"
	"strings"
)

// fzTail is one tail the generator can append to a coordinate.
type fzTail struct {
	text    string // includes leading separator
	unknown bool
	rewrite bool // changes coordinate
}

var fzNeutral = []string{
	".exclude(\"x\", \"y\")", " exclude(\"x\", \"y\")", ".excludeAll(ExclusionRule(\"x\"))",
	".classifier(\"tests\")", " classifier \"tests\"", ".intransitive()", " intransitive()",
	".notTransitive()", ".withSources()", " withSources()", ".withJavadoc()", ".force()",
	".changing()", ".artifacts(Artifact(\"x\"))", ".from(\"http://x/y.jar\")",
}

var fzUnknown = []fzTail{
	{text: ".withConfigurations(Some(\"test\"))", unknown: true},
	{text: " withConfigurations(Some(\"test\"))", unknown: true},
	{text: ".configure(\"test\")", unknown: true},
	{text: ".foo()", unknown: true},
	{text: " bar", unknown: true},
	{text: " + baz", unknown: true},
	{text: " ## qux", unknown: true},
	{text: " \"stray\"", unknown: true},
	{text: ".withRevision(\"9\")", unknown: true, rewrite: true},
	{text: ".withName(\"zz\")", unknown: true, rewrite: true},
	{text: " (x)", unknown: true},
}

var fzCfgs = []struct {
	text       string
	nonCompile bool
}{
	{"\"test\"", true},
	{"Test", true},
	{"\"it,test\"", true},
	{"\"it;test\"", true},
	{"Runtime", true},
	{"\"runtime\"", true},
	{"Provided", true},
	{"\"provided\"", true},
	{"\"\"", true},
	{"\"a->b\"", true},
	{"\"test->default(compile)\"", true},
	{"\"custom\"", true},
	{"IntegrationTest", true},
	{"Configurations.Test", true},
	{"\",\"", true},
	{"\"compile\"", false},
	{"Compile", false},
	{"\"compile->default\"", false},
	{"\"optional\"", false},
}

// fzDep is one generated coordinate and what the generator knows about it.
type fzDep struct {
	org, name, ver string
	verText        string
	text           string
	unknown        bool
	rewrite        bool
	nonCompile     bool
	hasCfg         bool
	tags           map[string]bool
}

// fzGen is the seeded generator state for one build file.
type fzGen struct {
	r        *rand.Rand
	n        int
	vals     map[string]string
	inParens bool
}

// sep returns a same-line separator, sometimes a comment.
func (g *fzGen) sep() string {
	switch g.r.Intn(8) {
	case 0:
		return " /* c */ "
	case 1:
		return "/**/"
	default:
		return " "
	}
}

// nl returns a separator that may break the line; only used where Scala continues.
func (g *fzGen) nl(allowNL bool) string {
	if allowNL {
		switch g.r.Intn(6) {
		case 0:
			return "\n    "
		case 1:
			return " // c\n    "
		}
	}
	return g.sep()
}

// dep generates one coordinate with random tails and an optional configuration.
func (g *fzGen) dep(allowCfg bool) fzDep {
	g.n++
	d := fzDep{org: fmt.Sprintf("g%d.org", g.n), name: fmt.Sprintf("a%d", g.n), tags: map[string]bool{}}
	ops := []string{"%", "%%", "%%%"}
	op := ops[g.r.Intn(3)]
	if g.r.Intn(3) == 0 {
		vn := fmt.Sprintf("v%d", g.n)
		d.ver = fmt.Sprintf("%d.%d", g.n, g.r.Intn(9))
		g.vals[vn] = d.ver
		d.verText = vn
	} else {
		d.ver = fmt.Sprintf("%d.%d.%d", g.n, g.r.Intn(9), g.r.Intn(9))
		d.verText = "\"" + d.ver + "\""
	}
	coord := "\"" + d.org + "\"" + g.sep() + op + g.sep() + "\"" + d.name + "\"" + g.sep() + "%" + g.sep() + d.verText
	wrapped := g.r.Intn(4) == 0
	if wrapped {
		coord = "(" + coord + ")"
	}
	var b strings.Builder
	b.WriteString(coord)
	nt := g.r.Intn(4)
	cfgPos := -1
	if allowCfg && g.r.Intn(2) == 0 {
		cfgPos = g.r.Intn(nt + 1)
	}
	for i := 0; i <= nt; i++ {
		if i == cfgPos {
			c := fzCfgs[g.r.Intn(len(fzCfgs))]
			// leading % on next line, or same line
			b.WriteString(g.nl(true))
			b.WriteString("%" + g.sep() + c.text)
			if d.unknown {
				d.tags["H3-unknown-then-cfg"] = true
			}
			d.hasCfg = true
			if c.nonCompile {
				d.nonCompile = true
			}
		}
		if i == nt {
			break
		}
		if g.r.Intn(4) == 0 {
			u := fzUnknown[g.r.Intn(len(fzUnknown))]
			// newline before an ident tail only when inside parens (Scala continues)
			allowNL := g.inParens || strings.HasPrefix(u.text, ".")
			if strings.HasPrefix(u.text, ".") {
				b.WriteString(g.nl(allowNL))
				b.WriteString(u.text)
			} else {
				if allowNL {
					sepx := g.nl(true)
					if strings.Contains(sepx, "\n") {
						d.tags["H2-newline-infix-in-parens"] = true
					}
					b.WriteString(sepx)
					b.WriteString(strings.TrimLeft(u.text, " "))
				} else {
					b.WriteString(u.text)
				}
			}
			d.unknown = true
			if u.text == " (x)" {
				d.tags["X-invalid-group-after-call"] = true
			}
			if u.rewrite {
				d.rewrite = true
			}
		} else {
			t := fzNeutral[g.r.Intn(len(fzNeutral))]
			if strings.HasPrefix(t, ".") {
				b.WriteString(g.nl(true))
			}
			b.WriteString(t)
		}
	}
	d.text = b.String()
	return d
}

// fzCase is one generated build file and its coordinates.
type fzCase struct {
	text string
	deps []fzDep
}

// fzBuild generates the build file for one seed.
func fzBuild(seed int64) fzCase {
	g := &fzGen{r: rand.New(rand.NewSource(seed)), vals: map[string]string{}}
	var out strings.Builder
	var all []fzDep
	stmts := 1 + g.r.Intn(3)
	var body strings.Builder
	for s := 0; s < stmts; s++ {
		text, stmtDeps := g.statement()
		body.WriteString(text + "\n")
		all = append(all, stmtDeps...)
	}
	g.writeVals(&out)
	out.WriteString("lazy val core = project\n")
	out.WriteString(body.String())
	return fzCase{text: out.String(), deps: all}
}

// writeVals writes the top-level version vals the generated coordinates use.
func (g *fzGen) writeVals(out *strings.Builder) {
	for i := 1; i <= g.n; i++ {
		vn := fmt.Sprintf("v%d", i)
		if v, ok := g.vals[vn]; ok {
			out.WriteString("val " + vn + " = \"" + v + "\"\n")
		}
	}
}

// statement generates one libraryDependencies statement, with an optional
// axis or wrapper, and the coordinates it declares.
func (g *fzGen) statement() (string, []fzDep) {
	// axis
	axisScoped := false
	prefix, suffix := "", ""
	key := "libraryDependencies"
	switch g.r.Intn(12) {
	case 0:
		key = "ThisBuild / libraryDependencies"
	case 1:
		key = "Global / libraryDependencies"
	case 2:
		key = "Test / libraryDependencies"
		axisScoped = true
	case 3:
		key = "Compile / libraryDependencies"
		axisScoped = true
	case 4:
		key = "libraryDependencies in Test"
		axisScoped = true
	case 5:
		key = "libraryDependencies.in(Test)"
		axisScoped = true
	case 6:
		prefix, suffix = "inConfig(Test)(", ")"
		axisScoped = true
	case 7:
		prefix, suffix = "inConfig(Test) { ", " }"
		axisScoped = true
	case 8:
		prefix, suffix = "lazy val core = project.settings(", ")"
	case 9:
		prefix, suffix = "inThisBuild(Seq(", "))"
	}
	inSettingsParens := strings.HasSuffix(prefix, "(")
	kind := g.r.Intn(3)
	var stmtDeps []fzDep
	var stmt strings.Builder
	stmt.WriteString(prefix + key)
	if kind == 0 {
		g.inParens = inSettingsParens
		d := g.dep(true)
		stmt.WriteString(" += " + d.text)
		stmtDeps = append(stmtDeps, d)
	} else {
		if kind == 1 {
			stmt.WriteString(" ++= ")
		} else {
			stmt.WriteString(" := ")
		}
		// collection-level config applies via map only if no element has cfg
		mapCfg := g.r.Intn(3) == 0
		nColl := 1 + g.r.Intn(2)
		infixMapAll := mapCfg && nColl > 1 && g.r.Intn(2) == 0
		var chainDeps []fzDep
		for c := 0; c < nColl; c++ {
			if c > 0 {
				stmt.WriteString(" ++ ")
			}
			colls := []string{"Seq", "List", "Vector"}
			stmt.WriteString(colls[g.r.Intn(3)] + "(\n  ")
			ne := 1 + g.r.Intn(3)
			var cdeps []fzDep
			for e := 0; e < ne; e++ {
				if e > 0 {
					stmt.WriteString(",\n  ")
				}
				g.inParens = true
				d := g.dep(!mapCfg)
				stmt.WriteString(d.text)
				cdeps = append(cdeps, d)
			}
			stmt.WriteString("\n)")
			// per-collection tail (dot form binds to this collection only)
			if !infixMapAll && g.r.Intn(3) == 0 {
				switch g.r.Intn(6) {
				case 0:
					stmt.WriteString(".distinct")
					if mapCfg && c == nColl-1 {
						for i := range cdeps {
							cdeps[i].tags["H5-noarg-call-swallows-infix"] = true
						}
					}
				case 1:
					stmt.WriteString(".toSeq")
					if mapCfg && c == nColl-1 {
						for i := range cdeps {
							cdeps[i].tags["H5-noarg-call-swallows-infix"] = true
						}
					}
				case 2:
					stmt.WriteString(".flatMap(x => Seq(x))")
					for i := range cdeps {
						cdeps[i].unknown = true
					}
				case 3:
					stmt.WriteString(".map(_.withSources())")
				case 4:
					stmt.WriteString(".map(x => x % Test)")
					for i := range cdeps {
						cdeps[i].unknown = true
					}
				case 5:
					stmt.WriteString(".map(_.foo())")
					for i := range cdeps {
						cdeps[i].unknown = true
					}
				}
			}
			lastWithMap := mapCfg && !infixMapAll && c == nColl-1
			if !lastWithMap && mapCfg && !infixMapAll {
				// Draw as the reference generator did, so a seed yields
				// the same build file as the 30,000-case review run.
				_ = g.r.Intn(2)
			}
			if lastWithMap {
				c2 := fzCfgs[g.r.Intn(len(fzCfgs))]
				forms := []string{".map(_ % " + c2.text + ")", " map (_ % " + c2.text + ")", "\n  .map(_ % " + c2.text + ")", ".map(_ %\n " + c2.text + " withSources())"}
				stmt.WriteString(forms[g.r.Intn(len(forms))])
				for i := range cdeps {
					if cdeps[i].unknown {
						cdeps[i].tags["H3b-unknown-then-collection-cfg"] = true
					}
					cdeps[i].hasCfg = true
					if c2.nonCompile {
						cdeps[i].nonCompile = true
					}
				}
			}
			chainDeps = append(chainDeps, cdeps...)
		}
		if infixMapAll {
			c2 := fzCfgs[g.r.Intn(len(fzCfgs))]
			stmt.WriteString(" map (_ % " + c2.text + ")")
			for i := range chainDeps {
				if chainDeps[i].unknown {
					chainDeps[i].tags["H3b-unknown-then-collection-cfg"] = true
				}
				chainDeps[i].hasCfg = true
				if c2.nonCompile {
					chainDeps[i].nonCompile = true
				}
			}
		}
		if infixMapAll {
			for i := range chainDeps {
				chainDeps[i].tags["H4-infix-map-over-chain"] = true
			}
		}
		stmtDeps = append(stmtDeps, chainDeps...)
	}
	stmt.WriteString(suffix)
	if axisScoped {
		for i := range stmtDeps {
			stmtDeps[i].nonCompile = true
			if strings.Contains(prefix, "{") {
				stmtDeps[i].tags["H1-inConfig-brace"] = true
			}
		}
	}
	return stmt.String(), stmtDeps
}
