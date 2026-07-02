// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsIncludeInheritanceRetractLookupIndexes(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNornicDB, err)
	}

	expected := []string{
		"CREATE INDEX function_repo_id IF NOT EXISTS FOR (f:Function) ON (f.repo_id)",
		"CREATE INDEX function_path IF NOT EXISTS FOR (f:Function) ON (f.path)",
		"CREATE INDEX class_repo_id IF NOT EXISTS FOR (c:Class) ON (c.repo_id)",
		"CREATE INDEX class_path IF NOT EXISTS FOR (c:Class) ON (c.path)",
		"CREATE INDEX interface_repo_id IF NOT EXISTS FOR (i:Interface) ON (i.repo_id)",
		"CREATE INDEX interface_path IF NOT EXISTS FOR (i:Interface) ON (i.path)",
		"CREATE INDEX trait_repo_id IF NOT EXISTS FOR (t:Trait) ON (t.repo_id)",
		"CREATE INDEX trait_path IF NOT EXISTS FOR (t:Trait) ON (t.path)",
		"CREATE INDEX struct_repo_id IF NOT EXISTS FOR (s:Struct) ON (s.repo_id)",
		"CREATE INDEX struct_path IF NOT EXISTS FOR (s:Struct) ON (s.path)",
		"CREATE INDEX enum_repo_id IF NOT EXISTS FOR (e:Enum) ON (e.repo_id)",
		"CREATE INDEX enum_path IF NOT EXISTS FOR (e:Enum) ON (e.path)",
		"CREATE INDEX protocol_repo_id IF NOT EXISTS FOR (p:Protocol) ON (p.repo_id)",
		"CREATE INDEX protocol_path IF NOT EXISTS FOR (p:Protocol) ON (p.path)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

func TestSchemaStatementsIncludeShellExecRetractLookupIndexes(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNornicDB, err)
	}

	expected := []string{
		"CREATE INDEX shell_command_repo_id IF NOT EXISTS FOR (s:ShellCommand) ON (s.repo_id)",
		"CREATE INDEX shell_command_path IF NOT EXISTS FOR (s:ShellCommand) ON (s.path)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}
