// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

// TestSchemaStatementsContainsSecurityGroupRuleSchema asserts the
// :SecurityGroupRule canonical node (issue #1135 PR2b, Option D) receives a uid
// uniqueness constraint and the direction / is_internet lookup indexes that back
// internet-exposure reads and per-direction reachability fan-out. The uid
// constraint is the idempotency guarantee for the port-precise rule MERGE.
func TestSchemaStatementsContainsSecurityGroupRuleSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE CONSTRAINT security_group_rule_uid_unique IF NOT EXISTS FOR (n:SecurityGroupRule) REQUIRE n.uid IS UNIQUE",
		"CREATE INDEX security_group_rule_direction IF NOT EXISTS FOR (r:SecurityGroupRule) ON (r.direction)",
		"CREATE INDEX security_group_rule_is_internet IF NOT EXISTS FOR (r:SecurityGroupRule) ON (r.is_internet)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

// TestSchemaStatementsForBackendAddsSecurityGroupRuleNornicDBUIDLookup asserts
// the NornicDB schema-backed MERGE lookup index exists for the rule node, so the
// idempotent uid MERGE does not fall back to a label scan on NornicDB.
func TestSchemaStatementsForBackendAddsSecurityGroupRuleNornicDBUIDLookup(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(t, stmts, "CREATE INDEX nornicdb_security_group_rule_uid_lookup IF NOT EXISTS FOR (n:SecurityGroupRule) ON (n.uid)")
}
