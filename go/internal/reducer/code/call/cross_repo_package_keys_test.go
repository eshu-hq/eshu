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
