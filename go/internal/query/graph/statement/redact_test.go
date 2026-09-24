// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statement

import (
	"strings"
	"testing"
)

// TestRedact proves every literal token class is replaced by Placeholder while
// identifiers, labels, relationship types, parameters, property keys, and
// backtick-quoted identifiers survive, and that whitespace is collapsed.
func TestRedact(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", " \n\t ", ""},
		{"whitespace collapsed", "MATCH (n:Repo {id: $id})\nRETURN   n", "MATCH (n:Repo {id: $id}) RETURN n"},
		{
			"single and double quoted strings and limit",
			`MATCH (r:Repository) WHERE r.name = 'acme-private-repo' AND r.token = "tok-SECRET-123" RETURN r LIMIT 1`,
			`MATCH (r:Repository) WHERE r.name = <REDACTED> AND r.token = <REDACTED> RETURN r LIMIT <REDACTED>`,
		},
		{"integer", "RETURN 42", "RETURN <REDACTED>"},
		{"float", "RETURN 2.5, .5, 0.05", "RETURN <REDACTED>, <REDACTED>, <REDACTED>"},
		{"exponent", "RETURN 1e10, 1.5E-3, 2e+4", "RETURN <REDACTED>, <REDACTED>, <REDACTED>"},
		{"suffix", "RETURN 3f, 4.5d", "RETURN <REDACTED>, <REDACTED>"},
		{"hex and octal", "RETURN 0x1F, 0o17, 007", "RETURN <REDACTED>, <REDACTED>, <REDACTED>"},
		{"negative literal", "WHERE n.x = -5", "WHERE n.x = <REDACTED>"},
		{"spaced minus stays an operator", "RETURN a - 1", "RETURN a - <REDACTED>"},
		{"variable length range", "MATCH (a)-[:INHERITS*1..5]->(b)", "MATCH (a)-[:INHERITS*<REDACTED>..<REDACTED>]->(b)"},
		{"open low range", "MATCH (a)-[:R*..5]->(b)", "MATCH (a)-[:R*..<REDACTED>]->(b)"},
		{"open high range", "MATCH (a)-[:R*2..]->(b)", "MATCH (a)-[:R*<REDACTED>..]->(b)"},
		{"digits in identifiers", "MATCH (n1:Label2)-[r3:REL_4]->(m) RETURN n1", "MATCH (n1:Label2)-[r3:REL_4]->(m) RETURN n1"},
		{"parameters", "WHERE n.a = $param1 AND n.b = $1", "WHERE n.a = $param1 AND n.b = $1"},
		{"property chain", "RETURN a.b.c", "RETURN a.b.c"},
		{"backtick identifier", "MATCH (n:`My Label 1`) RETURN n", "MATCH (n:`My Label 1`) RETURN n"},
		{"quote inside backticks is not a string", "MATCH (n:`it's`) WHERE n.x = 5 RETURN n", "MATCH (n:`it's`) WHERE n.x = <REDACTED> RETURN n"},
		{"doubled backtick", "MATCH (n:`a``b`) RETURN n", "MATCH (n:`a``b`) RETURN n"},
		{"unterminated backtick redacts rest", "MATCH (n:`abc 'secret'", "MATCH (n:<REDACTED>"},
		{"backslash escaped quote", `WHERE n.x = 'it\'s secret' AND y`, "WHERE n.x = <REDACTED> AND y"},
		{"doubled quote", "WHERE n.x = 'it''s secret' AND y", "WHERE n.x = <REDACTED> AND y"},
		{"double quoted escaped", `WHERE n.x = "say \"hi\"" AND y`, "WHERE n.x = <REDACTED> AND y"},
		{"empty strings", `WHERE n.x = '' AND n.y = "" RETURN n`, "WHERE n.x = <REDACTED> AND n.y = <REDACTED> RETURN n"},
		{"comment markers inside a string", "WHERE n.u = 'http://x/*y*/' RETURN 1", "WHERE n.u = <REDACTED> RETURN <REDACTED>"},
		{"unterminated single quote redacts rest", "WHERE n.name = 'abc secret RETURN n", "WHERE n.name = <REDACTED>"},
		{"unterminated double quote redacts rest", `WHERE n.name = "abc secret RETURN n`, "WHERE n.name = <REDACTED>"},
		{"trailing backslash in string", `WHERE n.x = 'abc\`, "WHERE n.x = <REDACTED>"},
		{"line comment dropped", "MATCH (n) // secret note 42\nRETURN n", "MATCH (n) RETURN n"},
		{"block comment dropped", "MATCH /* secret 'x' */ (n) RETURN n", "MATCH (n) RETURN n"},
		{"unterminated block comment dropped", "MATCH (n) /* secret", "MATCH (n)"},
		{"unicode identifiers and strings", "MATCH (ñ:Café) WHERE ñ.name = 'Zoë 日本語' RETURN ñ", "MATCH (ñ:Café) WHERE ñ.name = <REDACTED> RETURN ñ"},
		{"unicode string only", `RETURN "日本語"`, "RETURN <REDACTED>"},
		{"arrow is not a negative literal", "MATCH (a)-->(b), (a)<-[:R]-(b) RETURN a", "MATCH (a)-->(b), (a)<-[:R]-(b) RETURN a"},
		{"digit separators decimal", "WHERE n.cc = 4111_1111_1111_1111", "WHERE n.cc = <REDACTED>"},
		{"digit separators ssn", "WHERE n.ssn = 123_45_6789 AND y", "WHERE n.ssn = <REDACTED> AND y"},
		{"digit separators hex", "RETURN 0x1F_FF, 1", "RETURN <REDACTED>, <REDACTED>"},
		{"digit separators octal", "RETURN 0o1_7, 1", "RETURN <REDACTED>, <REDACTED>"},
		{"digit separators fraction", "RETURN 1_000.5_5, 1", "RETURN <REDACTED>, <REDACTED>"},
		{"digit separators exponent", "RETURN 1e1_0, 1", "RETURN <REDACTED>, <REDACTED>"},
		{"signed exponent with separator", "RETURN 1e-1_0 AS x", "RETURN <REDACTED> AS x"},
		{"trailing letters belong to the number", "RETURN 1abc AS x, 12e AS y", "RETURN <REDACTED> AS x, <REDACTED> AS y"},
		{"no-break space before a number", "WHERE n.pin =\u00a01234", "WHERE n.pin = <REDACTED>"},
		{"ideographic space before a number", "WHERE n.pin =\u30001234 AND y", "WHERE n.pin = <REDACTED> AND y"},
		{"en space before a number", "WHERE n.pin =\u20031234", "WHERE n.pin = <REDACTED>"},
		{"unicode space before a string", "WHERE n.t =\u00a0'tok-SECRET' AND y", "WHERE n.t = <REDACTED> AND y"},
		{"unicode space between tokens collapses", "MATCH\u00a0\u3000(n)\u2028RETURN\u205fn", "MATCH (n) RETURN n"},
		{"ascii separator controls are whitespace", "MATCH\x1f(n)\x1cRETURN n", "MATCH (n) RETURN n"},
		{"unicode space after an identifier", "MATCH (n) WHERE n.a\u00a0= 5", "MATCH (n) WHERE n.a = <REDACTED>"},
		{"list literal", "WHERE n.id IN [1, 2, 'x']", "WHERE n.id IN [<REDACTED>, <REDACTED>, <REDACTED>]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestRedactLeaksNoLiteralValue proves no literal-derived byte survives, for a
// statement that mixes every literal class and a comment.
func TestRedactLeaksNoLiteralValue(t *testing.T) {
	const in = "MATCH (r:Repository) /* note-SECRET */ WHERE r.name = 'acme-private-repo' " +
		`AND r.token = "tok-SECRET-123" AND r.n = 987654 AND r.f = 3.14159 // tail-SECRET` + "\nRETURN r"
	got := Redact(in)
	for _, leaked := range []string{"acme", "private", "tok-", "SECRET", "987654", "3.14159"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("Redact() = %q leaked %q", got, leaked)
		}
	}
}

