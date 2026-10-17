// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// These tests prove on real Postgres that the production loader, unchanged by
// #8280, loads the producer file facts for every new key form, one test per
// shape. Producer and consumer payloads come from parser.DefaultEngine, so the
// keys the consumer asks for are exactly the ones the parser stamps.

const (
	loaderJSManifest = `{"name": "@acme/log", "main": "dist/index.js"}`
	loaderJSProducer = "export class Logger {\n  static create() { return new Logger(); }\n}\n\n" +
		"export default class Client {\n  static connect() {}\n}\n"
	loaderJSConsumer = "import { Logger } from \"@acme/log\";\nimport Client from \"@acme/log\";\n\n" +
		"export function run() {\n  Logger.create();\n  Client.connect();\n}\n"
)

// seedLoaderJSProducer stores the JS producer as an active scope with its
// package.json.
func seedLoaderJSProducer(t *testing.T, ctx context.Context, database db.Executor, suffix string, now time.Time) {
	t.Helper()
	producer := parseLoaderFixture(t, map[string]string{"package.json": loaderJSManifest, "src/index.ts": loaderJSProducer}, "src/index.ts")
	scope, repo, generation := "scope:js-log"+suffix, "repository:r_js_log"+suffix, "generation-js-log"+suffix
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, scope, repo, generation, now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, repo, "package.json", loaderJSManifest, generation, now)
	seedParsedFileFact(t, ctx, database, "fact-js-log"+suffix, scope, generation, "src/index.ts", producer, now)
}

func loaderJSConsumerKeys(t *testing.T) []string {
	t.Helper()
	return callPackageKeys(parseLoaderFixture(t, map[string]string{
		"package.json": `{"name": "acme-app", "dependencies": {"@acme/log": "1.0.0"}}`,
		"src/app.ts":   loaderJSConsumer,
	}, "src/app.ts"))
}

// assertLoaderLoads fails unless consumerKeys holds each key and the loader
// returns exactly wantIDs for it.
func assertLoaderLoads(t *testing.T, ctx context.Context, store *FactStore, consumerKeys []string, want map[string][]string) {
	t.Helper()
	for key, wantIDs := range want {
		if !slices.Contains(consumerKeys, key) {
			t.Fatalf("consumer parse keys = %v, want %q among them", consumerKeys, key)
		}
		loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{key})
		if err != nil {
			t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(%q) error = %v, want nil", key, err)
		}
		if got := factIDs(loaded); !reflect.DeepEqual(got, wantIDs) {
			t.Errorf("LoadActiveCodeCallSymbolDefinitionFacts(%q) fact ids = %#v, want %#v", key, got, wantIDs)
		}
	}
}

func TestReducerContentionGateActiveCodeCallSymbolLoaderResolvesStaticMemberKeys(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedLoaderJSProducer(t, ctx, database, "", now)
	store := NewFactStore(SQLDB{DB: database})
	consumerKeys := loaderJSConsumerKeys(t)
	assertLoaderLoads(t, ctx, store, consumerKeys, map[string][]string{
		"package:@acme/log#Logger.create":   {"fact-js-log"},
		"package:@acme/log#default.connect": {"fact-js-log"},
	})

	// A mirror that publishes the same npm package makes the key ambiguous: the
	// loader returns both candidates, and the reducer resolves neither.
	seedLoaderJSProducer(t, ctx, database, "-mirror", now.Add(time.Second))
	assertLoaderLoads(t, ctx, store, consumerKeys, map[string][]string{
		"package:@acme/log#Logger.create": {"fact-js-log", "fact-js-log-mirror"},
	})
}

