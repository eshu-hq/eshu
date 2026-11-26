// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

// TestSchemaStatementsContainsCidrBlockSchema asserts the CidrBlock canonical
// node (issue #1135 PR2a) receives a uid uniqueness constraint and the cidr
// lookup index that backs internet-exposure reads and the later ALLOWS_INGRESS
// edge join (PR2b).
func TestSchemaStatementsContainsCidrBlockSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE CONSTRAINT cidr_block_uid_unique IF NOT EXISTS FOR (n:CidrBlock) REQUIRE n.uid IS UNIQUE",
		"CREATE INDEX cidr_block_cidr IF NOT EXISTS FOR (c:CidrBlock) ON (c.cidr)",
		"CREATE INDEX cidr_block_is_internet IF NOT EXISTS FOR (c:CidrBlock) ON (c.is_internet)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

// TestSchemaStatementsContainsPrefixListSchema asserts the PrefixList canonical
// node (issue #1135 PR2a) receives a uid uniqueness constraint and the
// prefix_list_id lookup index that backs the later edge join (PR2b).
func TestSchemaStatementsContainsPrefixListSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE CONSTRAINT prefix_list_uid_unique IF NOT EXISTS FOR (n:PrefixList) REQUIRE n.uid IS UNIQUE",
		"CREATE INDEX prefix_list_prefix_list_id IF NOT EXISTS FOR (p:PrefixList) ON (p.prefix_list_id)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

// TestSchemaStatementsForBackendDropsSecurityGroupEndpointNornicDBUIDLookupIndex
// asserts the NornicDB schema drops the retired uid lookup indexes for both new
// canonical labels, so the uid uniqueness constraints (whose 1.5.x backing
// indexes serve the idempotent uid MERGEs) can be created.
func TestSchemaStatementsForBackendDropsSecurityGroupEndpointNornicDBUIDLookupIndex(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	for _, want := range []string{
		"DROP INDEX nornicdb_cidr_block_uid_lookup IF EXISTS",
		"DROP INDEX nornicdb_prefix_list_uid_lookup IF EXISTS",
	} {
		assertContainsStatement(t, stmts, want)
	}
}
