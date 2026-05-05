package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPython(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.py")
	writeTestFile(
		t,
		filePath,
		`import os

class Greeter:
    pass

def hello(name):
    value = os.path.join(name, "child")
    return value

hello("world")
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{VariableScope: "all"})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["path"] != filePath {
		t.Fatalf("path = %#v, want %#v", got["path"], filePath)
	}
	if got["repo_path"] != repoRoot {
		t.Fatalf("repo_path = %#v, want %#v", got["repo_path"], repoRoot)
	}
	if got["lang"] != "python" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "python")
	}
	if got["is_dependency"] != false {
		t.Fatalf("is_dependency = %#v, want %#v", got["is_dependency"], false)
	}

	assertNamedBucketContains(t, got, "functions", "hello")
	assertNamedBucketContains(t, got, "classes", "Greeter")
	assertNamedBucketContains(t, got, "variables", "value")
	assertNamedBucketContains(t, got, "imports", "os")
	assertNamedBucketContains(t, got, "function_calls", "join")
}

func TestDefaultEngineParsePathPythonNotebook(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "analysis.ipynb")
	writeTestFile(
		t,
		filePath,
		`{
  "cells": [
    {
      "cell_type": "markdown",
      "metadata": {},
      "source": [
        "# Notebook title"
      ]
    },
    {
      "cell_type": "code",
      "execution_count": 1,
      "metadata": {},
      "outputs": [],
      "source": [
        "import os\n",
        "\n",
        "class NotebookGreeter:\n",
        "    pass\n",
        "\n",
        "def hello(name):\n",
        "    return os.path.join(name, \"child\")\n",
        "\n",
        "hello(\"world\")\n"
      ]
    }
  ],
  "metadata": {},
  "nbformat": 4,
  "nbformat_minor": 5
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{VariableScope: "all"})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "python" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "python")
	}
	assertNamedBucketContains(t, got, "functions", "hello")
	assertNamedBucketContains(t, got, "classes", "NotebookGreeter")
	assertNamedBucketContains(t, got, "imports", "os")
	assertNamedBucketContains(t, got, "function_calls", "join")
}

func TestDefaultEngineParsePathGo(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.go")
	writeTestFile(
		t,
		filePath,
		`package main

import "fmt"

type Reader interface {
	Read(p []byte) (n int, err error)
}

type Point struct {
	X int
}

var Version = "1.0.0"

func Greet(name string) string {
	fmt.Println(name)
	return name
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

	if got["lang"] != "go" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "go")
	}

	assertNamedBucketContains(t, got, "functions", "Greet")
	assertNamedBucketContains(t, got, "interfaces", "Reader")
	assertNamedBucketContains(t, got, "structs", "Point")
	assertNamedBucketContains(t, got, "variables", "Version")
	assertNamedBucketContains(t, got, "imports", "fmt")
	assertNamedBucketContains(t, got, "function_calls", "Println")
}

func TestDefaultEngineParsePathJavaScript(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "sample.js")
	writeTestFile(
		t,
		filePath,
		`import fs from "node:fs";

class Greeter {
  greet(name) {
    return name;
  }
}

const version = "1.0.0";

function hello(value) {
  return fs.readFileSync(value);
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

	if got["lang"] != "javascript" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "javascript")
	}

	assertNamedBucketContains(t, got, "functions", "hello")
	assertNamedBucketContains(t, got, "classes", "Greeter")
	assertNamedBucketContains(t, got, "variables", "version")
	assertBucketContainsFieldValue(t, got, "imports", "source", "node:fs")
	assertNamedBucketContains(t, got, "function_calls", "readFileSync")
}

func TestDefaultEngineParsePathTypeScript(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "sample.ts")
	writeTestFile(
		t,
		filePath,
		`import { readFileSync } from "fs";

interface Reader {
  read(path: string): string;
}

class ConfigReader implements Reader {
  read(path: string): string {
    return readFileSync(path, "utf-8");
  }
}

const version = "1.0.0";

function hello(value: string): string {
  return value;
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

	if got["lang"] != "typescript" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "typescript")
	}

	assertNamedBucketContains(t, got, "functions", "hello")
	assertNamedBucketContains(t, got, "classes", "ConfigReader")
	assertNamedBucketContains(t, got, "variables", "version")
	assertBucketContainsFieldValue(t, got, "imports", "name", "readFileSync")
	assertBucketContainsFieldValue(t, got, "imports", "source", "fs")
	assertNamedBucketContains(t, got, "function_calls", "readFileSync")
	assertNamedBucketContains(t, got, "interfaces", "Reader")
}

func TestDefaultEngineParsePathRawText(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "templates", "config.cfg.j2")
	writeTestFile(
		t,
		filePath,
		`apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Values.name }}
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

	if got["lang"] != "config_template" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "config_template")
	}
	assertEmptyNamedBucket(t, got, "functions")
	assertEmptyNamedBucket(t, got, "classes")
	assertEmptyNamedBucket(t, got, "variables")
	assertEmptyNamedBucket(t, got, "imports")
	assertEmptyNamedBucket(t, got, "function_calls")
	if got["artifact_type"] != "generic_config_template" {
		t.Fatalf("artifact_type = %#v, want %#v", got["artifact_type"], "generic_config_template")
	}
	if got["template_dialect"] != "jinja" {
		t.Fatalf("template_dialect = %#v, want %#v", got["template_dialect"], "jinja")
	}
	iacRelevant, ok := got["iac_relevant"].(bool)
	if !ok {
		t.Fatalf("iac_relevant = %T, want bool", got["iac_relevant"])
	}
	if !iacRelevant {
		t.Fatalf("iac_relevant = %#v, want true", got["iac_relevant"])
	}
}

func TestDefaultEnginePreScanPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	pythonPath := filepath.Join(repoRoot, "service.py")
	goPath := filepath.Join(repoRoot, "main.go")
	writeTestFile(
		t,
		pythonPath,
		`class Greeter:
    pass

def hello(name):
    return name
`,
	)
	writeTestFile(
		t,
		goPath,
		`package main

type Point struct{}

func Greet() {}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanPaths([]string{pythonPath, goPath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "Greeter", pythonPath)
	assertPrescanContains(t, got, "hello", pythonPath)
	assertPrescanContains(t, got, "Point", goPath)
	assertPrescanContains(t, got, "Greet", goPath)
}

func TestDefaultEnginePreScanRepositoryPathsWithWorkersMatchesSequential(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	pythonPath := filepath.Join(repoRoot, "service.py")
	goPath := filepath.Join(repoRoot, "main.go")
	writeTestFile(
		t,
		pythonPath,
		`class Greeter:
    pass

def hello(name):
    return name
`,
	)
	writeTestFile(
		t,
		goPath,
		`package main

type Point struct{}

func Greet() {}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	paths := []string{pythonPath, goPath}
	want, err := engine.PreScanRepositoryPaths(repoRoot, paths)
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	got, err := engine.PreScanRepositoryPathsWithWorkers(repoRoot, paths, 2)
	if err != nil {
		t.Fatalf("PreScanRepositoryPathsWithWorkers() error = %v, want nil", err)
	}

	if !prescanMapsEqual(got, want) {
		t.Fatalf("parallel prescan = %#v, want %#v", got, want)
	}
}

func TestDefaultEnginePreScanPathsJavaScriptAndTypeScript(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	jsPath := filepath.Join(repoRoot, "sample.js")
	tsPath := filepath.Join(repoRoot, "sample.ts")
	writeTestFile(
		t,
		jsPath,
		`class Greeter {}
function hello() {}
const world = () => world;
`,
	)
	writeTestFile(
		t,
		tsPath,
		`interface Reader {}
class ConfigReader implements Reader {}
function loadConfig() {}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanPaths([]string{jsPath, tsPath})
	if err != nil {
		t.Fatalf("PreScanPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "Greeter", jsPath)
	assertPrescanContains(t, got, "hello", jsPath)
	assertPrescanContains(t, got, "Reader", tsPath)
	assertPrescanContains(t, got, "ConfigReader", tsPath)
	assertPrescanContains(t, got, "loadConfig", tsPath)
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := ensureParentDirectory(path); err != nil {
		t.Fatalf("ensureParentDirectory(%q) error = %v, want nil", path, err)
	}
	if err := osWriteFile(path, []byte(body)); err != nil {
		t.Fatalf("osWriteFile(%q) error = %v, want nil", path, err)
	}
}

func assertNamedBucketContains(t *testing.T, payload map[string]any, key string, wantName string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name == wantName {
			return
		}
	}
	t.Fatalf("%s missing name %q in %#v", key, wantName, items)
}

func assertEmptyNamedBucket(t *testing.T, payload map[string]any, key string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	if len(items) != 0 {
		t.Fatalf("%s = %#v, want empty bucket", key, items)
	}
}

func assertBucketContainsFieldValue(
	t *testing.T,
	payload map[string]any,
	key string,
	field string,
	wantValue string,
) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		value, _ := item[field].(string)
		if value == wantValue {
			return
		}
	}
	t.Fatalf("%s missing %s=%q in %#v", key, field, wantValue, items)
}

func assertPrescanContains(t *testing.T, importsMap map[string][]string, name string, wantPath string) {
	t.Helper()

	paths, ok := importsMap[name]
	if !ok {
		t.Fatalf("imports map missing %q", name)
	}
	for _, path := range paths {
		if path == wantPath {
			return
		}
	}
	t.Fatalf("imports map[%q] = %#v, want path %q", name, paths, wantPath)
}

func prescanMapsEqual(left, right map[string][]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftPaths := range left {
		rightPaths, ok := right[key]
		if !ok || len(leftPaths) != len(rightPaths) {
			return false
		}
		for i := range leftPaths {
			if leftPaths[i] != rightPaths[i] {
				return false
			}
		}
	}
	return true
}
