// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"
)

// These tests pin the #8280 static member keys. A static method of an exported
// class carries export_name <Export>.<method>, where <Export> is the class's
// own export name ("default" for the entry file's default export). A consumer
// call on a class bound to a bare package import (`C.m()`, `D.m()`, `ns.C.m()`)
// carries the same key. Every shape that could name the wrong method stays
// unkeyed.

// methodKeys returns the package keys of the one functions-bucket item with
// the given name declared on the given line.
func methodKeys(t *testing.T, payload map[string]any, name string, line int) (string, string) {
	t.Helper()

	items, _ := payload["functions"].([]map[string]any)
	for _, item := range items {
		if item["name"] != name || item["line_number"] != line {
			continue
		}
		packageID, _ := item["package_id"].(string)
		exportName, _ := item["export_name"].(string)
		return packageID, exportName
	}
	t.Fatalf("functions has no %q on line %d in %#v", name, line, items)
	return "", ""
}

// memberProducerSource is the producer entry file the member key tests parse.
const memberProducerSource = `export class Logger {
  static create(name: string) { return new Logger(); }
  warn(message: string) {}
  private secret() {}
  protected guarded() {}
  #hidden() {}
  get level() { return 1; }
  set level(value: number) {}
  constructor() {}
  ["computed"]() {}
  static async load() {}
  public open() {}
}

class Internal {
  static create() {}
  warn() {}
}

export default class Client {
  static connect() {}
  send() {}
}

export abstract class Base {
  abstract run(): void;
  shared() {}
}

export const helpers = {
  format() {},
};

export namespace Tools {
  export class Inner {
    static make() {}
  }
}

export function outer() {
  return class Local {
    static build() {}
  };
}
`

// memberOtherProducerSource is a non-entry file of the same package.
const memberOtherProducerSource = `export default class Other {
  static make() {}
}

export class Util {
  static make() {}
}
`

// writeMemberProducer writes the member producer package and returns its root
// and the paths of its entry and non-entry files.
func writeMemberProducer(t *testing.T) (string, string, string) {
	t.Helper()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "@acme/log", "main": "dist/index.js"}`)
	entryPath := filepath.Join(repoRoot, "src", "index.ts")
	writeTestFile(t, entryPath, memberProducerSource)
	otherPath := filepath.Join(repoRoot, "src", "other.ts")
	writeTestFile(t, otherPath, memberOtherProducerSource)
	return repoRoot, entryPath, otherPath
}

// assertMethodKeys checks the export_name of each (name, line) method; an
// empty want means no key.
func assertMethodKeys(t *testing.T, payload map[string]any, cases []methodKeyCase) {
	t.Helper()

	for _, tc := range cases {
		wantID := ""
		if tc.wantExport != "" {
			wantID = "@acme/log"
		}
		if gotID, gotExport := methodKeys(t, payload, tc.name, tc.line); gotID != wantID || gotExport != tc.wantExport {
			t.Errorf("method %q line %d keys = (%q, %q), want (%q, %q)", tc.name, tc.line, gotID, gotExport, wantID, tc.wantExport)
		}
	}
}

type methodKeyCase struct {
	name       string
	line       int
	wantExport string
}

func TestDefaultEngineParsePathTypeScriptStampsProducerStaticMethodKeys(t *testing.T) {
	t.Parallel()

	repoRoot, entryPath, otherPath := writeMemberProducer(t)
	entry := parsePackageKeyFixture(t, repoRoot, entryPath)
	assertMethodKeys(t, entry, []methodKeyCase{
		{"create", 2, "Logger.create"},
		{"load", 11, "Logger.load"},
		{"connect", 21, "default.connect"},
		// Private, protected, #private, accessors, the constructor, a computed
		// name, a non-exported class, an object literal, a namespace member, and
		// a class expression are not package methods another package can call.
		{"secret", 4, ""},
		{"guarded", 5, ""},
		{"level", 7, ""},
		{"level", 8, ""},
		{"constructor", 9, ""},
		{"create", 16, ""},
		{"warn", 17, ""},
		{"format", 31, ""},
		{"make", 36, ""},
		{"build", 42, ""},
	})
	for _, item := range entry["functions"].([]map[string]any) {
		if name, _ := item["name"].(string); name == "#hidden" || name == "computed" || name == `["computed"]` {
			if exportName, _ := item["export_name"].(string); exportName != "" {
				t.Errorf("method %q export_name = %q, want none", name, exportName)
			}
		}
	}

	// A default export outside the main or module entry file is not the
	// package default, so its statics stay unkeyed; a named class still keys.
	other := parsePackageKeyFixture(t, repoRoot, otherPath)
	assertMethodKeys(t, other, []methodKeyCase{{"make", 2, ""}, {"make", 6, "Util.make"}})
}

func TestDefaultEngineParsePathTypeScriptSkipsProducerMethodKeysUnderFixtureManifest(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "@acme/log"}`)
	writeTestFile(t, filepath.Join(repoRoot, "tests", "fixtures", "app", "package.json"), `{"name": "@acme/log"}`)
	filePath := filepath.Join(repoRoot, "tests", "fixtures", "app", "src", "logger.ts")
	writeTestFile(t, filePath, `export class Logger {
  static create() {}
  warn() {}
}
`)

	got := parsePackageKeyFixture(t, repoRoot, filePath)
	for _, tc := range []struct {
		name string
		line int
	}{{"create", 2}, {"warn", 3}} {
		if gotID, gotExport := methodKeys(t, got, tc.name, tc.line); gotID != "" || gotExport != "" {
			t.Errorf("fixture method %q keys = (%q, %q), want none", tc.name, gotID, gotExport)
		}
	}
}

