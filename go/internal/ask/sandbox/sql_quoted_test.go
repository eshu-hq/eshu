package sandbox

import (
	"strings"
	"testing"
)

// TestDoubleQuotedIdentifiers exercises the doubleQuotedIdentifiers helper
// directly: correct extraction of identifier content, "" escape handling, and
// no panic on unterminated identifiers.
func TestDoubleQuotedIdentifiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single simple identifier",
			raw:  `SELECT "pg_sleep" FROM t`,
			want: []string{"pg_sleep"},
		},
		{
			name: "multiple identifiers",
			raw:  `SELECT "user_id", "name" FROM t`,
			want: []string{"user_id", "name"},
		},
		{
			name: "double-quote escape inside identifier",
			raw:  `SELECT "a""b" FROM t`,
			want: []string{`a"b`},
		},
		{
			name: "schema-qualified quoted identifier",
			raw:  `SELECT pg_catalog."pg_advisory_lock"(1)`,
			want: []string{"pg_advisory_lock"},
		},
		{
			name: "no quoted identifiers",
			raw:  "SELECT id FROM t",
			want: nil,
		},
		{
			name: "unterminated identifier — no panic, partial content returned",
			raw:  `SELECT "oops`,
			want: []string{"oops"},
		},
		{
			name: "empty quoted identifier",
			raw:  `SELECT "" FROM t`,
			want: []string{""},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := doubleQuotedIdentifiers(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("doubleQuotedIdentifiers(%q) = %v; want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("doubleQuotedIdentifiers(%q)[%d] = %q; want %q", tc.raw, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestValidateSQLQuotedIdentifierBypass is the adversarial regression suite for
// P1 #2: double-quoted identifier names that match denylist keywords must be
// denied even though the normalizer masks their content.
func TestValidateSQLQuotedIdentifierBypass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		query          string
		wantAllowed    bool
		reasonContains string
	}{
		// ── DENY: denylist keyword smuggled as double-quoted identifier ────────
		{
			// `"pg_sleep"(10)` is callable; Postgres resolves the quoted name to
			// the function pg_sleep.  normalizer masks it → without the fix the
			// token scan never sees PG_SLEEP and the query is allowed.
			name:           "quoted pg_sleep function call denied",
			query:          `SELECT "pg_sleep"(10)`,
			wantAllowed:    false,
			reasonContains: "PG_SLEEP",
		},
		{
			// Schema-qualified: pg_catalog."pg_advisory_lock"(1)
			name:           "schema-qualified quoted pg_advisory_lock denied",
			query:          `SELECT pg_catalog."pg_advisory_lock"(1)`,
			wantAllowed:    false,
			reasonContains: "PG_ADVISORY_LOCK",
		},
		{
			// DELETE is a DML denylist word used as a quoted identifier.
			name:           "quoted DELETE identifier denied",
			query:          `SELECT "DELETE" FROM t`,
			wantAllowed:    false,
			reasonContains: "DELETE",
		},
		{
			// DROP as a quoted identifier.
			name:           "quoted DROP identifier denied",
			query:          `SELECT "DROP" AS x`,
			wantAllowed:    false,
			reasonContains: "DROP",
		},
		{
			// INSERT as a quoted identifier name.
			name:           "quoted INSERT identifier denied",
			query:          `SELECT "INSERT" FROM t`,
			wantAllowed:    false,
			reasonContains: "INSERT",
		},

		// ── ALLOW: normal quoted identifiers that are NOT denylist words ──────
		{
			// user_id and name are regular column identifiers; must not be denied.
			name:        "normal quoted column identifiers allowed",
			query:       `SELECT "user_id", "name" FROM t`,
			wantAllowed: true,
		},
		{
			// Unquoted identifiers are unaffected by the double-quote check.
			name:        "plain unquoted query unaffected",
			query:       "SELECT id FROM t",
			wantAllowed: true,
		},
		{
			// "" escape case: content is `a"b`, which is not a denylist word.
			// Must not panic and must be allowed.
			name:        "double-quote escape in identifier not a denylist word — allowed",
			query:       `SELECT "a""b" FROM t`,
			wantAllowed: true,
		},
		{
			// currval is NOT in the denylist (read-only); quoting it must not
			// accidentally deny.
			name:        "quoted currval (read-only) allowed",
			query:       `SELECT "currval"('my_seq')`,
			wantAllowed: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := validateSQL(tc.query)
			if got.Allowed != tc.wantAllowed {
				t.Fatalf("validateSQL(%q): Allowed=%v, want %v (Reason=%q)",
					tc.query, got.Allowed, tc.wantAllowed, got.Reason)
			}
			if !tc.wantAllowed {
				if got.Reason == "" {
					t.Errorf("validateSQL(%q): denied but Reason is empty", tc.query)
				}
				if tc.reasonContains != "" &&
					!strings.Contains(strings.ToUpper(got.Reason), strings.ToUpper(tc.reasonContains)) {
					t.Errorf("validateSQL(%q): Reason=%q, want it to contain %q",
						tc.query, got.Reason, tc.reasonContains)
				}
				// Bounded reason contract: must not echo the query body.
				if len(got.Reason) > 120 {
					t.Errorf("validateSQL(%q): Reason too long (%d bytes): %q",
						tc.query, len(got.Reason), got.Reason)
				}
				if len(tc.query) > 4 && strings.Contains(got.Reason, tc.query) {
					t.Errorf("validateSQL(%q): Reason echoes query body: %q", tc.query, got.Reason)
				}
			} else {
				if got.Reason != "" {
					t.Errorf("validateSQL(%q): Allowed=true but Reason non-empty: %q",
						tc.query, got.Reason)
				}
			}
		})
	}
}
