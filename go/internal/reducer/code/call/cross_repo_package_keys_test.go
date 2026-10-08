// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// The #7601 cross-repository JS/TS join, driven end to end from real parser
// output: the producer's package_id/export_name and the consumer's
// package_export_symbol are whatever the JavaScript parser emits, so these
// tests fail if either side's keys drift apart.

// packageKeyRepo is one parsed repository: its id and its file envelopes.
type packageKeyRepo struct {
	repoID    string
	envelopes []facts.Envelope
}

// parsePackageKeyRepo writes files under a fresh root, parses each through the
// default engine, and gives every function and class a stable uid of the form
// uid:<repoID>:<name>.
func parsePackageKeyRepo(t *testing.T, repoID string, files map[string]string) packageKeyRepo {
	t.Helper()

	root := t.TempDir()
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	for relativePath, body := range files {
		writeReducerTestFile(t, filepath.Join(root, relativePath), body)
	}
	repo := packageKeyRepo{repoID: repoID}
	for relativePath := range files {
		if filepath.Ext(relativePath) == ".json" {
			continue
		}
		path := filepath.Join(root, relativePath)
		payload, err := engine.ParsePath(root, path, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", relativePath, err)
		}
		for _, bucket := range []string{"functions", "classes"} {
			items, _ := payload[bucket].([]map[string]any)
			for _, item := range items {
				item["uid"] = "uid:" + repoID + ":" + payloadcore.AnyToString(item["name"])
			}
		}
		repo.envelopes = append(repo.envelopes, facts.Envelope{FactKind: "file", Payload: map[string]any{
			"repo_id":          repoID,
			"relative_path":    relativePath,
			"parsed_file_data": payload,
		}})
	}
	return repo
}

const packageKeyProducerIndex = `export function formatPrice(value: number) {
  return round(value);
}

function round(value: number) {
  return value;
}

export class PriceFormatter {}
`

const packageKeyConsumerPage = `import { formatPrice, PriceFormatter } from "@acme/format";
import * as fmt from "@acme/format";
import type { Money } from "@acme/format";

export function renderPage(amount: Money) {
  formatPrice(amount);
  fmt.formatPrice(amount);
  round(amount);
  return new PriceFormatter();
}
`

// packageKeyConsumer also declares its own round, the same name as the
// producer's private helper, so the call to it must stay in the consumer.
func packageKeyConsumer(t *testing.T) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, "repo-app", map[string]string{
		"package.json":   `{"name": "acme-app", "dependencies": {"@acme/format": "1.0.0"}}`,
		"src/page.ts":    packageKeyConsumerPage,
		"src/helpers.ts": "export function round(value: number) { return value; }\n",
	})
}

// callRowsFrom returns the CALLS rows of one caller keyed by callee. A
// constructor call also yields an INSTANTIATES row, which is left out.
func callRowsFrom(rows []map[string]any, callerID string) map[string]map[string]any {
	byCallee := map[string]map[string]any{}
	for _, row := range rows {
		relationship := payloadcore.AnyToString(row["relationship_type"])
		if payloadcore.AnyToString(row["caller_entity_id"]) == callerID && (relationship == "" || relationship == "CALLS") {
			byCallee[payloadcore.AnyToString(row["callee_entity_id"])] = row
		}
	}
	return byCallee
}

func TestExtractRowsResolvesParsedJavaScriptPackageImportAcrossRepositories(t *testing.T) {
	t.Parallel()

	producer := parsePackageKeyRepo(t, "repo-format", map[string]string{
		"package.json": `{"name": "@acme/format", "main": "dist/index.js"}`,
		"src/index.ts": packageKeyProducerIndex,
	})
	consumer := packageKeyConsumer(t)

	_, rows := ExtractRows(append(consumer.envelopes, producer.envelopes...))
	fromPage := callRowsFrom(rows, "uid:repo-app:renderPage")

	for _, callee := range []string{"uid:repo-format:formatPrice", "uid:repo-format:PriceFormatter"} {
		row, ok := fromPage[callee]
		if !ok {
			t.Fatalf("no CALLS row renderPage -> %s; rows from renderPage = %#v", callee, fromPage)
		}
		if got := payloadcore.AnyToString(row["resolution_method"]); got != string(codeprovenance.MethodImportBinding) {
			t.Errorf("%s resolution_method = %q, want %q", callee, got, codeprovenance.MethodImportBinding)
		}
		if got := payloadcore.AnyToString(row["repo_id"]); got != "repo-app" {
			t.Errorf("%s repo_id = %q, want repo-app", callee, got)
		}
	}
	// round is not imported from the package: it stays on the consumer's own
	// helper, and the producer's private round gains no cross-repo caller.
	if _, ok := fromPage["uid:repo-format:round"]; ok {
		t.Errorf("renderPage resolved to the producer's non-exported round: %#v", fromPage)
	}
	if _, ok := fromPage["uid:repo-app:round"]; !ok {
		t.Errorf("renderPage lost its call to the consumer's own round: %#v", fromPage)
	}
}

