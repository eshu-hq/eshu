// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// This file holds the interpolation arm of the grammar fuzz. It wraps
// generated statements in two brace blocks that open with interpolated
// strings holding unbalanced brackets inside nested strings (issue #8980).
// Each block ends with a same-line probe on a fixed worst-case host: a
// dependency probe in the first block, a val probe in the second. A scanner
// that loses bracket sync on those strings resolves the probe dependency or
// leaks the probe val into a top-level version reference, which the I1/I2/I3
// and missing invariants catch. The two probes need separate blocks because
// the first fake closer wins: a second one on the same block goes unmatched.
// Coordinates in the blocks are marked unknown; the column-0 control
// statements must resolve exactly as the generator drew them.

import (
	"fmt"
	"math/rand"
	"strings"
)

// fzInterpHosts are hostile interpolated strings: every nested string holds
// a bracket that must not count toward the `${...}` depth.
var fzInterpHosts = []string{
	`s"${"}"}"`,
	`s"${"{"}"`,
	`s"${"["}"`,
	`s"${"]"}"`,
	`s"${s"${x}"}"`,
	`s"${if (ok) "}" else "{"}"`,
	`s"$x${"}"}"`,
	`s"$$${x}"`,
	`s"${f(1 /* " */)}"`,
	`s"${"""x"""}}"`,
	`s"""${"}"}"""`,
	`s"${Map(1 -> 2)}"`,
}

// fzInterpDepProbe is the dependency probe continuing the first block's
// probe line. It is marked unknown: it must never resolve.
var fzInterpDepProbe = fzDep{
	org: "qx.interp", name: "qy", ver: "1", verText: `"1"`,
	text:    `"qx.interp" % "qy" % "1"`,
	unknown: true, tags: map[string]bool{"INTERP-SAMELINE": true},
}

// fzInterpBlock writes one hostile brace block holding generated statements.
// Every coordinate it declares is marked unknown.
func fzInterpBlock(g *fzGen, body *strings.Builder, all *[]fzDep, head string) {
	body.WriteString(head + " {\n")
	nhosts := 1 + g.r.Intn(2)
	for h := 0; h < nhosts; h++ {
		fmt.Fprintf(body, "  val h%d = %s\n", h, fzInterpHosts[g.r.Intn(len(fzInterpHosts))])
	}
	stmts := 1 + g.r.Intn(2)
	for s := 0; s < stmts; s++ {
		text, deps := g.statement()
		body.WriteString(indentLines(text, 2))
		for i := range deps {
			deps[i].unknown = true
			deps[i].tags["INTERP-BLOCK"] = true
		}
		*all = append(*all, deps...)
	}
}

// fzInterpBuild generates one build file with two hostile brace blocks (a
// dependency probe, then a val probe shadowing a control's version val),
// followed by the top-level version vals, one more hostile string, and
// column-0 control statements.
func fzInterpBuild(seed int64) fzCase {
	g := &fzGen{r: rand.New(rand.NewSource(seed)), vals: map[string]string{}}
	var body strings.Builder
	var all []fzDep
	// The controls are drawn before the val probe so the probe can shadow a
	// version val a control actually references.
	var ctrlTexts []string
	var ctrlDeps []fzDep
	controls := 1 + g.r.Intn(2)
	for s := 0; s < controls; s++ {
		text, deps := g.statement()
		ctrlTexts = append(ctrlTexts, text)
		ctrlDeps = append(ctrlDeps, deps...)
	}
	shadowTarget := ""
	for _, d := range ctrlDeps {
		if !strings.HasPrefix(d.verText, "\"") {
			shadowTarget = d.verText
			break
		}
	}
	fzInterpBlock(g, &body, &all, "foo")
	probe := fzInterpDepProbe
	probe.tags = map[string]bool{"INTERP-SAMELINE": true}
	fmt.Fprintf(&body, "  val hp = %s; libraryDependencies += %s; name := \"x\"\n", `s"${"}"}"`, probe.text)
	all = append(all, probe)
	body.WriteString("}\n")
	fzInterpBlock(g, &body, &all, "bar")
	if shadowTarget != "" {
		fmt.Fprintf(&body, "  val hq = %s; val %s = %q\n", `s"${"}"}"`, shadowTarget, fzShadowValue)
		for i := range ctrlDeps {
			if ctrlDeps[i].verText == shadowTarget {
				ctrlDeps[i].tags["SHADOW"] = true
			}
		}
	}
	for i := 1; i <= g.n; i++ {
		vn := fmt.Sprintf("v%d", i)
		if _, ok := g.vals[vn]; !ok || g.r.Intn(2) == 0 {
			continue
		}
		fmt.Fprintf(&body, "  val %s = %q\n", vn, fzShadowValue)
	}
	body.WriteString("}\n")
	// Both blocks (with their shadows) come before the top-level version
	// vals: vals resolve first-wins, so a leaked shadow flips a control to
	// the shadow value and trips I2.
	var out strings.Builder
	out.WriteString(body.String())
	g.writeVals(&out)
	out.WriteString("lazy val core = project\n")
	out.WriteString("val htop = " + fzInterpHosts[g.r.Intn(len(fzInterpHosts))] + "\n")
	for _, text := range ctrlTexts {
		out.WriteString(text + "\n")
	}
	all = append(all, ctrlDeps...)
	return fzCase{text: out.String(), deps: all}
}