// memberConsumerSource is the consumer file the member key tests parse.
const memberConsumerSource = `import { Logger, Logger as AliasLogger } from "@acme/log";
import Client from "@acme/log";
import * as log from "@acme/log";
import type { Base } from "@acme/log";
import { Undeclared } from "not-declared";
import { Dup } from "@acme/log";
import { Dup } from "@acme/other";

function earlier(early: unknown) {
  return early;
}

export function run(input: unknown) {
  const early = new Logger();
  early.warn("e");
  Logger.create("a");
  AliasLogger.create("b");
  Client.connect();
  log.Logger.create("c");
  const logger = new Logger();
  logger.warn("x");
  const client = new Client();
  client.send();
  const nsLogger = new log.Logger();
  nsLogger.warn("y");
  let mutable = new Logger();
  mutable.warn("z");
  const shadowed = new Logger();
  shadowed.warn("s");
  const fromCall = Logger.create("q");
  fromCall.warn("q");
  const plain = new Undeclared();
  plain.run();
  Undeclared.make();
  Base.make();
  Dup.make();
  logger.warn.call(null);
  obj.warn();
  log.deep.chain.call();
}

function withParameter(shadowed: Logger) {
  return shadowed;
}
`

// parseMemberConsumer parses memberConsumerSource under a manifest that
// declares both producer packages.
func parseMemberConsumer(t *testing.T) map[string]any {
	t.Helper()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {
  "@acme/log": "1", "@acme/other": "1"}}`)
	filePath := filepath.Join(repoRoot, "src", "app.ts")
	writeTestFile(t, filePath, memberConsumerSource)
	return parsePackageKeyFixture(t, repoRoot, filePath)
}

// assertCallKeys checks the package_export_symbol of each function_call.
func assertCallKeys(t *testing.T, payload map[string]any, cases [][2]string) {
	t.Helper()

	for _, tc := range cases {
		if key := callPackageKey(t, payload, tc[0], "function_call"); key != tc[1] {
			t.Errorf("%s package_export_symbol = %q, want %q", tc[0], key, tc[1])
		}
	}
}

func TestDefaultEngineParsePathTypeScriptStampsConsumerStaticMemberKeys(t *testing.T) {
	t.Parallel()

	assertCallKeys(t, parseMemberConsumer(t), [][2]string{
		{"Logger.create", "package:@acme/log#Logger.create"},
		{"AliasLogger.create", "package:@acme/log#Logger.create"},
		{"Client.connect", "package:@acme/log#default.connect"},
		{"log.Logger.create", "package:@acme/log#Logger.create"},
		// An undeclared package, a type-only import, a name bound to two
		// packages, a deeper chain, and a non-import receiver stay unkeyed.
		{"Undeclared.make", ""},
		{"Base.make", ""},
		{"Dup.make", ""},
		{"logger.warn.call", ""},
		{"obj.warn", ""},
		{"log.deep.chain.call", ""},
	})
}

// shadowProducerSource declares methods that a field, a parameter property, or
// a `this.<name> =` write of the same name shadows.
const shadowProducerSource = `export class Shadowed {
  handler = () => {};
  static make = 1;
  constructor(public onReady: () => void) {
    this.reset = () => {};
  }
  handler() {}
  reset() {}
  onReady() {}
  static make() {}
  static boot() {}
  boot() {}
  plain() {}
}

