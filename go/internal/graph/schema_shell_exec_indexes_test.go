// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

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
