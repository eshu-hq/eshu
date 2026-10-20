// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"testing"
)

func TestSplitProfileStatementsSingleStatement(t *testing.T) {
	t.Parallel()

	cypher := "MATCH (n:Workload {id: $target_id}) RETURN n LIMIT 10"
	got := SplitProfileStatements(cypher)
	if len(got) != 1 || got[0] != cypher {
		t.Fatalf("SplitProfileStatements() = %q, want the single statement unchanged", got)
	}
}

func TestSplitProfileStatementsFamilyJoin(t *testing.T) {
	t.Parallel()

	parts := []string{
		"MATCH path = (start:Repository {id: $target_id})-[*1..1]->(impacted) RETURN impacted.id as id",
		"MATCH path = (start:Repository {id: $target_id})-[*2..2]->(impacted) RETURN impacted.id as id",
		"MATCH path = (start:Repository {id: $target_id})-[*3..3]->(impacted) RETURN impacted.id as id",
	}
	joined := parts[0] + "\n;\n" + parts[1] + "\n;\n" + parts[2]
	got := SplitProfileStatements(joined)
	if len(got) != len(parts) {
		t.Fatalf("SplitProfileStatements() split %d parts, want %d", len(got), len(parts))
	}
	for i := range parts {
		if got[i] != parts[i] {
			t.Fatalf("part %d = %q, want %q", i, got[i], parts[i])
		}
	}
}

func TestSplitProfileStatementsBlank(t *testing.T) {
	t.Parallel()

	for _, cypher := range []string{"", "   ", "\n;\n", "MATCH (n) RETURN n\n;\n"} {
		got := SplitProfileStatements(cypher)
		for _, part := range got {
			if part == "" {
				t.Fatalf("SplitProfileStatements(%q) yielded an empty part: %q", cypher, got)
			}
		}
	}
	if got := SplitProfileStatements(""); len(got) != 0 {
		t.Fatalf("SplitProfileStatements(\"\") = %q, want no parts", got)
	}
}