// FuzzRedact proves the scanner never panics on arbitrary text and returns
// trimmed, single-spaced output, so the fingerprint stays stable. Text inside a
// backtick identifier is kept verbatim, so inputs with a backtick only get the
// no-panic and trim checks.
func FuzzRedact(f *testing.F) {
	for _, seed := range []string{"", "'", `"\`, "`", "/*", "//", "-", ".", "1e", "0x", "0o", "$", "a''b", "\x00\xff'", "MATCH (n) RETURN 1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := Redact(in)
		if got != strings.Trim(got, " \t\n\v\f\r") || (!strings.Contains(in, "`") && strings.Contains(got, "  ")) {
			t.Fatalf("Redact(%q) = %q, want trimmed single-spaced text", in, got)
		}
	})
}

// FuzzRedactLeaksNoPlantedSecret plants fuzzer-chosen secret tokens in literal
// positions -- every numeric form with digit separators, single- and
// double-quoted strings with escapes, unterminated strings, and comments --
// behind ASCII and unicode whitespace, and asserts none survives. It exists so
// a leak class that only shows up under some input shape is found by the
// fuzzer, not by a reviewer.
func FuzzRedactLeaksNoPlantedSecret(f *testing.F) {
	f.Add("4111", "1111", uint8(0), uint8(0))
	f.Add("ab12", "cd34", uint8(3), uint8(1))
	f.Add("dead", "beef", uint8(6), uint8(2))
	f.Add("0f0f", "f0f0", uint8(9), uint8(3))
	f.Fuzz(func(t *testing.T, rawFirst, rawSecond string, kind, space uint8) {
		first, second := hexOnly(rawFirst), hexOnly(rawSecond)
		if len(first) < 4 || len(second) < 4 {
			t.Skip()
		}
		spaces := []string{" ", "\u00a0", "\u3000", "\t", "\u2003", "\x1f", "\u2028", "\u202f"}
		gap := spaces[int(space)%len(spaces)]
		literals := []string{
			"7" + first + "_" + second,
			"0x" + first + "_" + second,
			"0o" + first + "_" + second,
			"1_" + first + ".5_" + second,
			"1e1_" + first + second,
			"-7" + first + "_" + second,
			"'" + first + "\\'" + second + "'",
			"'" + first + "''" + second + "'",
			`"` + first + `\"` + second + `"`,
			`"` + first + `""` + second + `"`,
			"'" + first + " " + second,
			`"` + first + " " + second,
		}
		statements := []string{"MATCH (p) WHERE p.q =" + gap + literals[int(kind)%len(literals)]}
		if int(kind)%len(literals) < 10 {
			statements[0] += gap + "RETURN p"
		}
		statements = append(statements,
			"MATCH (p) /* "+first+" "+second+" */"+gap+"RETURN p",
			"MATCH (p) // "+first+" "+second+"\nRETURN p",
		)
		for _, statement := range statements {
			got := Redact(statement)
			if strings.Contains(got, first) || strings.Contains(got, second) {
				t.Fatalf("Redact(%q) = %q, leaked a planted secret", statement, got)
			}
		}
	})
}

// hexOnly keeps only lowercase hex digits, so a planted secret can never equal
// text the scanner legitimately keeps (keywords, identifiers, the placeholder).
func hexOnly(in string) string {
	var out []byte
	for i := 0; i < len(in) && len(out) < 16; i++ {
		if c := in[i]; (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			out = append(out, c)
		}
	}
	return string(out)
}
