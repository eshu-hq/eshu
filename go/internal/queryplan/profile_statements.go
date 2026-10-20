// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"strings"
)

// profileStatementSeparator joins the statements of a multi-statement
// profile family. It is the exact separator the production capture uses
// when one pinned read emits one statement per depth, so the live profile
// gate splits precisely what the capture joined.
const profileStatementSeparator = "\n;\n"

// SplitProfileStatements splits one manifest entry's Cypher into the
// individual statements a backend executes. Neo4j PROFILE accepts exactly
// one statement per query, so the live profile gate profiles each part
// separately with the entry's operator requirements. Single-statement
// entries yield their text unchanged; blank input yields no parts.
func SplitProfileStatements(cypher string) []string {
	var parts []string
	for _, part := range strings.Split(cypher, profileStatementSeparator) {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}