export class Base {
  warn() {}
}

export class Sub extends Base {
  own() {}
}
`

// parseShadowProducer parses shadowProducerSource and a plain JavaScript
// class whose field shadows a method.
func parseShadowProducer(t *testing.T) (map[string]any, map[string]any) {
	t.Helper()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "@acme/log"}`)
	filePath := filepath.Join(repoRoot, "src", "shadowed.ts")
	writeTestFile(t, filePath, shadowProducerSource)
	// Plain JavaScript spells a class field as field_definition.
	jsPath := filepath.Join(repoRoot, "src", "shadowed.js")
	writeTestFile(t, jsPath, "export class Plain {\n  handler = () => {};\n  handler() {}\n  kept() {}\n}\n")
	return parsePackageKeyFixture(t, repoRoot, filePath), parsePackageKeyFixture(t, repoRoot, jsPath)
}

// TestDefaultEngineParsePathTypeScriptSkipsShadowedMethodKeys pins the
// shadowing rule: a class field (static or not), a TypeScript parameter
// property, or a `this.<name> =` write of the same name replaces the method,
// so a call by that name may never reach it and the method gets no key.
func TestDefaultEngineParsePathTypeScriptSkipsShadowedMethodKeys(t *testing.T) {
	t.Parallel()

	ts, js := parseShadowProducer(t)
	assertMethodKeys(t, ts, []methodKeyCase{
		{"handler", 7, ""},
		{"reset", 8, ""},
		{"onReady", 9, ""},
		{"make", 10, ""},
		{"boot", 11, "Shadowed.boot"},
	})
	assertMethodKeys(t, js, []methodKeyCase{{"handler", 3, ""}})
}

// TestDefaultEngineParsePathTypeScriptSkipsStaticMembersOfShadowedImports is
// the #8280 F1 sweep for the static and default shapes: when the file binds
// the imported name again anywhere (a parameter, a catch or for-of binding, a
// destructuring pattern, a class or function name, or a const in another
// function), `C.m()` may read the local binding, so it stays unkeyed. A
// `this.x` receiver is never keyed.
func TestDefaultEngineParsePathTypeScriptSkipsStaticMembersOfShadowedImports(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		source   string
		fullName string
	}{
		{"parameter", "import { Logger } from \"@acme/log\";\nfunction f(Logger: unknown) { return Logger; }\nLogger.create();\n", "Logger.create"},
		{"catch binding", "import { Logger } from \"@acme/log\";\ntry { f(); } catch (Logger) { g(); }\nLogger.create();\n", "Logger.create"},
		{"for-of binding", "import { Logger } from \"@acme/log\";\nfor (const Logger of list) { g(); }\nLogger.create();\n", "Logger.create"},
		{"destructuring parameter", "import { Logger } from \"@acme/log\";\nfunction f({ Logger }: any) { return Logger; }\nLogger.create();\n", "Logger.create"},
		{"local class", "import { Logger } from \"@acme/log\";\nfunction f() { class Logger {} return Logger; }\nLogger.create();\n", "Logger.create"},
		{"local function", "import { Logger } from \"@acme/log\";\nfunction g() { function Logger() {} return Logger; }\nLogger.create();\n", "Logger.create"},
		{"default name in another function", "import Client from \"@acme/log\";\nfunction f() { const Client = 1; return Client; }\nClient.connect();\n", "Client.connect"},
		{"namespace parameter", "import * as log from \"@acme/log\";\nfunction f(log: unknown) { return log; }\nlog.Logger.create();\n", "log.Logger.create"},
		{"this receiver", "import { Logger } from \"@acme/log\";\nclass A {\n  log = new Logger();\n  run() { this.log.warn(); }\n}\n", "this.log.warn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {"@acme/log": "1"}}`)
			filePath := filepath.Join(repoRoot, "src", "shadow.ts")
			writeTestFile(t, filePath, tc.source)
			if key := callPackageKey(t, parsePackageKeyFixture(t, repoRoot, filePath), tc.fullName, "function_call"); key != "" {
				t.Errorf("%s package_export_symbol = %q, want none", tc.fullName, key)
			}
		})
	}
}

