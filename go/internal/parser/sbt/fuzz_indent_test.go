// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// This file holds the indentation arm of the grammar fuzz. sbt 2.x reads
// build.sbt as Scala 3, where a braceless block is an indented region at
// bracket depth zero. The arm wraps generated statements in such blocks and
// marks their coordinates unknown, so I1 and I3 fail if any of them resolves,
// and it declares block-local vals that shadow a top-level version val, so I2
// fails if a top-level reference resolves to the block-local value.

import (
	"fmt"
	"math/rand"
	"strings"
)

// fzIndentBlock is one Scala 3 braceless block shape: the lines before the
// wrapped statement, its indentation, and the lines after it. A sameLine block
// ends its head with a closer and ';' at column 0, so the statement starts on
// the closer's line, still inside the block, and only its continuation lines
// are indented.
type fzIndentBlock struct {
	name, head, tail string
	indent           int
	sameLine         bool
}

var fzIndentBlocks = []fzIndentBlock{
	{name: "if", head: "if isCI then\n  println(1)\n", indent: 2, tail: "else\n  println(2)\n"},
	{name: "else", head: "if isCI then\n  println(1)\nelse\n  println(2)\n", indent: 2},
	{name: "def", head: "def deps =\n  println(1)\n", indent: 2},
	{name: "lazy-val", head: "lazy val helper =\n  println(1)\n", indent: 2},
	{name: "match", head: "isCI match\n  case true =>\n    println(1)\n", indent: 4, tail: "  case _ =>\n    println(2)\n"},
	{name: "for", head: "for i <- 1 to 2 do\n  println(i)\n", indent: 2},
	{name: "while", head: "while isCI do\n  println(1)\n", indent: 2},
	{name: "bare", head: "println(0)\n", indent: 2},
	{name: "brace-closer", head: "def deps =\n  locally {\n  1\n}; ", indent: 2, sameLine: true},
	{name: "paren-closer", head: "def deps =\n  foo(\n  1\n); ", indent: 2, sameLine: true},
	{name: "bracket-closer", head: "def deps =\n  foo[\n  Int\n]; ", indent: 2, sameLine: true},
}

// fzShadowForms declare a block-local val; %[1]s is its name, %[2]s its value.
var fzShadowForms = []string{
	"lazy val shadow =\n  val %[1]s = \"%[2]s\"\n  %[1]s\n",
	"def shadow =\n  val %[1]s = \"%[2]s\"\n  %[1]s\n",
	"if isCI then\n  val %[1]s = \"%[2]s\"\n  println(%[1]s)\n",
	"isCI match\n  case true =>\n    val %[1]s = \"%[2]s\"\n    println(%[1]s)\n  case _ =>\n    println(0)\n",
	"def shadow =\n  locally {\n  1\n}; val %[1]s = \"%[2]s\"\n",
	"def shadow =\n  foo(\n  1\n); val %[1]s = \"%[2]s\"\n",
}

// fzShadowValue is the block-local version; no generated coordinate uses it.
const fzShadowValue = "0.0.0-shadow"

// indentLines prefixes every line of text with width spaces.
func indentLines(text string, width int) string {
	pad := strings.Repeat(" ", width)
	return pad + strings.ReplaceAll(text, "\n", "\n"+pad) + "\n"
}

// wrap places a generated statement inside the block.
func (b fzIndentBlock) wrap(text string) string {
	if !b.sameLine {
		return indentLines(text, b.indent)
	}
	first, rest, multi := strings.Cut(text, "\n")
	if !multi {
		return first + "\n"
	}
	return first + "\n" + indentLines(rest, b.indent)
}

// fzIndentBuild generates one build file whose statements sit either at
// column 0 or inside a braceless block, with block-local vals declared before
// the top-level vals they shadow.
func fzIndentBuild(seed int64) fzCase {
	g := &fzGen{r: rand.New(rand.NewSource(seed)), vals: map[string]string{}}
	var body strings.Builder
	var all []fzDep
	stmts := 1 + g.r.Intn(3)
	for s := 0; s < stmts; s++ {
		text, deps := g.statement()
		kind := g.r.Intn(len(fzIndentBlocks) + 1)
		if kind == len(fzIndentBlocks) {
			body.WriteString(text + "\n")
			all = append(all, deps...)
			continue
		}
		block := fzIndentBlocks[kind]
		body.WriteString(block.head + block.wrap(text) + block.tail)
		for i := range deps {
			deps[i].unknown = true
			deps[i].tags["IND-"+block.name] = true
		}
		all = append(all, deps...)
	}
	var out strings.Builder
	for i := 1; i <= g.n; i++ {
		vn := fmt.Sprintf("v%d", i)
		if _, ok := g.vals[vn]; !ok || g.r.Intn(2) == 0 {
			continue
		}
		fmt.Fprintf(&out, fzShadowForms[g.r.Intn(len(fzShadowForms))], vn, fzShadowValue)
		for j := range all {
			if all[j].verText == vn {
				all[j].tags["SHADOW"] = true
			}
		}
	}
	g.writeVals(&out)
	out.WriteString("lazy val core = project\n")
	out.WriteString(body.String())
	return fzCase{text: out.String(), deps: all}
}
