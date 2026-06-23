package accuracygate_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/accuracygate"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// complexityCase is one hand-counted cyclomatic-complexity expectation. A
// straight-line function must score exactly 1 and a branchy function must score
// the hand-counted McCabe value, proving the language computes real complexity
// rather than a fabricated constant (issues #3488 / #3524).
type complexityCase struct {
	language     string
	fileName     string
	source       string
	functionName string
	want         int
}

// complexityScoredLanguages is the per-language straight-line + branchy fixture
// set the gate measures. Each language contributes one straight-line case (want
// 1) and one branchy case (want > 1); a language is "scored" only when both
// observed values equal the hand-counted expectation, so a regression to a
// constant (every function = 1, or a frozen value) drops it from coverage.
//
// This mirrors the proven fixtures in
// parser/engine_cyclomatic_complexity_test.go but is owned here so the gate's
// complexity coverage count is measured end-to-end through the real parser.
func complexityScoredLanguages() map[string][]complexityCase {
	return map[string][]complexityCase{
		"go": {
			{language: "go", fileName: "straight.go", functionName: "Straight", want: 1,
				source: "package p\n\nfunc Straight(x int) int {\n\treturn x + 1\n}\n"},
			{language: "go", fileName: "branchy.go", functionName: "Branchy", want: 5,
				source: "package p\n\nfunc Branchy(x int) int {\n\tif x > 0 && x < 10 {\n\t\treturn 1\n\t}\n\tfor range []int{} {\n\t}\n\tswitch x {\n\tcase 1:\n\t\treturn 2\n\t}\n\treturn 0\n}\n"},
		},
		"python": {
			{language: "python", fileName: "straight.py", functionName: "straight", want: 1,
				source: "def straight(x):\n    return x + 1\n"},
			{language: "python", fileName: "branchy.py", functionName: "branchy", want: 4,
				source: "def branchy(x):\n    if x > 0 and x < 10:\n        return 1\n    for i in range(x):\n        pass\n    return 0\n"},
		},
		"c": {
			{language: "c", fileName: "straight.c", functionName: "straight", want: 1,
				source: "int straight(int x){ return x + 1; }\n"},
			{language: "c", fileName: "branchy.c", functionName: "branchy", want: 4,
				source: "int branchy(int x){\n  if (x > 0 && x < 10) { return 1; }\n  for (int i = 0; i < x; i++) {}\n  return 0;\n}\n"},
		},
		"cpp": {
			{language: "cpp", fileName: "straight.cpp", functionName: "straight", want: 1,
				source: "int straight(int x){ return x + 1; }\n"},
			{language: "cpp", fileName: "branchy.cpp", functionName: "branchy", want: 4,
				source: "int branchy(int x){\n  if (x > 0 && x < 10) { return 1; }\n  for (int i = 0; i < x; i++) {}\n  return 0;\n}\n"},
		},
		"java": {
			{language: "java", fileName: "Straight.java", functionName: "straight", want: 1,
				source: "class Straight {\n  int straight(int x){ return x + 1; }\n}\n"},
			{language: "java", fileName: "Branchy.java", functionName: "branchy", want: 4,
				source: "class Branchy {\n  int branchy(int x){\n    if (x > 0 && x < 10) { return 1; }\n    for (int i = 0; i < x; i++) {}\n    return 0;\n  }\n}\n"},
		},
		"csharp": {
			{language: "csharp", fileName: "Straight.cs", functionName: "Straight", want: 1,
				source: "class Straight {\n  int Straight(int x){ return x + 1; }\n}\n"},
			{language: "csharp", fileName: "Branchy.cs", functionName: "Branchy", want: 4,
				source: "class Branchy {\n  int Branchy(int x){\n    if (x > 0 && x < 10) { return 1; }\n    for (int i = 0; i < x; i++) {}\n    return 0;\n  }\n}\n"},
		},
		"rust": {
			{language: "rust", fileName: "straight.rs", functionName: "straight", want: 1,
				source: "fn straight(x: i32) -> i32 { x + 1 }\n"},
			{language: "rust", fileName: "branchy.rs", functionName: "branchy", want: 4,
				source: "fn branchy(x: i32) -> i32 {\n    if x > 0 && x < 10 { return 1; }\n    for _ in 0..x {}\n    0\n}\n"},
		},
		"scala": {
			{language: "scala", fileName: "Straight.scala", functionName: "straight", want: 1,
				source: "object S {\n  def straight(x: Int): Int = x + 1\n}\n"},
			{language: "scala", fileName: "Branchy.scala", functionName: "branchy", want: 3,
				source: "object B {\n  def branchy(x: Int): Int = {\n    if (x > 0 && x < 10) { return 1 }\n    0\n  }\n}\n"},
		},
	}
}

// measureComplexity parses every fixture through the real engine and builds a
// coverage Metric: a language is correct (scored) only when all of its cases
// observe the hand-counted complexity. The per-language label records the
// observed straight-line and branchy values so the published report shows
// exactly what each language produced.
func measureComplexity(t *testing.T, engine *parser.Engine) accuracygate.Metric {
	t.Helper()

	scored := complexityScoredLanguages()
	languages := make([]string, 0, len(scored))
	for language := range scored {
		languages = append(languages, language)
	}
	sort.Strings(languages)

	labels := make(map[string]string, len(languages))
	var correct []string
	for _, language := range languages {
		ok, detail := languageComputesRealComplexity(t, engine, scored[language])
		labels[language] = detail
		if ok {
			correct = append(correct, language)
		}
	}
	return accuracygate.CoverageMetric(correct, labels)
}

// languageComputesRealComplexity reports whether every case for a language
// observes its hand-counted complexity, plus a detail string for the report.
func languageComputesRealComplexity(t *testing.T, engine *parser.Engine, cases []complexityCase) (bool, string) {
	t.Helper()

	allCorrect := true
	parts := make([]string, 0, len(cases))
	for _, complexityCase := range cases {
		got, ok := observeComplexity(t, engine, complexityCase)
		parts = append(parts, fmt.Sprintf("%s=%d(want %d)", complexityCase.functionName, got, complexityCase.want))
		if !ok || got != complexityCase.want {
			allCorrect = false
		}
	}
	status := "scored"
	if !allCorrect {
		status = "regressed"
	}
	return allCorrect, status + ":" + joinParts(parts)
}

// observeComplexity parses one fixture and returns the function's observed
// cyclomatic complexity.
func observeComplexity(t *testing.T, engine *parser.Engine, complexityCase complexityCase) (int, bool) {
	t.Helper()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, complexityCase.fileName)
	writeFixtureFile(t, filePath, complexityCase.source)

	parsed, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v", complexityCase.fileName, err)
	}
	functions, ok := parsed["functions"].([]map[string]any)
	if !ok {
		return 0, false
	}
	for _, function := range functions {
		name, _ := function["name"].(string)
		if name != complexityCase.functionName {
			continue
		}
		value, ok := function["cyclomatic_complexity"].(int)
		return value, ok
	}
	return 0, false
}

func joinParts(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += ","
		}
		out += part
	}
	return out
}
