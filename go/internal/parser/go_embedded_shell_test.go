// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathGoEmbeddedShellCommands(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "repo.go")
	writeTestFile(
		t,
		filePath,
		`package repo

import (
	execpkg "os/exec"
)

func runArchive() error {
	cmd := execpkg.CommandContext(ctx, "tar", "-czf", "out.tgz", ".")
	return cmd.Run()
}

func notCommand(runner interface{ Command(string) error }) error {
	return runner.Command("tar")
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	commands, ok := got["embedded_shell_commands"].([]map[string]any)
	if !ok {
		t.Fatalf("embedded_shell_commands = %T, want []map[string]any", got["embedded_shell_commands"])
	}

	want := []map[string]any{
		{
			"function_name":        "runArchive",
			"function_line_number": 7,
			"line_number":          8,
			"api":                  "os/exec.CommandContext",
			"language":             "go",
		},
	}

	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("embedded_shell_commands = %#v, want %#v", commands, want)
	}
}