func TestExtractRowsLeavesParsedJavaScriptPackageImportUnresolvedWhenTwoProducersShareTheName(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"package.json": `{"name": "@acme/format", "main": "dist/index.js"}`,
		"src/index.ts": packageKeyProducerIndex,
	}
	first := parsePackageKeyRepo(t, "repo-format-a", files)
	second := parsePackageKeyRepo(t, "repo-format-b", files)
	consumer := packageKeyConsumer(t)

	envelopes := append(append(consumer.envelopes, first.envelopes...), second.envelopes...)
	_, rows := ExtractRows(envelopes)
	for callee := range callRowsFrom(rows, "uid:repo-app:renderPage") {
		switch callee {
		case "uid:repo-format-a:formatPrice", "uid:repo-format-b:formatPrice",
			"uid:repo-format-a:PriceFormatter", "uid:repo-format-b:PriceFormatter":
			t.Errorf("ambiguous package key resolved to %s, want no cross-repository row", callee)
		}
	}
}

// An npm alias dependency resolves the consumer's call to the producer that
// publishes the TARGET name: the parser keys the call package:bar#widget for
// `"foo": "npm:bar@1"`, and the reducer joins it to bar's export (#7613).
func TestExtractRowsResolvesNpmAliasImportToTargetProducer(t *testing.T) {
	t.Parallel()

	producer := parsePackageKeyRepo(t, "repo-lib", map[string]string{
		"package.json": `{"name": "lib", "main": "dist/index.js"}`,
		"src/index.js": "export function widget() { return 1; }\n",
	})
	consumer := parsePackageKeyRepo(t, "repo-app", map[string]string{
		"package.json": `{"name": "acme-app", "dependencies": {"w": "npm:lib@1.0.0"}}`,
		"src/page.js": `import { widget } from "w";

export function render() {
  widget();
}
`,
	})

	_, rows := ExtractRows(append(consumer.envelopes, producer.envelopes...))
	fromRender := callRowsFrom(rows, "uid:repo-app:render")
	row, ok := fromRender["uid:repo-lib:widget"]
	if !ok {
		t.Fatalf("no CALLS row render -> uid:repo-lib:widget; rows from render = %#v", fromRender)
	}
	if got := payloadcore.AnyToString(row["resolution_method"]); got != string(codeprovenance.MethodImportBinding) {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
}

// A keyed call whose package no producer publishes must not fall back to the
// consumer's own same-named function: the call is bound to an explicitly
// imported but unresolvable target, and the key's package is not one of the
// consumer repository's own manifest names (#7610).
func TestExtractRowsLeavesUnresolvedExternalPackageKeyOffLocalName(t *testing.T) {
	t.Parallel()

	consumer := parsePackageKeyRepo(t, "repo-app", map[string]string{
		"package.json": `{"name": "acme-app", "dependencies": {"@acme/format": "1.0.0"}}`,
		"src/page.ts": `import { formatPrice } from "@acme/format";

export function render(amount: number) {
  return formatPrice(amount);
}
`,
		"src/money.ts": "export function formatPrice(amount: number) { return amount; }\n",
	})

	_, rows := ExtractRows(consumer.envelopes)
	if fromRender := callRowsFrom(rows, "uid:repo-app:render"); len(fromRender) != 0 {
		t.Errorf("unresolved external key gained rows from render: %#v, want none", fromRender)
	}
}

// The #7610 carve-out, end to end: the key's package is a same-repository
// workspace package whose export list the parser does not key, so no symbol
// key joins the call and the repo-unique fallback to the workspace definition
// is the true edge.
func TestExtractRowsKeepsWorkspacePackageFallbackForUnkeyedExportList(t *testing.T) {
	t.Parallel()

	mono := parsePackageKeyRepo(t, "repo-mono", map[string]string{
		"package.json":                  `{"name": "mono-root", "private": true}`,
		"packages/app/package.json":     `{"name": "@acme/app", "dependencies": {"@acme/widgets": "1.0.0"}}`,
		"packages/app/src/page.js":      "import { widget } from \"@acme/widgets\";\n\nexport function render() {\n  return widget();\n}\n",
		"packages/widgets/package.json": `{"name": "@acme/widgets", "main": "index.js"}`,
		"packages/widgets/index.js":     "function widget() { return 1; }\n\nexport { widget };\n",
	})

	_, rows := ExtractRows(mono.envelopes)
	fromRender := callRowsFrom(rows, "uid:repo-mono:render")
	row, ok := fromRender["uid:repo-mono:widget"]
	if !ok {
		t.Fatalf("no CALLS row render -> uid:repo-mono:widget; rows from render = %#v", fromRender)
	}
	if got := payloadcore.AnyToString(row["resolution_method"]); got != string(codeprovenance.MethodRepoUniqueName) {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodRepoUniqueName)
	}
}