// TestDefaultEngineParsePathSkipsMemberKeysTheOpenRuleMisses pins the #8310
// repros for the #8280 static member keys (ruling (B)): an import alias, a
// using declaration, a `\u` escape, or a sloppy-mode eval var can rebind the
// import name where the #7601 open rule does not look, so `Logger.create()`
// stays unkeyed. The #7601 plain call `Logger()` keeps its key exactly as on
// main, hole included; #8310 tracks closing it.
func TestDefaultEngineParsePathSkipsMemberKeysTheOpenRuleMisses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		ext    string
		source string
	}{
		{"import alias", ".ts", "namespace N {\n  import Logger = Other.Thing;\n  Logger.create();\n  Logger();\n}\n"},
		{"using declaration", ".ts", "{\n  using Logger = res();\n  Logger.create();\n  Logger();\n}\n"},
		{"unicode escape", ".ts", "{\n  let \\u004cogger = other;\n  Logger.create();\n  Logger();\n}\n"},
		{"sloppy eval var", ".js", "function g() {\n  eval(\"var Logger = other\");\n  Logger.create();\n  Logger();\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoRoot := t.TempDir()
			writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {"@acme/log": "1"}}`)
			filePath := filepath.Join(repoRoot, "src", "repro"+tc.ext)
			writeTestFile(t, filePath, "import { Logger } from \"@acme/log\";\n"+tc.source)
			got := parsePackageKeyFixture(t, repoRoot, filePath)
			if key := callPackageKey(t, got, "Logger.create", "function_call"); key != "" {
				t.Errorf("Logger.create package_export_symbol = %q, want none", key)
			}
			if key := callPackageKey(t, got, "Logger", "function_call"); key != "package:@acme/log#Logger" {
				t.Errorf("Logger() package_export_symbol = %q, want the #7601 key package:@acme/log#Logger, unchanged from main", key)
			}
		})
	}
}

// TestDefaultEngineParsePathSkipsMemberKeysInFilesWithWith pins review F8 for
// the #8280 member keys: inside a sloppy-mode `with (o)` block a name may
// resolve to a property of o, so a file with a with statement keys no member
// call. The #7601 call keeps its key as on main. The word in a comment, a
// string, or an identifier is no with statement.
func TestDefaultEngineParsePathSkipsMemberKeysInFilesWithWith(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "package.json"), `{"name": "consumer-app", "dependencies": {"@acme/log": "1"}}`)
	withPath := filepath.Join(repoRoot, "src", "legacy.js")
	writeTestFile(t, withPath, "const { Logger } = require(\"@acme/log\");\n\nfunction a(o) {\n  with (o) {\n    Logger.create(\"with\");\n    Logger();\n  }\n}\n")
	got := parsePackageKeyFixture(t, repoRoot, withPath)
	if key := callPackageKey(t, got, "Logger.create", "function_call"); key != "" {
		t.Errorf("Logger.create package_export_symbol = %q, want none in a file with a with statement", key)
	}
	if key := callPackageKey(t, got, "Logger", "function_call"); key != "package:@acme/log#Logger" {
		t.Errorf("Logger() package_export_symbol = %q, want the #7601 key unchanged", key)
	}

	plainPath := filepath.Join(repoRoot, "src", "plain.js")
	writeTestFile(t, plainPath, "const { Logger } = require(\"@acme/log\");\n// with (o) is not used here\nconst without = \"with (x)\";\nLogger.create(without);\n")
	if key := callPackageKey(t, parsePackageKeyFixture(t, repoRoot, plainPath), "Logger.create", "function_call"); key != "package:@acme/log#Logger.create" {
		t.Errorf("Logger.create package_export_symbol = %q, want package:@acme/log#Logger.create", key)
	}
}
