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
			// base 1 + if 1 + && 1 + while 1 + match arms 2 = 6
			source:       "fn branchy(x: i32) -> i32 {\n  if x > 0 && x < 10 {}\n  while x > 0 {}\n  match x { 1 => 1, _ => 0 }\n}\n",
			functionName: "branchy",
			want:         6,
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
