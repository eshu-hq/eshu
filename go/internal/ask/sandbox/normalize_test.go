// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		mustContain    []string // keywords that MUST appear in masked (code, not hidden)
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
			// NUL and other non-whitespace control bytes are now rejected by normalize
			// to prevent token-split evasion (e.g. D\x00ELETE → tokens "D"+"ELETE").
			name:    "nul byte binary input",
			dialect: DialectSQL,
			query:   "\x00\x01SELECT",
			wantErr: true,
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

		// ── digit-leading dollar-tag bypass (CVE-class: stacked-statement) ────
		//
		// $1$ is a positional parameter in Postgres, NOT a dollar-quote opener.
		// The normalizer must keep the scanner in CODE state so that real `;`
		// separators and dangerous keywords inside the "body" are visible.
		{
			name:    "digit-tag $1$ is not a dollar-quote — DROP and stacked stmt visible",
			dialect: DialectSQL,
			// $1$ is NOT a dollar-quote delimiter; the $ chars are ordinary code
			// (positional params / stray punctuation), the `;` separates statements,
			// and DROP is visible as a real code keyword.
			query:          "SELECT a=$1; DROP TABLE t",
			statementCount: 2,
			mustContain:    []string{"DROP"}, // DROP must appear in masked (it is code)
		},
		{
			name:    "digit-tag bypass: stacked DROP must not be swallowed",
			dialect: DialectSQL,
			// Regression for the reported bypass:
			//   SELECT * FROM t WHERE a=$1 OR (1=1)$1$;DROP TABLE users;--$1$
			// With the bug, $1$ was treated as a quote opener swallowing ";DROP TABLE users;".
			// After the fix, $1 is a parameter (code), the `;` separates statements,
			// and DROP appears in masked output.
			query:          "SELECT * FROM t WHERE a=$1 OR (1=1)$1$;DROP TABLE users;--$1$",
			statementCount: 2,                // first stmt + DROP stmt; trailing --$1$ is a line comment
			mustContain:    []string{"DROP"}, // DROP must be visible, not swallowed
		},
		{
			// Real positional params with no closing $ sequence: treated as code,
			// no error, statementCount stays 1.
			name:           "real positional params $1 $2 are ordinary code",
			dialect:        DialectSQL,
			query:          "SELECT id FROM t WHERE a = $1 AND b = $2",
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
			for _, kw := range tc.mustContain {
				if !strings.Contains(strings.ToUpper(got.masked), strings.ToUpper(kw)) {
					t.Errorf("normalize masked missing %q (expected as visible code); masked=%q", kw, got.masked)
				}
			}
			if got.statementCount != tc.statementCount {
				t.Errorf("normalize statementCount = %d; want %d (masked=%q)", got.statementCount, tc.statementCount, got.masked)
			}
		})
	}
}

// TestNormalizeRejectsControlChars verifies that queries containing control
// bytes (other than the permitted whitespace bytes TAB/LF/CR) are rejected
// before any scanning begins, closing the token-split evasion vector where a
// control byte between keyword characters (e.g. D\x00ELETE) splits the token
// into two non-denylist fragments. The no-panic suite that passes NUL bytes
// remains valid — returning an err is a non-panic result.
func TestNormalizeRejectsControlChars(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		query   string
		wantErr bool // true → err must be non-empty
	}{
		// ── hostile: control bytes that split tokens ──────────────────────────
		{
			name:    "NUL byte in query",
			query:   "MATCH (n) D\x00ELETE n RETURN n",
			wantErr: true,
		},
		{
			name:    "SOH (0x01) byte in query",
			query:   "MATCH (n) D\x01ELETE n RETURN n",
			wantErr: true,
		},
		{
			name:    "BEL (0x07) byte in query",
			query:   "SELECT\x07 1",
			wantErr: true,
		},
		{
			name:    "ESC (0x1B) byte in query",
			query:   "SELECT \x1B1",
			wantErr: true,
		},
		{
			name:    "DEL (0x7F) byte in query",
			query:   "SELECT\x7f 1",
			wantErr: true,
		},
		{
			name:    "NUL-only query",
			query:   "\x00",
			wantErr: true,
		},
		{
			name:    "multiple control bytes",
			query:   "\x01\x02\x03",
			wantErr: true,
		},

		// ── permitted: normal whitespace bytes are fine ───────────────────────
		{
			name:    "normal query with spaces is fine",
			query:   "MATCH (n) RETURN n",
			wantErr: false,
		},
		{
			name:    "query with TAB (0x09) is fine",
			query:   "MATCH (n)\tRETURN n",
			wantErr: false,
		},
		{
			name:    "query with LF (0x0A) is fine",
			query:   "MATCH (n)\nRETURN n",
			wantErr: false,
		},
		{
			name:    "query with CR (0x0D) is fine",
			query:   "MATCH (n)\rRETURN n",
			wantErr: false,
		},
		{
			name:    "multi-line query with tabs and newlines is fine",
			query:   "MATCH (n)\n\tWHERE n.x = 1\r\nRETURN n",
			wantErr: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalize(DialectCypher, tc.query)
			if tc.wantErr {
				if got.err == "" {
					t.Fatalf("normalize(%q) err = %q; want non-empty (control char should be rejected)", tc.query, got.err)
				}
			} else {
				// Only check the control-char error; other errors (e.g. no RETURN) are irrelevant here.
				if got.err == "control character not permitted" {
					t.Fatalf("normalize(%q) unexpectedly rejected with control-char error; want no such error", tc.query)
				}
			}
		})
	}
}
