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