func TestReducerContentionGateActiveCodeCallSymbolLoaderResolvesPHPInstanceKeys(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	const manifest = `{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}`
	producer := parseLoaderFixture(t, map[string]string{
		"composer.json":  manifest,
		"src/Logger.php": "<?php\nnamespace Acme\\Logging;\n\nclass Logger\n{\n    public function info(string $message): void {}\n}\n",
	}, "src/Logger.php")
	consumer := parseLoaderFixture(t, map[string]string{
		"composer.json": `{"name": "acme/app", "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock": `{"packages": [{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}]}`,
		"app/Service.php": "<?php\nnamespace App;\n\nuse Acme\\Logging\\Logger;\n\n" +
			"class Service\n{\n    public function run(): void\n    {\n        $logger = new Logger();\n        $logger->info('x');\n    }\n}\n",
	}, "app/Service.php")
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php-log", "repository:r_php_log", "generation-php-log", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_log", "composer.json", manifest, "generation-php-log", now)
	seedParsedFileFact(t, ctx, database, "fact-php-logger", "scope:php-log", "generation-php-log", "src/Logger.php", producer, now)
	assertLoaderLoads(t, ctx, NewFactStore(SQLDB{DB: database}), callPackageKeys(consumer), map[string][]string{
		`package:acme/logging#Acme\Logging\Logger->info`: {"fact-php-logger"},
	})
}

func TestReducerContentionGateActiveCodeCallSymbolLoaderResolvesPHPFunctionKeys(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	const manifest = `{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}, "files": ["src/functions.php"]}}`
	producer := parseLoaderFixture(t, map[string]string{
		"composer.json":     manifest,
		"src/functions.php": "<?php\nnamespace Acme\\Logging;\n\nfunction format_message(string $message): string { return $message; }\n",
	}, "src/functions.php")
	consumer := parseLoaderFixture(t, map[string]string{
		"composer.json": `{"name": "acme/app", "autoload": {"psr-4": {"App\\": "app/"}}}`,
		"composer.lock": `{"packages": [{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}]}`,
		"app/run.php":   "<?php\nnamespace App;\n\nuse function Acme\\Logging\\format_message;\n\nfunction run(): void\n{\n    format_message('y');\n}\n",
	}, "app/run.php")
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php-log", "repository:r_php_log", "generation-php-log", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_log", "composer.json", manifest, "generation-php-log", now)
	seedParsedFileFact(t, ctx, database, "fact-php-functions", "scope:php-log", "generation-php-log", "src/functions.php", producer, now)
	assertLoaderLoads(t, ctx, NewFactStore(SQLDB{DB: database}), callPackageKeys(consumer), map[string][]string{
		`package:acme/logging#function:Acme\Logging\format_message`: {"fact-php-functions"},
	})
}

// parseLoaderFixture writes files under a fresh repository root and returns
// the parser.DefaultEngine payload of relativePath.
func parseLoaderFixture(t *testing.T, files map[string]string, relativePath string) map[string]any {
	t.Helper()
	root := t.TempDir()
	for path, body := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(root, filepath.Join(root, relativePath), false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", relativePath, err)
	}
	return payload
}

// callPackageKeys returns the package_export_symbol of every call in payload.
func callPackageKeys(payload map[string]any) []string {
	calls, _ := payload["function_calls"].([]map[string]any)
	var keys []string
	for _, call := range calls {
		if key, _ := call["package_export_symbol"].(string); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// seedParsedFileFact stores one active file fact whose parsed_file_data is a
// real parser payload.
func seedParsedFileFact(t *testing.T, ctx context.Context, database db.Executor, factID, scopeID, generationID, relativePath string, parsed map[string]any, observedAt time.Time) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"repo_id":          scopeID,
		"relative_path":    relativePath,
		"parsed_file_data": parsed,
	})
	if err != nil {
		t.Fatalf("marshal payload %q: %v", factID, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES ($1, $2, $3, 'file', 'file:' || $2 || ':' || $4, 'git', $4, $5, $5, $6::jsonb)`,
		factID, scopeID, generationID, relativePath, observedAt, string(payload)); err != nil {
		t.Fatalf("insert fact %q: %v", factID, err)
	}
}
