// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// ES2022 allows a string literal wherever a module export name is spelled
// (`export { a as 'from' }`, `import { 'b c' as d }`). The recorded name is the
// string's value, so the quote style never changes which symbol is named
// (issue #7461).

func TestDefaultEngineParsePathReExportStringLiteralNamesAreUnquoted(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{"ts", "js"} {
		ext := ext
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			got := parseQuotedModuleNamesFixture(t, "barrel."+ext, `export { a as 'from' } from './quoted';
export { "x y" as "z w" } from './spaced';
export { 'same' as singleAlias } from './single';
export { "same" as doubleAlias } from './double';
export { plain as bare } from './plain';
export { e as '\u0066rom2' } from './escaped';
export { '\u0067one' as g } from './escaped-original';
export { 'a' as 'b c' } from './attributes' with { type: 'json' };
export { '' as emptyOriginal } from './empty';
`)

			// An empty original name would be read by the reducer as the exported
			// name, resolving to the wrong symbol, so it is not recorded.
			for _, item := range got["imports"].([]map[string]any) {
				if item["name"] == "emptyOriginal" {
					t.Fatalf("empty-original re-export was recorded: %#v", item)
				}
			}

			from := findNamedBucketItem(t, got, "imports", "from")
			assertStringFieldValue(t, from, "source", "./quoted")
			assertStringFieldValue(t, from, "import_type", "reexport")
			assertStringFieldValue(t, from, "original_name", "a")

			spaced := findNamedBucketItem(t, got, "imports", "z w")
			assertStringFieldValue(t, spaced, "source", "./spaced")
			assertStringFieldValue(t, spaced, "original_name", "x y")

			single := findNamedBucketItem(t, got, "imports", "singleAlias")
			assertStringFieldValue(t, single, "original_name", "same")
			double := findNamedBucketItem(t, got, "imports", "doubleAlias")
			assertStringFieldValue(t, double, "original_name", "same")

			bare := findNamedBucketItem(t, got, "imports", "bare")
			assertStringFieldValue(t, bare, "original_name", "plain")

			// An escaped spelling names the same symbol as the plain one.
			escapedExport := findNamedBucketItem(t, got, "imports", "from2")
			assertStringFieldValue(t, escapedExport, "source", "./escaped")
			assertStringFieldValue(t, escapedExport, "original_name", "e")
			escapedOriginal := findNamedBucketItem(t, got, "imports", "g")
			assertStringFieldValue(t, escapedOriginal, "original_name", "gone")

			// The attribute re-export recovery reads names through the same path.
			attributes := findNamedBucketItem(t, got, "imports", "b c")
			assertStringFieldValue(t, attributes, "source", "./attributes")
			assertStringFieldValue(t, attributes, "original_name", "a")
		})
	}
}

func TestDefaultEngineParsePathImportStringLiteralNamesAreUnquoted(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{"ts", "js"} {
		ext := ext
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			got := parseQuotedModuleNamesFixture(t, "consumer."+ext, `import { 'b c' as d } from './m';
import { "x y" as z } from './n';
import { 'same' as singleLocal } from './single';
import { "same" as doubleLocal } from './double';
import { 'a\u0062' as escaped } from './escaped';
import { '\u0066rom' as fromLocal } from './from';
import { plain as bare } from './plain';
import { ' x ' as spaced } from './whitespace';
`)

			// A name with surrounding whitespace would be trimmed to "x" by the
			// reducer and resolve as a different symbol, so it is skipped.
			for _, item := range got["imports"].([]map[string]any) {
				if item["alias"] == "spaced" {
					t.Fatalf("whitespace-bearing import name was recorded: %#v", item)
				}
			}

			bc := findNamedBucketItem(t, got, "imports", "b c")
			assertStringFieldValue(t, bc, "source", "./m")
			assertStringFieldValue(t, bc, "alias", "d")

			xy := findNamedBucketItem(t, got, "imports", "x y")
			assertStringFieldValue(t, xy, "source", "./n")
			assertStringFieldValue(t, xy, "alias", "z")

			assertStringFieldValue(t, importNamedBySource(t, got, "same", "./single"), "alias", "singleLocal")
			assertStringFieldValue(t, importNamedBySource(t, got, "same", "./double"), "alias", "doubleLocal")

			escaped := findNamedBucketItem(t, got, "imports", "ab")
			assertStringFieldValue(t, escaped, "alias", "escaped")

			fromLocal := findNamedBucketItem(t, got, "imports", "from")
			assertStringFieldValue(t, fromLocal, "source", "./from")
			assertStringFieldValue(t, fromLocal, "alias", "fromLocal")

			bare := findNamedBucketItem(t, got, "imports", "plain")
			assertStringFieldValue(t, bare, "alias", "bare")
		})
	}
}

