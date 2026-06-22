package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestDefaultEngineParsePathPythonMultilineClassHeaderUsesAST proves the class
// base/metaclass extraction is driven by the tree-sitter `superclasses` field
// rather than a single-line regex. The previous `(?m)` regex never matched a
// class header whose argument list spanned multiple physical lines, so this is
// the failing-first guard for the AST migration.
func TestDefaultEngineParsePathPythonMultilineClassHeaderUsesAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "multiline.py")
	writeTestFile(
		t,
		filePath,
		`class Base:
    pass


class Mixin:
    pass


class Logged(
    Base,
    pkg.Mixin,
    metaclass=abc.ABCMeta,
):
    pass
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

	logged := assertBucketItemByName(t, got, "classes", "Logged")
	bases, ok := logged["bases"].([]string)
	if !ok {
		t.Fatalf(`classes["Logged"]["bases"] = %T, want []string`, logged["bases"])
	}
	if want := []string{"Base", "Mixin"}; !reflect.DeepEqual(bases, want) {
		t.Fatalf(`classes["Logged"]["bases"] = %#v, want %#v`, bases, want)
	}
	assertStringFieldValue(t, logged, "metaclass", "abc.ABCMeta")
}

// TestDefaultEngineParsePathPythonSplatTypedParamAnnotations characterizes the
// parameter/return annotation extraction across typed splat parameters so the
// AST rewrite stays byte-identical to the prior regex payload.
func TestDefaultEngineParsePathPythonSplatTypedParamAnnotations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "splat.py")
	writeTestFile(
		t,
		filePath,
		`def handler(first: int, *args: str, **kwargs: bytes) -> dict[str, int]:
    return {}
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

	annotations, ok := got["type_annotations"].([]map[string]any)
	if !ok {
		t.Fatalf(`type_annotations = %T, want []map[string]any`, got["type_annotations"])
	}
	want := []map[string]any{
		{
			"name":            "args",
			"line_number":     1,
			"type":            "str",
			"annotation_kind": "parameter",
			"context":         "handler",
			"lang":            "python",
		},
		{
			"name":            "first",
			"line_number":     1,
			"type":            "int",
			"annotation_kind": "parameter",
			"context":         "handler",
			"lang":            "python",
		},
		{
			"name":            "handler",
			"line_number":     1,
			"type":            "dict[str, int]",
			"annotation_kind": "return",
			"context":         "handler",
			"lang":            "python",
		},
		{
			"name":            "kwargs",
			"line_number":     1,
			"type":            "bytes",
			"annotation_kind": "parameter",
			"context":         "handler",
			"lang":            "python",
		},
	}
	if !reflect.DeepEqual(annotations, want) {
		t.Fatalf("type_annotations = %#v, want %#v", annotations, want)
	}
}

// TestDefaultEngineParsePathPythonEmbeddedShellRichParity locks the embedded
// shell evidence payload across module aliases, direct imports, the os module,
// shadowing, nested-function attribution, and module-level calls so the AST
// rewrite reproduces the line-scan payload byte-for-byte.
func TestDefaultEngineParsePathPythonEmbeddedShellRichParity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "rich.py")
	writeTestFile(
		t,
		filePath,
		`import subprocess as sp
import os
from subprocess import run
from os import system as sh


def archive():
    proc = sp.Popen(["tar"])
    run(["ls"])
    os.system("echo hi")
    sh("echo bye")
    return proc


def shadowed():
    sp = make()
    sp.Popen(["x"])


def outer():
    def inner():
        sp.Popen(["nested"])
    return inner


result = sp.Popen(["module-level"])
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
		{"function_name": "archive", "function_line_number": 7, "line_number": 8, "api": "subprocess.Popen", "language": "python"},
		{"function_name": "archive", "function_line_number": 7, "line_number": 9, "api": "subprocess.run", "language": "python"},
		{"function_name": "archive", "function_line_number": 7, "line_number": 10, "api": "os.system", "language": "python"},
		{"function_name": "archive", "function_line_number": 7, "line_number": 11, "api": "os.system", "language": "python"},
		{"function_name": "outer", "function_line_number": 20, "line_number": 22, "api": "subprocess.Popen", "language": "python"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("embedded_shell_commands = %#v, want %#v", commands, want)
	}
}
