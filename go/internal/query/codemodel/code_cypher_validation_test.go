// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
	"testing"
)

// TestValidateReadOnlyCypherAcceptsReads proves plain read-only
// statements pass, including identifiers that merely contain a
// keyword as a substring.
func TestValidateReadOnlyCypherAcceptsReads(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		"MATCH (n:Function) WHERE n.name = $name RETURN n",
		"match (e) return e",
		"// created_at is informational\nMATCH (n) RETURN n.created_at",
		"RETURN 1 AS value",
	} {
		if err := ValidateReadOnlyCypher(query); err != nil {
			t.Errorf("read-only query rejected: %q: %v", query, err)
		}
	}
}

// TestValidateReadOnlyCypherRejectsMutations proves every mutation
// keyword and the LOAD CSV phrase fail closed, case-insensitively,
// while overlong queries fail on length.
func TestValidateReadOnlyCypherRejectsMutations(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		"MATCH (n) CREATE (m) RETURN m",
		"match (n) merge (n) return n",
		"MATCH (n) DETACH DELETE n",
		"MATCH (n) SET n.flag = true RETURN n",
		"MATCH (n) REMOVE n.flag RETURN n",
		"MATCH (n) DROP INDEX x",
		"CALL db.indexes()",
		"FOREACH (x IN [1] | CREATE (n))",
		"LOAD CSV FROM 'file:///x.csv' AS row RETURN row",
		"// DELETE is commented but still blocked\nMATCH (n) RETURN n",
	} {
		if err := ValidateReadOnlyCypher(query); err == nil {
			t.Errorf("mutating query accepted: %q", query)
		}
	}

	if err := ValidateReadOnlyCypher(strings.Repeat("x", cypherMaxQueryLength+1)); err == nil {
		t.Error("overlong query must fail validation")
	}
	if err := ValidateReadOnlyCypher(strings.Repeat("x", cypherMaxQueryLength)); err != nil {
		t.Errorf("boundary-length query must pass: %v", err)
	}
}
