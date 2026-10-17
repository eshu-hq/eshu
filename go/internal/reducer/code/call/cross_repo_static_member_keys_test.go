// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// The #8280 JS/TS static member keys, driven end to end from real parser
// output through ReferencedSymbolKeys and ExtractRows: `C.m()` on a named
// import and `D.m()` on a default import resolve to the producer's static
// method only when exactly one repository defines the key.

const memberKeyProducerIndex = `export class Logger {
  static create() { return new Logger(); }
  warn(message: string) {}
}

export default class Client {
  static connect() {}
  send() {}
}

class Hidden {
  static conceal() {}
}

export class Sub extends Logger {}
`

const memberKeyConsumerApp = `import { Logger, Sub } from "@acme/log";
import Client from "@acme/log";

export function run() {
  Logger.create();
  const logger = new Logger();
  logger.warn("x");
  Client.connect();
  const client = new Client();
  client.send();
}

export function runSub() {
  const sub = new Sub();
  sub.warn("inherited");
}
`

func memberKeyJavaScriptProducer(t *testing.T, repoID string) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, repoID, map[string]string{
		"package.json": `{"name": "@acme/log", "main": "dist/index.js"}`,
		"src/index.ts": memberKeyProducerIndex,
	})
}

// memberKeyJavaScriptConsumer also declares its own warn and send, so an
// unresolved member key must not link to them (#7610).
func memberKeyJavaScriptConsumer(t *testing.T) packageKeyRepo {
	t.Helper()
	return parsePackageKeyRepo(t, "repo-app", map[string]string{
		"package.json":   `{"name": "acme-app", "dependencies": {"@acme/log": "1.0.0"}}`,
		"src/app.ts":     memberKeyConsumerApp,
		"src/helpers.ts": "export function warn() {}\nexport function send() {}\n",
	})
}

func assertImportBindingRows(t *testing.T, from map[string]map[string]any, caller string, callees []string) {
	t.Helper()
	for _, callee := range callees {
		row, ok := from[callee]
		if !ok {
			t.Fatalf("no CALLS row %s -> %s; rows = %#v", caller, callee, from)
		}
		if got := payloadcore.AnyToString(row["resolution_method"]); got != string(codeprovenance.MethodImportBinding) {
			t.Errorf("%s resolution_method = %q, want %q", callee, got, codeprovenance.MethodImportBinding)
		}
		if got := payloadcore.AnyToString(row["repo_id"]); got != "repo-app" {
			t.Errorf("%s repo_id = %q, want repo-app", callee, got)
		}
	}
}

// assertReferencedKeys fails unless the consumer's file facts reference every
// key in want.
func assertReferencedKeys(t *testing.T, consumer packageKeyRepo, want []string) {
	t.Helper()
	keys := shared.ReferencedSymbolKeys(consumer.envelopes)
	for _, key := range want {
		if !slices.Contains(keys, key) {
			t.Errorf("ReferencedSymbolKeys = %v, want it to hold %q", keys, key)
		}
	}
}

// assertNoRowsTo fails when caller has a CALLS row to any forbidden callee.
func assertNoRowsTo(t *testing.T, rows []map[string]any, caller string, forbidden []string) {
	t.Helper()
	from := callRowsFrom(rows, caller)
	for _, callee := range forbidden {
		if _, ok := from[callee]; ok {
			t.Errorf("%s resolved to %s, want no row", caller, callee)
		}
	}
}

func TestExtractRowsResolvesParsedJavaScriptStaticMemberKeysAcrossRepositories(t *testing.T) {
	t.Parallel()

	consumer := memberKeyJavaScriptConsumer(t)
	producer := memberKeyJavaScriptProducer(t, "repo-log")
	assertReferencedKeys(t, consumer, []string{"package:@acme/log#Logger.create", "package:@acme/log#default.connect"})

	_, rows := ExtractRows(append(consumer.envelopes, producer.envelopes...))
	assertImportBindingRows(t, callRowsFrom(rows, "uid:repo-app:run"), "run", []string{"uid:repo-log:create", "uid:repo-log:connect"})
	assertNoRowsTo(t, rows, "uid:repo-app:run", []string{"uid:repo-log:conceal"})
}

func TestExtractRowsLeavesParsedJavaScriptStaticMemberKeysUnresolvedWhenTwoProducersShareTheName(t *testing.T) {
	t.Parallel()

	consumer := memberKeyJavaScriptConsumer(t)
	first := memberKeyJavaScriptProducer(t, "repo-log-a")
	second := memberKeyJavaScriptProducer(t, "repo-log-b")

	_, rows := ExtractRows(append(append(consumer.envelopes, first.envelopes...), second.envelopes...))
	assertNoRowsTo(t, rows, "uid:repo-app:run", []string{
		"uid:repo-log-a:create", "uid:repo-log-b:create", "uid:repo-log-a:connect", "uid:repo-log-b:connect",
	})
}
