// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestRepositoryFilesToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_repository_files")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"repo_id", "language", "path", "recursive", "ref"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestRepositoryFilesToolRequiresRepoID(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_repository_files")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required type = %T, want []string", schema["required"])
	}
	found := false
	for _, f := range required {
		if f == "repo_id" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("required = %#v, want repo_id listed", required)
	}
}

func TestRepositoryFilesToolLanguageIsOptional(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_repository_files")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	required, _ := schema["required"].([]string)
	for _, f := range required {
		if f == "language" {
			t.Fatalf("language is listed as required; it must be optional")
		}
	}
}

// TestIndexedFilesRuleInFileToolDescriptions pins the indexed-file rule into
// every tool that reads the file index, so a "file not found" is never
// mistaken for "Eshu cannot see dotfiles" (#8808).
func TestIndexedFilesRuleInFileToolDescriptions(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"list_repository_files", "get_file_content", "search_file_content"} {
		desc := requireToolDefinition(t, name).Description
		for _, want := range []string{
			"parser-registry name, prefix, extension, or documentation format",
			"Dotfiles are not hidden",
			".gitmodules",
			"CODEOWNERS",
			"gitignored",
			".eshuignore",
			"default-pruned",
			".eslintrc.js",
			".github/workflows/*",
			".env",
			"may simply not be indexed",
		} {
			if !strings.Contains(desc, want) {
				t.Errorf("%s description missing %q", name, want)
			}
		}
	}
}
