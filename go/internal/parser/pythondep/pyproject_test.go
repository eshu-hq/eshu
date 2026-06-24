// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"reflect"
	"testing"
)

// TestParsePyProjectPEP621AndPoetryDependencyTables proves the PyPI manifest
// contract for the supply-chain reducer. PEP 621 `project.dependencies` and
// Poetry's `tool.poetry.dependencies` must both reach the content_entity row
// with their declared range so vulnerability impact does not need to invoke a
// PyPI resolver.
func TestParsePyProjectPEP621AndPoetryDependencyTables(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "pyproject.toml", `
[project]
name = "demo"
dependencies = [
  "requests>=2.0,<3 ; python_version >= '3.8'",
  "numpy[mkl]~=1.26",
]

[project.optional-dependencies]
dev = ["pytest>=7"]

[tool.poetry.dependencies]
python = "^3.10"
httpx = "^0.27"
boto3 = { version = "^1.30", extras = ["crt"] }
my-local = { path = "../my-local" }
my-git = { git = "https://github.com/acme/my-git.git", rev = "v1.2.3" }

[tool.poetry.group.dev.dependencies]
mypy = "^1.8"

[tool.hatch.envs.test.dependencies]
hatch-tested = ">=0.1"
`)

	payload, err := ParsePyProject(path)
	if err != nil {
		t.Fatalf("ParsePyProject error = %v", err)
	}
	rows := variableRows(t, payload)
	byName := rowsByName(rows)

	requests, ok := byName["requests"]
	if !ok {
		t.Fatalf("requests dependency missing in %#v", rows)
	}
	if got, want := requests["value"], ">=2.0,<3"; got != want {
		t.Fatalf("requests value = %#v, want %q", got, want)
	}
	if marker, _ := requests["marker"].(string); marker != "python_version >= '3.8'" {
		t.Fatalf("requests marker = %#v, want python_version marker", requests["marker"])
	}
	if section, _ := requests["section"].(string); section != "project.dependencies" {
		t.Fatalf("requests section = %#v, want project.dependencies", requests["section"])
	}

	numpy, ok := byName["numpy"]
	if !ok {
		t.Fatalf("numpy missing")
	}
	if extras, ok := numpy["extras"].([]string); !ok || !reflect.DeepEqual(extras, []string{"mkl"}) {
		t.Fatalf("numpy extras = %#v, want [mkl]", numpy["extras"])
	}

	pytest, ok := byName["pytest"]
	if !ok {
		t.Fatalf("pytest missing")
	}
	if dev, _ := pytest["dev_dependency"].(bool); !dev {
		t.Fatalf("pytest dev_dependency = %#v, want true (optional-dependencies.dev)", pytest["dev_dependency"])
	}
	if section, _ := pytest["section"].(string); section != "project.optional-dependencies.dev" {
		t.Fatalf("pytest section = %#v, want optional-dependencies.dev", pytest["section"])
	}

	httpx, ok := byName["httpx"]
	if !ok {
		t.Fatalf("httpx missing")
	}
	if got, want := httpx["value"], "^0.27"; got != want {
		t.Fatalf("httpx value = %#v, want %q", got, want)
	}
	if section, _ := httpx["section"].(string); section != "tool.poetry.dependencies" {
		t.Fatalf("httpx section = %#v, want tool.poetry.dependencies", httpx["section"])
	}

	boto3, ok := byName["boto3"]
	if !ok {
		t.Fatalf("boto3 missing")
	}
	if got, want := boto3["value"], "^1.30"; got != want {
		t.Fatalf("boto3 value = %#v, want %q", got, want)
	}
	if extras, ok := boto3["extras"].([]string); !ok || !reflect.DeepEqual(extras, []string{"crt"}) {
		t.Fatalf("boto3 extras = %#v, want [crt]", boto3["extras"])
	}

	mypy, ok := byName["mypy"]
	if !ok {
		t.Fatalf("mypy missing")
	}
	if dev, _ := mypy["dev_dependency"].(bool); !dev {
		t.Fatalf("mypy dev_dependency = %#v, want true (poetry group dev)", mypy["dev_dependency"])
	}
	if section, _ := mypy["section"].(string); section != "tool.poetry.group.dev.dependencies" {
		t.Fatalf("mypy section = %#v, want poetry group dev section", mypy["section"])
	}

	// python is not a PyPI package — it is the Python interpreter version
	// constraint Poetry uses internally. It must not show up as a dependency
	// row so vulnerability matching cannot mis-match "python" to an unrelated
	// PyPI advisory.
	if _, ok := byName["python"]; ok {
		t.Fatalf("python interpreter constraint leaked into dependency rows: %#v", byName["python"])
	}

	// Path and git dependencies must surface as separate provenance kinds so
	// the reducer cannot treat them as exact registry versions.
	local := findRowByName(rows, "my-local")
	if local == nil {
		t.Fatalf("my-local missing")
	}
	if got, want := local["config_kind"], "path_dependency"; got != want {
		t.Fatalf("my-local config_kind = %#v, want %q", got, want)
	}
	if got, want := local["source_kind"], "path"; got != want {
		t.Fatalf("my-local source_kind = %#v, want %q", got, want)
	}

	git := findRowByName(rows, "my-git")
	if git == nil {
		t.Fatalf("my-git missing")
	}
	if got, want := git["config_kind"], "vcs_dependency"; got != want {
		t.Fatalf("my-git config_kind = %#v, want %q", got, want)
	}
	if got, want := git["source_kind"], "vcs"; got != want {
		t.Fatalf("my-git source_kind = %#v, want %q", got, want)
	}

	hatch, ok := byName["hatch-tested"]
	if !ok {
		t.Fatalf("hatch-tested missing")
	}
	if section, _ := hatch["section"].(string); section != "tool.hatch.envs.test.dependencies" {
		t.Fatalf("hatch-tested section = %#v, want hatch envs.test", hatch["section"])
	}
}

// TestParsePyProjectMalformedTableStaysMissingNotFake guards the rule that an
// invalid TOML body MUST NOT smuggle a dependency row through the parser. An
// empty/failed parse means the file is missing evidence, not safe.
func TestParsePyProjectMalformedTableStaysMissingNotFake(t *testing.T) {
	t.Parallel()

	// Closing brace mismatch and a stray `=` should not crash and should not
	// produce a confidently-named dependency row for unparseable lines.
	path := writeTempFile(t, "pyproject.toml", `
[project]
name = "demo"
dependencies = ["good>=1.0", "= junk"]
`)
	payload, err := ParsePyProject(path)
	if err != nil {
		t.Fatalf("ParsePyProject error = %v", err)
	}
	rows := variableRows(t, payload)
	if r := findRowByName(rows, "good"); r == nil {
		t.Fatalf("good>=1.0 missing despite being parseable")
	}
	malformed := findRowByConfigKind(rows, "malformed_dependency")
	if malformed == nil {
		t.Fatalf("expected malformed_dependency row for `= junk` in %#v", rows)
	}
	if got, _ := malformed["malformed"].(bool); !got {
		t.Fatalf("malformed.malformed = %#v, want true", malformed["malformed"])
	}
}

func findRowByName(rows []map[string]any, name string) map[string]any {
	for _, row := range rows {
		if got, _ := row["name"].(string); got == name {
			return row
		}
	}
	return nil
}
