package sandbox

import (
	"strings"
	"testing"
)

func TestValidateCypher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		wantAllowed bool
		// reasonContains is checked only when wantAllowed == false; it must appear
		// in the Reason string (case-insensitive). Also verifies that the full
		// query body is NOT echoed verbatim in the reason (bounded reason contract).
		reasonContains string
	}{
		// ── ALLOW cases ──────────────────────────────────────────────────────────
		{
			name:        "simple MATCH RETURN with LIMIT",
			query:       "MATCH (n:Service) RETURN n LIMIT 10",
			wantAllowed: true,
		},
		{
			// Key whole-word test: CALLS is a relationship type token, NOT the CALL
			// clause keyword. The tokenizer must NOT split on the S suffix.
			name:        "relationship type CALLS must not trip CALL denylist",
			query:       "MATCH (a)-[:CALLS]->(b) RETURN a, b",
			wantAllowed: true,
		},
		{
			name:        "WHERE with string literal RETURN",
			query:       "MATCH (n) WHERE n.name = 'x' RETURN n.name",
			wantAllowed: true,
		},
		{
			// DELETE appears only inside a // comment; normalize masks it away.
			name:        "DELETE in line comment is masked away — allowed",
			query:       "MATCH (n) RETURN n // DELETE n",
			wantAllowed: true,
		},

		// ── DENY cases ───────────────────────────────────────────────────────────
		{
			name:           "bare DELETE",
			query:          "MATCH (n) DELETE n",
			wantAllowed:    false,
			reasonContains: "DELETE",
		},
		{
			name:           "DETACH DELETE",
			query:          "MATCH (n) DETACH DELETE n",
			wantAllowed:    false,
			reasonContains: "DETACH",
		},
		{
			name:           "MERGE with RETURN",
			query:          "MERGE (n:X) RETURN n",
			wantAllowed:    false,
			reasonContains: "MERGE",
		},
		{
			name:           "CREATE with RETURN",
			query:          "CREATE (n) RETURN n",
			wantAllowed:    false,
			reasonContains: "CREATE",
		},
		{
			name:           "SET property",
			query:          "MATCH (n) SET n.x = 1 RETURN n",
			wantAllowed:    false,
			reasonContains: "SET",
		},
		{
			name:           "REMOVE property",
			query:          "MATCH (n) REMOVE n.x RETURN n",
			wantAllowed:    false,
			reasonContains: "REMOVE",
		},
		{
			name:           "CALL procedure",
			query:          "CALL apoc.x() YIELD v RETURN v",
			wantAllowed:    false,
			reasonContains: "CALL",
		},
		{
			// Stacked statements: statementCount == 2 → multi-statement deny.
			name:           "stacked statements via semicolon",
			query:          "MATCH (n) RETURN n; DELETE n",
			wantAllowed:    false,
			reasonContains: "multiple statements",
		},
		{
			name:           "no RETURN clause",
			query:          "MATCH (n) WHERE n.x = 1",
			wantAllowed:    false,
			reasonContains: "return",
		},
		{
			name:           "SELECT — not a Cypher read query",
			query:          "SELECT 1",
			wantAllowed:    false,
			reasonContains: "return",
		},
		{
			name:           "unterminated string literal",
			query:          "MATCH (n) WHERE n.x = 'oops RETURN n",
			wantAllowed:    false,
			reasonContains: "rejected",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := validateCypher(tc.query)
			if got.Allowed != tc.wantAllowed {
				t.Fatalf("validateCypher(%q) Allowed=%v; want %v (Reason=%q)",
					tc.query, got.Allowed, tc.wantAllowed, got.Reason)
			}
			if tc.wantAllowed {
				if got.Reason != "" {
					t.Errorf("allowed decision must have empty Reason; got %q", got.Reason)
				}
				return
			}
			// Denied path: verify bounded reason.
			if got.Reason == "" {
				t.Errorf("denied decision must have non-empty Reason")
			}
			if tc.reasonContains != "" &&
				!strings.Contains(strings.ToLower(got.Reason), strings.ToLower(tc.reasonContains)) {
				t.Errorf("Reason %q does not contain %q", got.Reason, tc.reasonContains)
			}
			// Bounded-reason contract: the full query text must NOT appear verbatim in the reason.
			// Queries longer than 10 chars are a meaningful test (short ones may collide with
			// keyword names in the reason).
			if len(tc.query) > 10 && strings.Contains(got.Reason, tc.query) {
				t.Errorf("Reason echoes full query — violates bounded-reason contract: %q", got.Reason)
			}
		})
	}
}
