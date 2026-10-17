// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// These tests pin the #8280 PHP function keys. A namespaced free function in a
// file the manifest's autoload.files lists, under a covering psr-4 prefix,
// carries export_name function:<FQN>; a bare `f(` keys only when a `use
// function` clause binds f.

const phpFunctionProducerComposerJSON = `{
  "name": "acme/logging",
  "autoload": {
    "psr-4": {"Acme\\Logging\\": "src/"},
    "files": ["src/functions.php", "./src/global.php", "src/other.php"]
  }
}`

const phpFunctionProducerSource = `<?php
namespace Acme\Logging;

function format_message(string $message): string { return $message; }

if (!function_exists('Acme\Logging\guarded_helper')) {
    function guarded_helper(): void {}
}

function outer(): void
{
    function inner(): void {}
}
`

func parsePHPFunctionProducer(t *testing.T, relativePath string) map[string]any {
	t.Helper()

	return parsePHPProducerRepo(t, map[string]string{
		"composer.json":     phpFunctionProducerComposerJSON,
		"src/functions.php": phpFunctionProducerSource,
		"src/unlisted.php":  "<?php\nnamespace Acme\\Logging;\n\nfunction unlisted(): void {}\n",
		"src/global.php":    "<?php\n\nfunction global_helper(): void {}\n",
		"src/other.php":     "<?php\nnamespace Other\\Space;\n\nfunction elsewhere(): void {}\n",
	}, relativePath)
}

func TestDefaultEngineParsePathPHPStampsProducerFunctionKeys(t *testing.T) {
	t.Parallel()

	got := parsePHPFunctionProducer(t, "src/functions.php")
	for _, name := range []string{"format_message", "guarded_helper", "outer"} {
		item := parsertest.AssertBucketItemByName(t, got, "functions", name)
		parsertest.AssertStringFieldValue(t, item, "package_id", "acme/logging")
		parsertest.AssertStringFieldValue(t, item, "export_name", `function:Acme\Logging\`+name)
	}
	// A function declared inside another function exists only after the outer
	// one runs.
	assertPHPNoPackageKey(t, "nested function", parsertest.AssertBucketItemByName(t, got, "functions", "inner"))

	for _, tc := range []struct {
		why  string
		path string
		name string
	}{
		{"file not listed in autoload.files", "src/unlisted.php", "unlisted"},
		{"global function: no psr-4 prefix covers it", "src/global.php", "global_helper"},
		{"listed file whose namespace no psr-4 prefix covers", "src/other.php", "elsewhere"},
	} {
		got := parsePHPFunctionProducer(t, tc.path)
		assertPHPNoPackageKey(t, tc.why, parsertest.AssertBucketItemByName(t, got, "functions", tc.name))
	}
}

func TestDefaultEngineParsePathPHPSkipsProducerFunctionKeysUnderFixtureManifest(t *testing.T) {
	t.Parallel()

	got := parsePHPProducerRepo(t, map[string]string{
		"composer.json":                        phpFunctionProducerComposerJSON,
		"tests/fixtures/app/composer.json":     phpFunctionProducerComposerJSON,
		"tests/fixtures/app/src/functions.php": phpFunctionProducerSource,
	}, "tests/fixtures/app/src/functions.php")
	assertPHPNoPackageKey(t, "fixture function", parsertest.AssertBucketItemByName(t, got, "functions", "format_message"))
}

const phpFunctionConsumerLock = `{
  "packages": [
    {"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}, "files": ["src/functions.php"]}},
    {"name": "acme/shared-one", "autoload": {"psr-4": {"Shared\\": "src/"}}}
  ]
}`

const phpFunctionConsumer = `<?php
namespace App;

use function Acme\Logging\format_message;
use function Acme\Logging\format_message as fmt;
use function Acme\Logging\{outer, guarded_helper};
use Acme\Logging\{Level, function grouped_fn, const LEVEL};
use function Other\Space\undeclared_fn;
use function Acme\Logging\twice;
use function Shared\twice;

function run(): void
{
    format_message('x');
    fmt('y');
    outer();
    guarded_helper();
    grouped_fn();
    undeclared_fn();
    twice();
    unbound();
}
`

func parsePHPFunctionConsumer(t *testing.T) map[string]any {
	t.Helper()

	return parsePHPProducerRepo(t, map[string]string{
		"composer.json": phpConsumerComposerJSON,
		"composer.lock": phpFunctionConsumerLock,
		"app/run.php":   phpFunctionConsumer,
	}, "app/run.php")
}

func TestDefaultEngineParsePathPHPStampsConsumerFunctionKeys(t *testing.T) {
	t.Parallel()

	got := parsePHPFunctionConsumer(t)
	for _, tc := range []struct {
		marker string
		name   string
		want   string
	}{
		{"format_message('x')", "format_message", `package:acme/logging#function:Acme\Logging\format_message`},
		{"fmt('y')", "fmt", `package:acme/logging#function:Acme\Logging\format_message`},
		{"outer();", "outer", `package:acme/logging#function:Acme\Logging\outer`},
		{"guarded_helper();", "guarded_helper", `package:acme/logging#function:Acme\Logging\guarded_helper`},
		// The function clause of a mixed group use binds a function.
		{"grouped_fn();", "grouped_fn", `package:acme/logging#function:Acme\Logging\grouped_fn`},
	} {
		call := phpCallOnLine(t, got, phpFunctionConsumer, tc.marker, tc.name)
		parsertest.AssertStringFieldValue(t, call, "package_export_symbol", tc.want)
	}
}

func TestDefaultEngineParsePathPHPSkipsUnprovableFunctionKeys(t *testing.T) {
	t.Parallel()

	got := parsePHPFunctionConsumer(t)
	for _, tc := range []struct {
		why    string
		marker string
		name   string
	}{
		{"no lock covers the namespace", "undeclared_fn();", "undeclared_fn"},
		{"local name bound to two functions", "twice();", "twice"},
		{"bare call with no use function", "unbound();", "unbound"},
	} {
		call := phpCallOnLine(t, got, phpFunctionConsumer, tc.marker, tc.name)
		if value, ok := call["package_export_symbol"]; ok {
			t.Errorf("%s: package_export_symbol = %#v, want absent", tc.why, value)
		}
	}
}
