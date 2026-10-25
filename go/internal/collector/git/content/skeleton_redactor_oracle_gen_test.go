// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// The body generators for TestRedactSkeletonOracle. tokenSoupBody throws the
// comment, string, template, and heredoc tokens together with no regard for
// validity; validBody and jsonBody build bodies hclsyntax lexes cleanly, so the
// two-direction comparison with the oracle runs on most of them.

var soupTokens = []string{
	"${", "%{", "$${", "%%{", "}", "{", `"`, `\"`, "<<EOF", "<<-EOF", "EOF", "<<INNER", "INNER",
	"#", "//", "/*", "*/", "\n", "\n", "\r\n", "\nPassw0rdHunter2: 1\n", "a", " ZqVx7Rk3Lm9 ", "x = ",
	strings.Repeat(`"${`, 40),
}

func tokenSoupBody(rng *rand.Rand) string {
	var b strings.Builder
	for n := 1 + rng.IntN(40); n > 0; n-- {
		b.WriteString(soupTokens[rng.IntN(len(soupTokens))])
		if rng.IntN(2) == 0 {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// hclGen builds valid HCL. Every newline it writes is g.nl, so one body is all
// LF or all CRLF.
type hclGen struct {
	rng *rand.Rand
	nl  string
}

func (g *hclGen) pick(options ...string) string { return options[g.rng.IntN(len(options))] }

func (g *hclGen) chance(percent int) bool { return g.rng.IntN(100) < percent }

func validBody(rng *rand.Rand) string {
	g := &hclGen{rng: rng, nl: g0nl(rng)}
	var b strings.Builder
	for n := 1 + rng.IntN(8); n > 0; n-- {
		b.WriteString(g.stmt())
		b.WriteString(g.nl)
	}
	body := b.String()
	if g.chance(15) {
		body = strings.TrimSuffix(body, g.nl)
	}
	return body
}

func g0nl(rng *rand.Rand) string {
	if rng.IntN(5) == 0 {
		return "\r\n"
	}
	return "\n"
}

func (g *hclGen) stmt() string {
	key := g.pick("a", "region", "db_password", "tags", "x_1")
	switch g.rng.IntN(14) {
	case 0:
		return key + " = " + g.quoted(0)
	case 1:
		return key + " = " + g.quoted(0) + g.pick(" # <<EOF", " // <<-EOF", " /* c */", " # \"")
	case 2, 3:
		return key + " = " + g.heredoc(0)
	case 4:
		return "# " + g.pick("<<EOF", "Passw0rdHunter2: 1", "\"", "${", "/*")
	case 5:
		return g.pick("// ", "//") + g.pick("<<EOF", "x", "\"${")
	case 6:
		return g.blockComment()
	case 7:
		return key + " = 1 " + g.blockComment()
	case 8:
		return "Passw0rdHunter2: 1"
	case 9:
		return key + " = {" + g.nl + "  b = " + g.quoted(0) + g.nl + "  c = [1, 2]" + g.nl + "}"
	case 10:
		return key + " = [" + g.nl + "  " + g.quoted(0) + "," + g.nl + "  " + g.quoted(0) + "," + g.nl + "]"
	case 11:
		return key + " = [" + g.heredoc(0) + g.nl + "]"
	case 12:
		return key + " = 3" + g.nl + g.nl + "Passw0rdHunter2: 1"
	}
	return key + " = " + g.pick("true", "var.a", "local.b[0]", `"1.2.3"`, "null")
}

func (g *hclGen) blockComment() string {
	switch g.rng.IntN(3) {
	case 0:
		return "/* one line */"
	case 1:
		return "/*" + g.nl + "Passw0rdHunter2: 1" + g.nl + "<<EOF" + g.nl + "*/"
	}
	return "/* \"" + g.nl + g.pick("EOF", "${", "}\"", "# x") + g.nl + " */"
}

// quoted writes a quoted template, with interpolations that may span lines.
func (g *hclGen) quoted(depth int) string {
	var b strings.Builder
	b.WriteByte('"')
	for n := g.rng.IntN(4); n >= 0; n-- {
		switch g.rng.IntN(6) {
		case 0:
			b.WriteString("${" + g.interp(depth) + "}")
		case 1:
			b.WriteString("%{ if a }x%{ endif }")
		case 2:
			b.WriteString(g.pick("$${", "%%{", "$$", `\"`, `\\`, "50%", "$", "#", "//", "/*"))
		default:
			b.WriteString(g.pick("text", " ", "k=v", "/a/*"))
		}
	}
	b.WriteByte('"')
	return b.String()
}

// interp writes the code of a ${ }. It may hold newlines, comments, nested
// strings, and a nested heredoc.
func (g *hclGen) interp(depth int) string {
	if depth >= 3 {
		return "var.a"
	}
	switch g.rng.IntN(8) {
	case 0:
		return " " + g.quoted(depth+1) + " "
	case 1:
		return g.nl + "  var.b" + g.nl
	case 2:
		return " # note" + g.nl + " var.c" + g.nl
	case 3:
		return " /* a" + g.nl + " b */ var.d "
	case 4:
		return " " + g.heredoc(depth+1) + g.nl
	case 5:
		return " { a = " + g.quoted(depth+1) + " }.a "
	case 6:
		return " f(" + g.nl + "  " + g.quoted(depth+1) + "," + g.nl + ") "
	}
	return " var.a "
}

// heredoc writes <<ID, a body, and the closing marker, with no trailing
// newline. The marker is distinct at each nesting depth.
func (g *hclGen) heredoc(depth int) string {
	marker := fmt.Sprintf("EOT%d", depth)
	open := g.pick("<<", "<<-") + marker + g.nl
	var b strings.Builder
	b.WriteString(open)
	for n := g.rng.IntN(5); n >= 0; n-- {
		switch g.rng.IntN(7) {
		case 0:
			b.WriteString("line ${" + g.interp(depth) + "} tail")
		case 1:
			b.WriteString("Passw0rdHunter2: 1")
		case 2:
			b.WriteString(g.pick("$${ x", "%%{ y", "\"quoted\"", "# hash", "/* open", "*/ close", "<<NOT", marker+"X"))
		case 3:
			b.WriteString("${" + g.interp(depth) + "}" + marker)
		case 4:
			b.WriteString("%{ if a }")
		}
		b.WriteString(g.nl)
	}
	b.WriteString(g.pick("", "  ", "\t") + marker)
	return b.String()
}

// jsonBody writes a pretty-printed JSON body, a .tfvars.json shape.
func jsonBody(rng *rand.Rand) string {
	g := &hclGen{rng: rng, nl: g0nl(rng)}
	var b strings.Builder
	g.jsonObject(&b, 0)
	b.WriteString(g.nl)
	return b.String()
}

func (g *hclGen) jsonObject(b *strings.Builder, depth int) {
	indent := strings.Repeat("  ", depth+1)
	b.WriteString("{" + g.nl)
	n := 1 + g.rng.IntN(4)
	for i := 0; i < n; i++ {
		key := g.pick("region", "db_password", "tags", "a.b", "us-east-1")
		b.WriteString(indent + `"` + key + `": `)
		switch g.rng.IntN(6) {
		case 0:
			b.WriteString(`"` + g.pick("plain", "with ${var.a} tpl", `esc \" quote`, "$${lit}", "http://x/y", "# not a comment", "a /* b */ c", "<<EOF") + `"`)
		case 1:
			b.WriteString(g.pick("1", "2.5", "true", "null"))
		case 2:
			b.WriteString("[" + g.nl + indent + `  "a",` + g.nl + indent + `  "b"` + g.nl + indent + "]")
		case 3:
			if depth < 2 {
				g.jsonObject(b, depth+1)
			} else {
				b.WriteString("{}")
			}
		default:
			b.WriteString(`"Passw0rdHunter2: 1"`)
		}
		if i < n-1 {
			b.WriteByte(',')
		}
		b.WriteString(g.nl)
	}
	b.WriteString(strings.Repeat("  ", depth) + "}")
}
