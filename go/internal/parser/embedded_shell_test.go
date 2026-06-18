package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathPythonEmbeddedShellCommands(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "repo.py")
	writeTestFile(
		t,
		filePath,
		`import subprocess as sp
from os import system as os_system

def archive():
    proc = sp.Popen(["tar", "cf", "out.tar", "."])
    return proc.wait()

def direct():
    return os_system("tar")

def not_shell(runner):
    return runner.Popen("tar")
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
			"function_name":        "archive",
			"function_line_number": 4,
			"line_number":          5,
			"api":                  "subprocess.Popen",
			"language":             "python",
		},
		{
			"function_name":        "direct",
			"function_line_number": 8,
			"line_number":          9,
			"api":                  "os.system",
			"language":             "python",
		},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("embedded_shell_commands = %#v, want %#v", commands, want)
	}
}

func TestDefaultEngineParsePathJavaScriptEmbeddedShellCommands(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "repo.js")
	writeTestFile(
		t,
		filePath,
		`const cp = require("child_process");
const { execFileSync: runFile } = require("child_process");

function archive() {
  cp.spawn("tar", ["cf", "out.tar", "."]);
}

function direct() {
  runFile("tar", ["tf", "out.tar"]);
}

function notShell(runner) {
  runner.spawn("tar");
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
			"function_name":        "archive",
			"function_line_number": 4,
			"line_number":          5,
			"api":                  "child_process.spawn",
			"language":             "javascript",
		},
		{
			"function_name":        "direct",
			"function_line_number": 8,
			"line_number":          9,
			"api":                  "child_process.execFileSync",
			"language":             "javascript",
		},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("embedded_shell_commands = %#v, want %#v", commands, want)
	}
}