// A specifier whose name cannot be recorded faithfully is skipped rather than
// recorded under a different symbol: a name or export alias with an undecodable
// escape. A string-literal import alias (`as ' padded '`) is not valid ECMAScript:
// the grammar takes only an identifier there, so error recovery records no row for
// it, which this test pins as well. A name or alias with an undecodable escape (its raw body is also the value of a different,
// valid literal), and an undecodable re-export name.
func TestDefaultEngineParsePathSkipsUnrecordableModuleNames(t *testing.T) {
	t.Parallel()

	got := parseQuotedModuleNamesFixture(t, "unrecordable.ts", `import { a as ' padded ' } from './padded-alias';
import { '\ud800' as b } from './lone-surrogate';
import { c as '\ud800' } from './lone-surrogate-alias';
import { kept as keptLocal } from './kept';
export { '\ud800' as d } from './lone-surrogate-export';
export { e as '\ud800' } from './lone-surrogate-export-alias';
`)

	items, ok := got["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	}
	for _, item := range items {
		source, _ := item["source"].(string)
		if item["name"] == source {
			// The module's own import row stays; only specifier rows are skipped.
			continue
		}
		switch source {
		case "./padded-alias", "./lone-surrogate", "./lone-surrogate-alias",
			"./lone-surrogate-export", "./lone-surrogate-export-alias":
			t.Fatalf("unrecordable specifier was recorded: %#v", item)
		}
	}
	assertStringFieldValue(t, importNamedBySource(t, got, "kept", "./kept"), "alias", "keptLocal")
}

// A name spelled with a UTF-16 surrogate-pair escape is the same symbol as the
// name spelled directly: both read as the one code point.
func TestDefaultEngineParsePathSurrogatePairEscapeNamesMatchLiteralSpelling(t *testing.T) {
	t.Parallel()

	got := parseQuotedModuleNamesFixture(t, "emoji.ts", `export { x as '\ud83d\ude00' } from './escaped';
export { y as '😀' } from './literal';
`)

	items, ok := got["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	}
	sources := make([]string, 0, 2)
	for _, item := range items {
		if item["name"] == "😀" {
			source, _ := item["source"].(string)
			sources = append(sources, source)
		}
	}
	if want := []string{"./escaped", "./literal"}; !reflect.DeepEqual(sources, want) {
		t.Fatalf("sources naming 😀 = %v, want %v (escape and literal spelling must agree)", sources, want)
	}
}

func TestDefaultEngineParsePathEmbeddedShellQuotedImportNames(t *testing.T) {
	t.Parallel()

	got := parseQuotedModuleNamesFixture(t, "runner.js", `import { 'execSync' as single } from "node:child_process";
import { "spawn" as double } from "child_process";

function build() {
  single("make");
}

function deploy() {
  double("kubectl");
}
`)
	commands, ok := got["embedded_shell_commands"].([]map[string]any)
	if !ok {
		t.Fatalf("embedded_shell_commands = %T, want []map[string]any", got["embedded_shell_commands"])
	}
	apis := make([]string, 0, len(commands))
	for _, command := range commands {
		api, _ := command["api"].(string)
		apis = append(apis, api)
	}
	want := []string{"child_process.execSync", "child_process.spawn"}
	if !reflect.DeepEqual(apis, want) {
		t.Fatalf("embedded shell apis = %#v, want %#v", apis, want)
	}
}

// A string-literal import alias is not valid ECMAScript (the grammar takes an
// identifier), so the embedded-shell reader never sees an empty alias: the import
// below binds no execSync local and records no shell command.
func TestDefaultEngineParsePathEmbeddedShellStringImportAliasRecordsNothing(t *testing.T) {
	t.Parallel()

	got := parseQuotedModuleNamesFixture(t, "string_alias.js", `import { 'execSync' as '' } from "node:child_process";

function build() {
  execSync("make");
}
`)
	if commands, _ := got["embedded_shell_commands"].([]map[string]any); len(commands) != 0 {
		t.Fatalf("embedded shell commands = %#v, want none for a string import alias", commands)
	}
}

func parseQuotedModuleNamesFixture(t *testing.T, name string, body string) map[string]any {
	t.Helper()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, name)
	writeTestFile(t, filePath, body)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	return got
}

// importNamedBySource returns the imports row with the given name and module
// source; two imports of the same name from different modules must both be
// found by their own source.
func importNamedBySource(t *testing.T, payload map[string]any, name string, source string) map[string]any {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		if item["name"] == name && item["source"] == source {
			return item
		}
	}
	t.Fatalf("imports has no row name=%q source=%q in %#v", name, source, items)
	return nil
}
