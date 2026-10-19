// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathSkipsPlainCallsOfReboundImports pins the #8310
// repros for the #7601 plain-call keys: an import alias, a using declaration,
// a block let, or a sloppy-mode eval var rebinds the import name where the
// open redeclaredImportNames rule does not look, so the plain `Logger()` call
// must stay unkeyed. Each case starts with the package import; the sloppy eval
// case is a script (.js), where eval shares the function scope.
func TestDefaultEngineParsePathSkipsPlainCallsOfReboundImports(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		ext    string
		source string
	}{
		{"import alias", ".ts", "namespace N {\n  import Logger = Other.Thing;\n  Logger.create();\n  Logger();\n}\n"},
		{"using declaration", ".ts", "{\n  using Logger = res();\n  Logger.create();\n  Logger();\n}\n"},
		{"block let", ".ts", "{\n  let Logger = other;\n  Logger.create();\n  Logger();\n}\n"},
		{"sloppy eval var", ".js", "function g() {\n  eval(\"var Logger = other\");\n  Logger.create();\n  Logger();\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {"@acme/log": "1"}}`)
			filePath := filepath.Join(repoRoot, "src", "repro"+tc.ext)
			writeTestFile(t, filePath, "import { Logger } from \"@acme/log\";\n"+tc.source)
			got := parsePackageKeyFixture(t, repoRoot, filePath)
			if key := callPackageKey(t, got, "Logger", "function_call"); key != "" {
				t.Errorf("Logger() package_export_symbol = %q, want none: the import name is rebound by %s", key, tc.name)
			}
		})
	}
}

// TestDefaultEngineParsePathKeepsPlainCallsOnlyUnderTheClosedRule is the
// #8310 recall table for the plain closed rule. Each construct puts one more
// occurrence of the import name Logger next to `Logger()`. The plain call
// keys only when that occurrence is the import itself, a call, `new`, a JSX
// tag, a member object, or a TypeScript type position. Any bare read of the
// name — an argument, an initializer, an extends clause, an instanceof
// operand, a condition, a typeof operand, an export, or a write — drops every
// plain key of that name in the file. That drop is the stated recall cost of
// closing the rule: on main all of the dropped rows below keyed `Logger()`
// except the two rebinding rows (the parameter and the block let).
func TestDefaultEngineParsePathKeepsPlainCallsOnlyUnderTheClosedRule(t *testing.T) {
	t.Parallel()

	kept := []string{
		"Logger();", "new Logger();", "Logger.create();", "Logger.LEVEL;",
		"const o = { Logger: 1 };",
	}
	tsKept := []string{
		"let v: Logger;", "function g(a: Logger) { return a; }",
	}
	tsxKept := []string{
		"const el = <Logger />;",
	}
	dropped := []string{
		// Bare reads that could hand the class to code that replaces it.
		"f(Logger);", "const y = Logger;", "x instanceof Logger;",
		"if (Logger) { f(); }", "typeof Logger;", "export { Logger };",
		// Writes and rebindings of the name.
		"Logger = o;", "function f(Logger) { return Logger; }",
		"{ let Logger = o; }",
	}
	tsDropped := []string{
		"class S extends Logger {}",
	}
	type row struct {
		ext       string
		construct string
		wantKey   bool
	}
	var rows []row
	for _, construct := range kept {
		rows = append(rows, row{".ts", construct, true}, row{".js", construct, true})
	}
	for _, construct := range tsKept {
		rows = append(rows, row{".ts", construct, true})
	}
	for _, construct := range tsxKept {
		rows = append(rows, row{".tsx", construct, true})
	}
	for _, construct := range dropped {
		rows = append(rows, row{".ts", construct, false}, row{".js", construct, false})
	}
	for _, construct := range tsDropped {
		rows = append(rows, row{".ts", construct, false})
	}
	for _, tc := range rows {
		t.Run(tc.ext+" "+tc.construct, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {"@acme/log": "1"}}`)
			filePath := filepath.Join(repoRoot, "src", "edge"+tc.ext)
			writeTestFile(t, filePath, "import { Logger } from \"@acme/log\";\n"+tc.construct+"\nLogger();\n")
			want := ""
			if tc.wantKey {
				want = "package:@acme/log#Logger"
			}
			if key := callPackageKey(t, parsePackageKeyFixture(t, repoRoot, filePath), "Logger", "function_call"); key != want {
				t.Errorf("Logger() package_export_symbol = %q, want %q next to %q", key, want, tc.construct)
			}
		})
	}
}

// TestDefaultEngineParsePathSkipsNamespaceMemberCallsOfModuleReboundImports
// pins the #8310 comment repro for the #7601 namespace-member keys: a
// `module fmt` declaration rebinds the `import * as fmt` name where the open
// redeclaredImportNames rule does not look. The closed plain rule drops every
// plain key of that name file-wide, so both the inner call (which reads the
// local module) and the outer call stay unkeyed; the outer drop is the stated
// recall cost of closing the rule.
func TestDefaultEngineParsePathSkipsNamespaceMemberCallsOfModuleReboundImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {"@acme/log": "1"}}`)
	filePath := filepath.Join(repoRoot, "src", "repro.ts")
	writeTestFile(t, filePath, "import * as fmt from \"@acme/log\";\nnamespace N {\n  module fmt { export function f() {} }\n  fmt.f();\n}\nfmt.f();\n")
	got := parsePackageKeyFixture(t, repoRoot, filePath)
	calls, _ := got["function_calls"].([]map[string]any)
	seen := 0
	for _, call := range calls {
		if call["full_name"] != "fmt.f" {
			continue
		}
		seen++
		if key, _ := call["package_export_symbol"].(string); key != "" {
			t.Errorf("fmt.f() package_export_symbol = %q, want none: the import name is rebound by module fmt", key)
		}
	}
	if seen != 2 {
		t.Errorf("fmt.f() call count = %d, want 2: the repro must cover both the inner and the outer call", seen)
	}
}
