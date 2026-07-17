// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestNornicDBRelationshipEntityLabelCypherWrapsUnionInCall guards the #5287
// fix: the per-label identity resolver must wrap its UNION arms in CALL{} with a
// plain outer RETURN. A top-level UNION is mis-parsed on the pinned build (the
// arms' columns are mangled into one row); CALL{…UNION…} executes correctly.
func TestNornicDBRelationshipEntityLabelCypherWrapsUnionInCall(t *testing.T) {
	t.Parallel()

	for _, scoped := range []bool{false, true} {
		q := nornicDBRelationshipEntityLabelCypher("uid", scoped)
		if !strings.HasPrefix(strings.TrimSpace(q), "CALL {") {
			t.Fatalf("identity query (scoped=%v) must wrap the UNION in CALL{}: %s", scoped, q)
		}
		// The outer query after the closing brace must be a plain RETURN — no
		// bare top-level UNION survives outside the CALL block.
		closeIdx := strings.LastIndex(q, "}")
		if closeIdx < 0 {
			t.Fatalf("identity query (scoped=%v) lost its CALL{} block: %s", scoped, q)
		}
		tail := q[closeIdx+1:]
		if strings.Contains(tail, "UNION") || !strings.Contains(tail, "RETURN") {
			t.Fatalf("identity query (scoped=%v) must have a plain outer RETURN after CALL{} (no top-level UNION), tail: %s", scoped, tail)
		}
	}
}
