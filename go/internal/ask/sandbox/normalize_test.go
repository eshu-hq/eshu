package sandbox

import (
	"strings"
	"testing"
)

// TestNormalizeAdversarial exercises the full adversarial matrix: comment smuggling,
// keyword-in-string-literal, stacked-statement bypasses, dollar quoting, and edge cases.
// All cases run in parallel.
func TestNormalizeAdversarial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		dialect        Dialect
		query          string
		noKeywords     []string // keywords that must NOT appear in masked
		statementCount int
		wantErr        bool // err must be non-empty
	}{
		// ── line comment smuggling ────────────────────────────────────────────
		{
			name:           "sql line comment hides DROP",
			dialect:        DialectSQL,
			query:          "SELECT 1 -- DROP TABLE x",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},
		{
			name:           "cypher line comment hides DELETE",
			dialect:        DialectCypher,
			query:          "MATCH (n) RETURN n // DELETE n",
			noKeywords:     []string{"DELETE"},
			statementCount: 1,
		},
		{
			name:           "sql double-dash comment at start",
			dialect:        DialectSQL,
			query:          "-- DROP TABLE x\nSELECT 1",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},

		// ── block comment smuggling ───────────────────────────────────────────
		{
			name:           "block comment hides DELETE in cypher",
			dialect:        DialectCypher,
			query:          "/* DELETE */ MATCH (n) RETURN n",
			noKeywords:     []string{"DELETE"},
			statementCount: 1,
		},
		{
			name:           "block comment hides DROP in sql",
			dialect:        DialectSQL,
			query:          "SELECT /* DROP TABLE t */ 1",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},
		{
			name:           "nested-like block comment (not nested — closed at first */)",
			dialect:        DialectSQL,
			query:          "SELECT /* DROP /* nested */ 1 */ 2",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},

		// ── single-quoted string smuggling ───────────────────────────────────
		{
			name:           "keyword inside single-quoted string sql",
			dialect:        DialectSQL,
			query:          "SELECT 'DROP TABLE t' AS x",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},
		{
			name:           "keyword inside single-quoted string cypher",
			dialect:        DialectCypher,
			query:          `RETURN 'DELETE everything' AS msg`,
			noKeywords:     []string{"DELETE"},
			statementCount: 1,
		},
		{
			name:           "doubled-quote escape does not end string early",
			dialect:        DialectSQL,
			query:          "SELECT 'it''s fine DROP' AS x",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},
		{
			name:           "multiple doubled-quotes in string",
			dialect:        DialectSQL,
			query:          "SELECT 'a''b''c DROP' AS x",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},

		// ── dollar-quoted string smuggling (SQL) ─────────────────────────────
		{
			name:           "dollar-quoted $$ hides DROP",
			dialect:        DialectSQL,
			query:          "SELECT $$ DROP TABLE t $$",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},
		{
			name:           "dollar-quoted $x$ hides DROP",
			dialect:        DialectSQL,
			query:          "SELECT $x$ DROP $x$",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},
		{
			name:           "dollar-quoted with tag containing digits",
			dialect:        DialectSQL,
			query:          "SELECT $tag1$ DROP TABLE t $tag1$",
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},

		// ── stacked statement counting ────────────────────────────────────────
		{
			name:           "stacked statements counted correctly",
			dialect:        DialectSQL,
			query:          "SELECT 1; DROP TABLE t",
			statementCount: 2,
		},
		{
			name:           "three stacked statements",
			dialect:        DialectSQL,
			query:          "SELECT 1; SELECT 2; SELECT 3",
			statementCount: 3,
		},
		{
			name:           "semicolon inside string not a separator",
			dialect:        DialectSQL,
			query:          "SELECT 'a;b;c'",
			statementCount: 1,
		},
		{
			name:           "semicolon inside block comment not a separator",
			dialect:        DialectSQL,
			query:          "SELECT 1 /* ; ; ; */",
			statementCount: 1,
		},

		// ── trailing semicolon handling ───────────────────────────────────────
		{
			name:           "trailing semicolon is not a second statement",
			dialect:        DialectSQL,
			query:          "SELECT 1;",
			statementCount: 1,
		},
		{
			name:           "trailing semicolon with trailing spaces",
			dialect:        DialectSQL,
			query:          "SELECT 1;   ",
			statementCount: 1,
		},
		{
			name:           "trailing semicolon with trailing newline",
			dialect:        DialectSQL,
			query:          "SELECT 1;\n",
			statementCount: 1,
		},
		{
			name:           "two real statements then trailing semicolon",
			dialect:        DialectSQL,
			query:          "SELECT 1; SELECT 2;",
			statementCount: 2,
		},

		// ── error cases ───────────────────────────────────────────────────────
		{
			name:    "unterminated single-quoted string",
			dialect: DialectSQL,
			query:   "SELECT 'oops",
			wantErr: true,
		},
		{
			name:    "unterminated block comment",
			dialect: DialectSQL,
			query:   "SELECT 1 /* oops",
			wantErr: true,
		},
		{
			name:    "unterminated dollar quote",
			dialect: DialectSQL,
			query:   "SELECT $$ oops",
			wantErr: true,
		},
		{
			name:    "unterminated dollar quote named tag",
			dialect: DialectSQL,
			query:   "SELECT $tag$ oops",
			wantErr: true,
		},
		{
			name:    "unterminated cypher backtick identifier",
			dialect: DialectCypher,
			query:   "MATCH (`node",
			wantErr: true,
		},

		// ── edge / hostile inputs ─────────────────────────────────────────────
		{
			name:           "empty string",
			dialect:        DialectSQL,
			query:          "",
			statementCount: 0,
		},
		{
			name:           "whitespace only",
			dialect:        DialectSQL,
			query:          "   \t\n  ",
			statementCount: 0,
		},
		{
			name:           "nul byte binary input",
			dialect:        DialectSQL,
			query:          "\x00\x01SELECT",
			statementCount: 1,
		},
		{
			name:           "only a semicolon",
			dialect:        DialectSQL,
			query:          ";",
			statementCount: 0,
		},
		{
			name:           "multiple semicolons only",
			dialect:        DialectSQL,
			query:          ";;;",
			statementCount: 0,
		},

		// ── backtick identifier masking (Cypher) ─────────────────────────────
		{
			name:           "cypher backtick identifier hides keyword",
			dialect:        DialectCypher,
			query:          "MATCH (`DELETE`) RETURN n",
			noKeywords:     []string{"DELETE"},
			statementCount: 1,
		},

		// ── double-quoted identifier masking ─────────────────────────────────
		{
			name:           "sql double-quoted identifier hides keyword",
			dialect:        DialectSQL,
			query:          `SELECT "DROP" AS x`,
			noKeywords:     []string{"DROP"},
			statementCount: 1,
		},

		// ── cypher does not apply SQL dollar quoting ─────────────────────────
		{
			name:    "cypher dollar-quote is NOT a string delimiter",
			dialect: DialectCypher,
			// In Cypher, $$ is NOT a string delimiter; it is a parameter prefix.
			// The content is code. statementCount=1, no error.
			query:          "MATCH (n {id: $id}) RETURN n",
			statementCount: 1,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalize(tc.dialect, tc.query)

			if tc.wantErr {
				if got.err == "" {
					t.Fatalf("normalize(%q, %q) err = %q; want non-empty", tc.dialect, tc.query, got.err)
				}
				return // error path — no further checks required
			}
			if got.err != "" {
				t.Fatalf("normalize(%q, %q) unexpected err = %q", tc.dialect, tc.query, got.err)
			}
			for _, kw := range tc.noKeywords {
				if strings.Contains(strings.ToUpper(got.masked), strings.ToUpper(kw)) {
					t.Errorf("normalize masked still contains %q; masked=%q", kw, got.masked)
				}
			}
			if got.statementCount != tc.statementCount {
				t.Errorf("normalize statementCount = %d; want %d (masked=%q)", got.statementCount, tc.statementCount, got.masked)
			}
		})
	}
}

