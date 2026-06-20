package sandbox

import (
	"strings"
	"testing"
)

// TestNormalizeCRLineComment is the regression suite for the CR-as-newline
// bypass (P1 #1). PostgreSQL treats bare CR (\r) as a newline, so a line
// comment terminated only by LF would allow an adversary to hide code:
//
//	SELECT 1 --\r FROM pg_sleep(10)
//
// After the fix both \r and \n terminate the line-comment state, so the bytes
// after the CR are visible as code in the masked output and are correctly
// denied by the downstream validator.
func TestNormalizeCRLineComment(t *testing.T) {
	t.Parallel()

	type tc struct {
		name           string
		dialect        Dialect
		query          string
		mustContain    []string // tokens that MUST appear in masked (visible code)
		noKeywords     []string // tokens that must NOT appear in masked (hidden)
		statementCount int
	}

	cases := []tc{
		// ── SQL -- comment terminated by bare CR ─────────────────────────────
		{
			name:    "bare CR ends SQL -- comment: PG_SLEEP and FROM visible",
			dialect: DialectSQL,
			// Without the fix the scanner stays in line-comment state through
			// \r and swallows "FROM pg_sleep(10)" as comment content, so those
			// tokens are masked and the downstream validate passes.
			// After the fix \r terminates the comment; " FROM pg_sleep(10)" is
			// code and both tokens appear in masked.
			query:          "SELECT 1 --\r FROM pg_sleep(10)",
			mustContain:    []string{"FROM", "PG_SLEEP"},
			statementCount: 1,
		},
		{
			name:           "bare CR ends SQL -- comment with denylist word: DELETE visible",
			dialect:        DialectSQL,
			query:          "SELECT 1 -- hide\r DELETE FROM t",
			mustContain:    []string{"DELETE", "FROM"},
			noKeywords:     []string{"hide"},
			statementCount: 1,
		},
		// ── Cypher // comment terminated by bare CR ──────────────────────────
		{
			name:    "bare CR ends Cypher // comment: DELETE visible as code",
			dialect: DialectCypher,
			// Cypher // line comment; CR must end it just like LF.
			query:          "MATCH (n) RETURN n //evil\r DELETE n",
			mustContain:    []string{"DELETE"},
			noKeywords:     []string{"evil"},
			statementCount: 1,
		},
		// ── Cypher -- comment terminated by bare CR ──────────────────────────
		{
			name:           "bare CR ends Cypher -- comment: SET visible as code",
			dialect:        DialectCypher,
			query:          "MATCH (n) RETURN n -- hide\r SET n.x=1",
			mustContain:    []string{"SET"},
			noKeywords:     []string{"hide"},
			statementCount: 1,
		},
		// ── CRLF: CR ends comment, LF is ordinary whitespace ─────────────────
		// These verify that the fix does not break the common CRLF line ending.
		// The CR ends the line-comment state; the subsequent LF is processed
		// as ordinary whitespace in code state (not masked).
		{
			name:           "CRLF SQL comment: comment content masked, code after visible",
			dialect:        DialectSQL,
			query:          "SELECT 1 --comment\r\nFROM t",
			noKeywords:     []string{"comment"},
			mustContain:    []string{"FROM"},
			statementCount: 1,
		},
		{
			name:           "CRLF Cypher // comment: comment content masked, RETURN visible",
			dialect:        DialectCypher,
			query:          "MATCH (n) //comment\r\nRETURN n",
			noKeywords:     []string{"comment"},
			mustContain:    []string{"RETURN"},
			statementCount: 1,
		},
		{
			name:           "CRLF SQL comment hides DROP: DROP not in masked, SELECT visible",
			dialect:        DialectSQL,
			query:          "SELECT 1 -- DROP TABLE x\r\nFROM t",
			noKeywords:     []string{"DROP"},
			mustContain:    []string{"SELECT", "FROM"},
			statementCount: 1,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := normalize(c.dialect, c.query)
			if got.err != "" {
				t.Fatalf("normalize(%q) unexpected err = %q", c.query, got.err)
			}
			for _, kw := range c.mustContain {
				if !strings.Contains(strings.ToUpper(got.masked), strings.ToUpper(kw)) {
					t.Errorf("masked missing %q (expected as visible code); masked=%q", kw, got.masked)
				}
			}
			for _, kw := range c.noKeywords {
				if strings.Contains(strings.ToUpper(got.masked), strings.ToUpper(kw)) {
					t.Errorf("masked still contains %q (should be hidden in comment); masked=%q", kw, got.masked)
				}
			}
			if got.statementCount != c.statementCount {
				t.Errorf("statementCount = %d; want %d (masked=%q)", got.statementCount, c.statementCount, got.masked)
			}
		})
	}
}

// TestNormalizeCRLineCommentValidateIntegration verifies that the CR bypass
// is closed end-to-end at the validateSQL level, not just in normalize.
// This is the adversarial integration test: the CR-terminated comment payload
// MUST be denied by validateSQL because pg_sleep appears as a code token after
// the fix.
func TestNormalizeCRLineCommentValidateIntegration(t *testing.T) {
	t.Parallel()

	t.Run("CR bypass payload denied by validateSQL", func(t *testing.T) {
		t.Parallel()
		// Before the fix: normalize swallowed "FROM pg_sleep(10)" as comment →
		// only "SELECT 1" was visible → validateSQL allowed it.
		// After the fix: CR ends the comment → pg_sleep is a code token →
		// validateSQL denies with PG_SLEEP in the reason.
		query := "SELECT 1 --\r FROM pg_sleep(10)"
		d := validateSQL(query)
		if d.Allowed {
			t.Fatalf("validateSQL(%q): Allowed=true, want false (pg_sleep must be denied)", query)
		}
		if !strings.Contains(strings.ToUpper(d.Reason), "PG_SLEEP") {
			t.Errorf("validateSQL(%q): Reason = %q, want it to contain PG_SLEEP", query, d.Reason)
		}
	})

	t.Run("CRLF payload still allowed when safe", func(t *testing.T) {
		t.Parallel()
		// A safe CRLF-terminated comment followed by a FROM clause must still
		// be allowed after the fix.
		query := "SELECT id --safe comment\r\nFROM t LIMIT 1"
		d := validateSQL(query)
		if !d.Allowed {
			t.Fatalf("validateSQL(%q): Allowed=false, want true; Reason=%q", query, d.Reason)
		}
	})
}