// Same carve-out with a CommonJS workspace producer: module.exports carries
// no export key either, so the fallback stays the true edge.
func TestExtractRowsKeepsWorkspacePackageFallbackForCommonJSProducer(t *testing.T) {
	t.Parallel()

	mono := parsePackageKeyRepo(t, "repo-cjs", map[string]string{
		"package.json":              `{"name": "cjs-root", "private": true}`,
		"packages/app/package.json": `{"name": "@acme/cjs-app", "dependencies": {"@acme/cjs-lib": "1.0.0"}}`,
		"packages/app/src/main.js":  "const { helper } = require(\"@acme/cjs-lib\");\n\nexport function run() {\n  return helper();\n}\n",
		"packages/lib/package.json": `{"name": "@acme/cjs-lib", "main": "index.js"}`,
		"packages/lib/index.js":     "function helper() { return 2; }\n\nmodule.exports = { helper };\n",
	})

	_, rows := ExtractRows(mono.envelopes)
	fromRun := callRowsFrom(rows, "uid:repo-cjs:run")
	row, ok := fromRun["uid:repo-cjs:helper"]
	if !ok {
		t.Fatalf("no CALLS row run -> uid:repo-cjs:helper; rows from run = %#v", fromRun)
	}
	if got := payloadcore.AnyToString(row["resolution_method"]); got != string(codeprovenance.MethodRepoUniqueName) {
		t.Errorf("resolution_method = %q, want %q", got, codeprovenance.MethodRepoUniqueName)
	}
}

// A jsconfig baseUrl alias that shares its name with a declared dependency
// resolves inside the consumer repository, so the call must not gain a
// cross-repository row to the unrelated corpus publisher of that name (#7613).
func TestExtractRowsLeavesJSConfigAliasOnDeclaredNameInsideItsRepository(t *testing.T) {
	t.Parallel()

	producer := parsePackageKeyRepo(t, "repo-api", map[string]string{
		"package.json": `{"name": "api", "main": "dist/index.js"}`,
		"src/index.js": "export function getUser() { return 1; }\n",
	})
	consumer := parsePackageKeyRepo(t, "repo-app", map[string]string{
		"package.json":     `{"name": "web-app", "dependencies": {"api": "1.0.0"}}`,
		"jsconfig.json":    `{"compilerOptions": {"baseUrl": "src"}}`,
		"src/api/index.js": "export function getUser() { return 2; }\n",
		"src/page.jsx": `import { getUser } from "api";

export function render() {
  getUser();
}
`,
	})

	_, rows := ExtractRows(append(consumer.envelopes, producer.envelopes...))
	for callee := range callRowsFrom(rows, "uid:repo-app:render") {
		if callee == "uid:repo-api:getUser" {
			t.Errorf("jsconfig alias call resolved to the foreign publisher %s, want no cross-repository row", callee)
		}
	}
}
