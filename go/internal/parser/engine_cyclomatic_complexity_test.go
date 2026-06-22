package parser

import (
	"path/filepath"
	"testing"
)

// TestCyclomaticComplexityPerLanguage proves that the native tree-sitter
// adapters compute real McCabe cyclomatic complexity instead of a constant 1.
// Each fixture is hand-counted: complexity = 1 + decision points, where a
// decision point is an if/elif, loop, switch/match arm, catch, ternary, or a
// short-circuit boolean operator (&& / ||). Straight-line functions must yield
// exactly 1 so complexity rankings stay meaningful per issue #3488.
func TestCyclomaticComplexityPerLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		fileName     string
		source       string
		functionName string
		want         int
	}{
		{
			name:         "go_straight_line",
			fileName:     "straight.go",
			source:       "package p\n\nfunc Straight(x int) int {\n\treturn x + 1\n}\n",
			functionName: "Straight",
			want:         1,
		},
		{
			name:     "go_branches_and_boolean",
			fileName: "branchy.go",
			// 1 base + if + && + for + switch case + ternary-like (none) = if(1) && (1) for(1) case(1)
			source:       "package p\n\nfunc Branchy(x int) int {\n\tif x > 0 && x < 10 {\n\t\treturn 1\n\t}\n\tfor range []int{} {\n\t}\n\tswitch x {\n\tcase 1:\n\t\treturn 2\n\t}\n\treturn 0\n}\n",
			functionName: "Branchy",
			want:         5,
		},
		{
			name:         "python_straight_line",
			fileName:     "straight.py",
			source:       "def straight(x):\n    return x + 1\n",
			functionName: "straight",
			want:         1,
		},
		{
			name:         "python_branches_and_boolean",
			fileName:     "branchy.py",
			source:       "def branchy(x):\n    if x > 0 and x < 10:\n        return 1\n    for i in range(x):\n        pass\n    return 0\n",
			functionName: "branchy",
			// base 1 + if 1 + and 1 + for 1 = 4
			want: 4,
		},
		{
			name:         "c_straight_line",
			fileName:     "straight.c",
			source:       "int straight(int x){ return x + 1; }\n",
			functionName: "straight",
			want:         1,
		},
		{
			name:     "c_branches_and_boolean",
			fileName: "branchy.c",
			// base 1 + if 1 + && 1 + for 1 + while 1 + case 1 + ?: 1 = 7
			source:       "int branchy(int x){\n  if (x > 0 && x < 10) { return 1; }\n  for (;;) {}\n  while (x) {}\n  switch (x) { case 1: break; }\n  return x ? 1 : 0;\n}\n",
			functionName: "branchy",
			want:         7,
		},
		{
			name:         "cpp_straight_line",
			fileName:     "straight.cpp",
			source:       "int straight(int x){ return x + 1; }\n",
			functionName: "straight",
			want:         1,
		},
		{
			name:     "cpp_branches_and_boolean",
			fileName: "branchy.cpp",
			// base 1 + if 1 + || 1 + while 1 + ?: 1 = 5
			source:       "int branchy(int x){\n  if (x > 0 || x < 10) {}\n  while (x) {}\n  return x ? 1 : 0;\n}\n",
			functionName: "branchy",
			want:         5,
		},
		{
			name:         "java_straight_line",
			fileName:     "Straight.java",
			source:       "class Straight {\n  int run(int x){ return x + 1; }\n}\n",
			functionName: "run",
			want:         1,
		},
		{
			name:     "java_branches_and_boolean",
			fileName: "Branchy.java",
			// base 1 + if 1 + && 1 + for 1 + catch 1 + ternary 1 = 6
			source:       "class Branchy {\n  int run(int x){\n    if (x > 0 && x < 10) {}\n    for (int i = 0; i < x; i++) {}\n    try {} catch (Exception e) {}\n    return x > 0 ? 1 : 0;\n  }\n}\n",
			functionName: "run",
			want:         6,
		},
		{
			name:         "csharp_straight_line",
			fileName:     "Straight.cs",
			source:       "class Straight {\n  int Run(int x){ return x + 1; }\n}\n",
			functionName: "Run",
			want:         1,
		},
		{
			name:     "csharp_branches_and_boolean",
			fileName: "Branchy.cs",
			// base 1 + if 1 + && 1 + foreach 1 + catch 1 + ternary 1 = 6
			source:       "class Branchy {\n  int Run(int x, int[] xs){\n    if (x > 0 && x < 10) {}\n    foreach (var i in xs) {}\n    try {} catch {}\n    return x > 0 ? 1 : 0;\n  }\n}\n",
			functionName: "Run",
			want:         6,
		},
		{
			name:         "rust_straight_line",
			fileName:     "straight.rs",
			source:       "fn straight(x: i32) -> i32 { x + 1 }\n",
			functionName: "straight",
			want:         1,
		},
		{
			name:     "rust_branches_and_boolean",
			fileName: "branchy.rs",
			// base 1 + if 1 + && 1 + while 1 + match real arm `1` 1 = 5. The
			// trailing `_` arm is the implicit else and is not a decision point.
			source:       "fn branchy(x: i32) -> i32 {\n  if x > 0 && x < 10 {}\n  while x > 0 {}\n  match x { 1 => 1, _ => 0 }\n}\n",
			functionName: "branchy",
			want:         5,
		},
		{
			name:         "scala_straight_line",
			fileName:     "Straight.scala",
			source:       "object Straight {\n  def run(x: Int): Int = x + 1\n}\n",
			functionName: "run",
			want:         1,
		},
		{
			name:     "scala_branches_and_boolean",
			fileName: "Branchy.scala",
			// base 1 + if 1 + && 1 = 3
			source:       "object Branchy {\n  def run(x: Int): Int = {\n    if (x > 0 && x < 10) 1 else 0\n  }\n}\n",
			functionName: "run",
			want:         3,
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
