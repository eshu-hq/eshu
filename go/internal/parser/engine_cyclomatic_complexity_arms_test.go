// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestCyclomaticComplexityCatchAndDefaultArms locks in two McCabe edge cases
// flagged on PR #3523:
//
//   - Exception handlers (catch/except) must each add one decision point. C++
//     try/catch in particular must not be silently zero.
//   - The switch `default` arm and the bare `match`/`case` wildcard `_` are the
//     implicit else, not a decision, so they must NOT add a decision point. A
//     switch or match whose only arm is the catch-all stays complexity 1.
func TestCyclomaticComplexityCatchAndDefaultArms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		fileName     string
		source       string
		functionName string
		want         int
	}{
		// Catch handlers each add one decision point.
		{
			name:         "cpp_single_catch",
			fileName:     "catch.cpp",
			source:       "int run(){\n  try { work(); }\n  catch (const Error& e) { recover(); }\n  return 0;\n}\n",
			functionName: "run",
			want:         2, // base 1 + catch 1
		},
		{
			name:         "cpp_two_catch",
			fileName:     "catch2.cpp",
			source:       "int run(){\n  try { work(); }\n  catch (const Error& e) {}\n  catch (...) {}\n  return 0;\n}\n",
			functionName: "run",
			want:         3, // base 1 + catch 2
		},
		{
			name:         "java_catch",
			fileName:     "Catch.java",
			source:       "class Catch {\n  int run(){\n    try {} catch (Exception e) {}\n    return 0;\n  }\n}\n",
			functionName: "run",
			want:         2,
		},
		{
			name:         "csharp_catch",
			fileName:     "Catch.cs",
			source:       "class Catch {\n  int Run(){\n    try {} catch (System.Exception e) {}\n    return 0;\n  }\n}\n",
			functionName: "Run",
			want:         2,
		},
		{
			name:         "scala_catch",
			fileName:     "Catch.scala",
			source:       "object Catch {\n  def run(): Int = {\n    try { 1 } catch { case e: Exception => 2 }\n  }\n}\n",
			functionName: "run",
			want:         2,
		},
		{
			name:         "python_except",
			fileName:     "except.py",
			source:       "def run():\n    try:\n        work()\n    except ValueError:\n        recover()\n",
			functionName: "run",
			want:         2,
		},
		// try-with-finally only (no catch): finally is unconditional cleanup, not
		// a decision point.
		{
			name:         "java_try_finally_only",
			fileName:     "Finally.java",
			source:       "class Finally {\n  int run(){\n    try { work(); } finally {}\n    return 0;\n  }\n}\n",
			functionName: "run",
			want:         1,
		},
		// switch default arm is the implicit else: one case + default = 2, not 3.
		{
			name:         "go_case_default",
			fileName:     "sw.go",
			source:       "package p\n\nfunc Run(x int) int {\n\tswitch x {\n\tcase 1:\n\t\treturn 1\n\tdefault:\n\t\treturn 0\n\t}\n}\n",
			functionName: "Run",
			want:         2,
		},
		{
			name:         "go_only_default",
			fileName:     "swd.go",
			source:       "package p\n\nfunc Run(x int) int {\n\tswitch x {\n\tdefault:\n\t\treturn 0\n\t}\n}\n",
			functionName: "Run",
			want:         1,
		},
		{
			name:         "java_case_default",
			fileName:     "Sw.java",
			source:       "class Sw {\n  int run(int x){\n    switch (x) {\n      case 1: return 1;\n      default: return 0;\n    }\n  }\n}\n",
			functionName: "run",
			want:         2,
		},
		{
			name:         "java_only_default",
			fileName:     "Swd.java",
			source:       "class Swd {\n  int run(int x){\n    switch (x) {\n      default: return 0;\n    }\n  }\n}\n",
			functionName: "run",
			want:         1,
		},
		{
			name:         "csharp_case_default",
			fileName:     "Sw.cs",
			source:       "class Sw {\n  int Run(int x){\n    switch (x) {\n      case 1: return 1;\n      default: return 0;\n    }\n  }\n}\n",
			functionName: "Run",
			want:         2,
		},
		{
			name:         "csharp_only_default",
			fileName:     "Swd.cs",
			source:       "class Swd {\n  int Run(int x){\n    switch (x) {\n      default: return 0;\n    }\n  }\n}\n",
			functionName: "Run",
			want:         1,
		},
		{
			name:         "c_case_default",
			fileName:     "sw.c",
			source:       "int run(int x){\n  switch (x) {\n    case 1: return 1;\n    default: return 0;\n  }\n  return 9;\n}\n",
			functionName: "run",
			want:         2,
		},
		{
			name:         "c_only_default",
			fileName:     "swd.c",
			source:       "int run(int x){\n  switch (x) {\n    default: return 0;\n  }\n  return 9;\n}\n",
			functionName: "run",
			want:         1,
		},
		// Rust match: bare wildcard arm is the implicit else.
		{
			name:         "rust_case_wildcard",
			fileName:     "m.rs",
			source:       "fn run(x: i32) -> i32 {\n  match x {\n    1 => 1,\n    _ => 0,\n  }\n}\n",
			functionName: "run",
			want:         2,
		},
		{
			name:         "rust_only_wildcard",
			fileName:     "md.rs",
			source:       "fn run(x: i32) -> i32 {\n  match x {\n    _ => 0,\n  }\n}\n",
			functionName: "run",
			want:         1,
		},
		{
			name:     "rust_guarded_wildcard",
			fileName: "mg.rs",
			// base 1 + pattern arm 1 + guarded wildcard arm 1 (guard is a decision);
			// the trailing bare wildcard is the implicit else = 3.
			source:       "fn run(x: i32) -> i32 {\n  match x {\n    1 => 1,\n    _ if x > 5 => 2,\n    _ => 0,\n  }\n}\n",
			functionName: "run",
			want:         3,
		},
		// Scala match: bare wildcard arm is the implicit else.
		{
			name:         "scala_case_wildcard",
			fileName:     "M.scala",
			source:       "object M {\n  def run(x: Int): Int = x match {\n    case 1 => 1\n    case _ => 0\n  }\n}\n",
			functionName: "run",
			want:         2,
		},
		// Python match: bare wildcard arm is the implicit else.
		{
			name:         "python_case_wildcard",
			fileName:     "m.py",
			source:       "def run(x):\n    match x:\n        case 1:\n            return 1\n        case _:\n            return 0\n",
			functionName: "run",
			want:         2,
		},
		// Elixir case: boolean literal arms (`false`/`true`) are real patterns,
		// not catch-alls. Only the bare `_` wildcard is the implicit else.
		{
			name:     "elixir_boolean_case_arms",
			fileName: "bools.ex",
			// base 1 + `false ->` 1 + `true ->` 1 = 3. The `_ ->` arm is the
			// implicit else and is not counted.
			source:       "def run(flag) do\n  case flag do\n    false -> 1\n    true -> 2\n    _ -> 3\n  end\nend\n",
			functionName: "run",
			want:         3,
		},
		{
			name:     "elixir_only_wildcard_case",
			fileName: "wild.ex",
			// A case whose only arm is the bare `_` catch-all stays at base 1.
			source:       "def run(flag) do\n  case flag do\n    _ -> 1\n  end\nend\n",
			functionName: "run",
			want:         1,
		},
	}

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			filePath := filepath.Join(repoRoot, testCase.fileName)
			writeTestFile(t, filePath, testCase.source)

			got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
			if err != nil {
				t.Fatalf("ParsePath() error = %v, want nil", err)
			}

			function := assertFunctionByName(t, got, testCase.functionName)
			assertIntFieldValue(t, function, "cyclomatic_complexity", testCase.want)
		})
	}
}
