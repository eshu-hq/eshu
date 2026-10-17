// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathKeysStaticMembersOnlyUnderTheClosedRule is the
// #8280 review F10 regression table for the closed import-name rule of the
// static member keys (ruling (B)). Each construct puts one more occurrence of
// the import name Logger next to `Logger.create()`. The member call keys only
// when that occurrence is a call, `new`, JSX, or member call on Logger, a
// TypeScript type position, a read that runs no user code, or text that is
// never the variable. Every other grammar position (a binding, a write, a
// plain read, an operand) leaves `Logger.create()` unkeyed.
func TestDefaultEngineParsePathKeysStaticMembersOnlyUnderTheClosedRule(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"Logger.build();", "Logger.build.call(null);", "Logger();", "new Logger();", "Logger.create?.();",
		"o.Logger;", "o.Logger();", "const p = { Logger: 1 };", "class K { Logger() {} }",
		"const s = \"Logger\";", "// Logger is the logging class", "const r = /Logger/;",
		"Logger: for (;;) { break Logger; }",
		"if (Logger) { k(); }", "while (Logger) { break; }", "do { k(); } while (Logger);",
		"!Logger;", "typeof Logger;", "Logger === y;", "Logger !== y;", "Logger ? 1 : 2;",
	}
	tsAllowed := []string{
		"let v: Logger;", "let w: typeof Logger;", "function g(a: Logger) { return a; }",
		"class Q implements Logger {}", "type T = Logger;", "const list: Array<Logger> = [];",
	}
	rejected := []string{
		// Plain reads and operands.
		"Logger;", "(Logger);", "f(Logger);", "new F(Logger);", "[Logger];", "({ Logger });", "({ k: Logger });",
		"`${Logger}`;", "-Logger;", "void Logger;", "Logger || y;", "Logger ?? y;", "Logger && y;",
		"Logger == y;", "Logger + 1;", "x instanceof Logger;", "\"k\" in Logger;", "const y = Logger;",
		"f(...Logger);", "export { Logger };", "export default Logger;", "class S extends Logger {}",
		"Logger.LEVEL;", "Logger[k]();",
		// Writes to the name or to its members.
		"Logger = o;", "Logger += 1;", "Logger++;", "[Logger] = a;", "({ Logger } = o);",
		"Logger.create = f;", "Logger.create += f;", "delete Logger.create;", "[Logger.create] = a;",
		"for (Logger of a) {}", "Object.assign(Logger, o);",
		// Bindings of the name.
		"{ let Logger = o; }", "{ var Logger = o; }", "function Logger() {}", "function f(Logger) {}",
		"const g = (Logger) => Logger;", "try {} catch (Logger) {}", "class Logger {}",
		"const c = class Logger {};", "function f({ Logger }) {}", "for (const Logger of a) {}",
		// Functions that can see the name.
		"function f() { return Logger; }", "const h = () => Logger;",
	}
	tsRejected := []string{
		"namespace N { import Logger = Other.Thing; }", "{ using Logger = res(); }",
		"async function f() { await using Logger = res(); }", "enum Logger { A }", "namespace Logger {}",
		"abstract class Logger {}", "declare class Logger {}", "function f(Logger: number) {}",
		"(Logger as any).create = f;", "Logger!.create = f;", "interface Logger {}",
	}
	type row struct {
		ext       string
		construct string
		wantKey   bool
	}
	var rows []row
	for _, construct := range allowed {
		rows = append(rows, row{".ts", construct, true}, row{".js", construct, true})
	}
	for _, construct := range tsAllowed {
		rows = append(rows, row{".ts", construct, true})
	}
	for _, construct := range rejected {
		rows = append(rows, row{".ts", construct, false}, row{".js", construct, false})
	}
	for _, construct := range tsRejected {
		rows = append(rows, row{".ts", construct, false})
	}
	for _, tc := range rows {
		t.Run(tc.ext+" "+tc.construct, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {"@acme/log": "1"}}`)
			filePath := filepath.Join(repoRoot, "src", "construct"+tc.ext)
			params := "o, a, f, y, k"
			if tc.ext == ".ts" {
				params = "o: any, a: any, f: any, y: any, k: any"
			}
			writeTestFile(t, filePath, "import { Logger } from \"@acme/log\";\n\nexport function run("+params+") {\n  "+
				tc.construct+"\n  Logger.create(\"in\");\n}\n")
			want := ""
			if tc.wantKey {
				want = "package:@acme/log#Logger.create"
			}
			if key := callPackageKey(t, parsePackageKeyFixture(t, repoRoot, filePath), "Logger.create", "function_call"); key != want {
				t.Errorf("Logger.create package_export_symbol = %q, want %q after %q", key, want, tc.construct)
			}
		})
	}
}