// TestNormalizeNoPanic confirms that hostile inputs never cause a panic regardless of content.
func TestNormalizeNoPanic(t *testing.T) {
	t.Parallel()

	hostile := []string{
		"",
		"   ",
		"\x00",
		"\x00\x01\x02\x03",
		"SELECT '\x00",
		"SELECT /*\x00",
		"'",
		"/*",
		"$$",
		"$tag$",
		"``",
		`"`,
		strings.Repeat("'", 10000),
		strings.Repeat("/*", 5000),
		strings.Repeat("$", 5000),
		"SELECT '" + strings.Repeat("a", 65536) + "'",
		"\xff\xfe",                      // invalid UTF-8
		"SELECT 1\x00; DROP TABLE t",    // NUL in code
		"'a''b''c''d" + "'",             // many doubled quotes, then terminated
		"$tag1$body$tag2$mismatch here", // tag mismatch — unterminated
	}

	for _, q := range hostile {
		q := q
		for _, d := range []Dialect{DialectSQL, DialectCypher} {
			d := d
			t.Run(string(d)+"/"+truncateLabel(q), func(t *testing.T) {
				t.Parallel()
				// The only guarantee is no panic.
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("normalize panicked: %v", r)
					}
				}()
				_ = normalize(d, q)
			})
		}
	}
}

// truncateLabel shortens a string to a safe test-name length.
func truncateLabel(s string) string {
	const maxLen = 32
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
