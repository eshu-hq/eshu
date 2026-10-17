// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// The #7601 PHP producer half: a class or interface that composer's
// autoload psr-4 map loads carries package_id (the nearest composer.json name)
// and export_name (the fully qualified name); a public static method of such a
// class carries export_name <FQCN>::<method>. The reducer derives
// package:<package_id>#<export_name> from them, so a key here must only ever
// name a symbol a consumer can load.

const phpProducerComposerJSON = `{
  "name": "acme/logging",
  "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}},
  "autoload-dev": {"psr-4": {"Acme\\Logging\\Tests\\": "tests/"}}
}`

const phpProducerLogger = `<?php
namespace Acme\Logging;

class Logger
{
    public static function create(): self
    {
        return new self();
    }

    final public static function sealed(): void {}

    static function implicitPublic(): void {}

    public function info(string $message): void {}

    protected static function guarded(): void {}

    private static function hidden(): void {}
}
`

// parsePHPProducerRepo writes files under a fresh repository root and parses
// relativePath through the default engine.
func parsePHPProducerRepo(t *testing.T, files map[string]string, relativePath string) map[string]any {
	t.Helper()

	repoRoot := t.TempDir()
	for path, body := range files {
		full := filepath.Join(repoRoot, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		parsertest.WriteFile(t, full, body)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filepath.Join(repoRoot, relativePath), false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", relativePath, err)
	}
	return got
}

// assertPHPNoPackageKey fails when item carries either producer key field.
func assertPHPNoPackageKey(t *testing.T, label string, item map[string]any) {
	t.Helper()
	for _, field := range []string{"package_id", "export_name"} {
		if value, ok := item[field]; ok {
			t.Errorf("%s %s = %#v, want absent", label, field, value)
		}
	}
}

func TestDefaultEngineParsePathPHPStampsProducerPackageKeysOnPSR4Classes(t *testing.T) {
	t.Parallel()

	got := parsePHPProducerRepo(t, map[string]string{
		"composer.json":  phpProducerComposerJSON,
		"src/Logger.php": phpProducerLogger,
	}, "src/Logger.php")

	class := parsertest.AssertBucketItemByName(t, got, "classes", "Logger")
	parsertest.AssertStringFieldValue(t, class, "package_id", "acme/logging")
	parsertest.AssertStringFieldValue(t, class, "export_name", `Acme\Logging\Logger`)

	for _, method := range []string{"create", "sealed", "implicitPublic"} {
		item := parsertest.AssertFunctionByNameAndClass(t, got, method, "Logger")
		parsertest.AssertStringFieldValue(t, item, "package_id", "acme/logging")
		parsertest.AssertStringFieldValue(t, item, "export_name", `Acme\Logging\Logger::`+method)
	}
	// A public instance method carries the #8280 instance key, never the
	// static :: form.
	info := parsertest.AssertFunctionByNameAndClass(t, got, "info", "Logger")
	parsertest.AssertStringFieldValue(t, info, "export_name", `Acme\Logging\Logger->info`)
	for _, method := range []string{"guarded", "hidden"} {
		assertPHPNoPackageKey(t, "method "+method, parsertest.AssertFunctionByNameAndClass(t, got, method, "Logger"))
	}
}

func TestDefaultEngineParsePathPHPStampsProducerPackageKeysOnNestedNamespaceAndInterface(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"composer.json": phpProducerComposerJSON,
		"src/Handler/StreamHandler.php": `<?php
namespace Acme\Logging\Handler;

class StreamHandler {}
`,
		"src/LoggerInterface.php": `<?php
namespace Acme\Logging;

interface LoggerInterface {}
`,
	}
	got := parsePHPProducerRepo(t, files, "src/Handler/StreamHandler.php")
	class := parsertest.AssertBucketItemByName(t, got, "classes", "StreamHandler")
	parsertest.AssertStringFieldValue(t, class, "package_id", "acme/logging")
	parsertest.AssertStringFieldValue(t, class, "export_name", `Acme\Logging\Handler\StreamHandler`)

	got = parsePHPProducerRepo(t, files, "src/LoggerInterface.php")
	iface := parsertest.AssertBucketItemByName(t, got, "interfaces", "LoggerInterface")
	parsertest.AssertStringFieldValue(t, iface, "package_id", "acme/logging")
	parsertest.AssertStringFieldValue(t, iface, "export_name", `Acme\Logging\LoggerInterface`)
}

// TestDefaultEngineParsePathPHPSkipsProducerKeysComposerCannotAutoload pins the
// negatives: composer would never load these classes for a consumer, so a key
// on them could only mint a false edge.
func TestDefaultEngineParsePathPHPSkipsProducerKeysComposerCannotAutoload(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		files        map[string]string
		relativePath string
		bucket       string
		itemName     string
	}{
		{
			// The path does not match the namespace, so psr-4 cannot load it.
			name: "path does not match the class name",
			files: map[string]string{
				"composer.json":     phpProducerComposerJSON,
				"src/Misplaced.php": "<?php\nnamespace Acme\\Logging;\n\nclass NotMisplaced {}\n",
			},
			relativePath: "src/Misplaced.php",
			bucket:       "classes",
			itemName:     "NotMisplaced",
		},
		{
			// autoload-dev is root-only: no consumer ever loads it.
			name: "autoload-dev class",
			files: map[string]string{
				"composer.json":        phpProducerComposerJSON,
				"tests/LoggerTest.php": "<?php\nnamespace Acme\\Logging\\Tests;\n\nclass LoggerTest {}\n",
			},
			relativePath: "tests/LoggerTest.php",
			bucket:       "classes",
			itemName:     "LoggerTest",
		},
		{
			// psr-4 autoloads classes, never free functions.
			name: "free function under a psr-4 root",
			files: map[string]string{
				"composer.json":  phpProducerComposerJSON,
				"src/helper.php": "<?php\nnamespace Acme\\Logging;\n\nfunction helper(): void {}\n",
			},
			relativePath: "src/helper.php",
			bucket:       "functions",
			itemName:     "helper",
		},
		{
			name: "classmap autoload only",
			files: map[string]string{
				"composer.json":  `{"name": "acme/logging", "autoload": {"classmap": ["src/"]}}`,
				"src/Logger.php": phpProducerLogger,
			},
			relativePath: "src/Logger.php",
			bucket:       "classes",
			itemName:     "Logger",
		},
		{
			// An empty psr-4 prefix is a fallback for every namespace; it proves
			// no ownership of any one class.
			name: "empty psr-4 fallback prefix",
			files: map[string]string{
				"composer.json":               `{"name": "acme/logging", "autoload": {"psr-4": {"": "src/"}}}`,
				"src/Acme/Logging/Logger.php": phpProducerLogger,
			},
			relativePath: "src/Acme/Logging/Logger.php",
			bucket:       "classes",
			itemName:     "Logger",
		},
		{
			name:         "no composer.json",
			files:        map[string]string{"src/Logger.php": phpProducerLogger},
			relativePath: "src/Logger.php",
			bucket:       "classes",
			itemName:     "Logger",
		},
		{
			name: "composer.json without a name",
			files: map[string]string{
				"composer.json":  `{"autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}`,
				"src/Logger.php": phpProducerLogger,
			},
			relativePath: "src/Logger.php",
			bucket:       "classes",
			itemName:     "Logger",
		},
		{
			// Composer package names are lowercase vendor/name.
			name: "composer.json name outside the composer grammar",
			files: map[string]string{
				"composer.json":  `{"name": "Acme/Logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}`,
				"src/Logger.php": phpProducerLogger,
			},
			relativePath: "src/Logger.php",
			bucket:       "classes",
			itemName:     "Logger",
		},
		{
			// A vendored copy is not a publisher.
			name: "vendored copy under vendor/",
			files: map[string]string{
				"composer.json":                      `{"name": "acme/app"}`,
				"vendor/acme/logging/composer.json":  phpProducerComposerJSON,
				"vendor/acme/logging/src/Logger.php": phpProducerLogger,
			},
			relativePath: "vendor/acme/logging/src/Logger.php",
			bucket:       "classes",
			itemName:     "Logger",
		},
		{
			// Each namespace block has its own names; the file is skipped whole.
			name: "file with two namespace blocks",
			files: map[string]string{
				"composer.json":  phpProducerComposerJSON,
				"src/Logger.php": "<?php\nnamespace Acme\\Logging;\n\nclass Logger {}\n\nnamespace Acme\\Other;\n\nclass Thing {}\n",
			},
			relativePath: "src/Logger.php",
			bucket:       "classes",
			itemName:     "Logger",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parsePHPProducerRepo(t, tc.files, tc.relativePath)
			assertPHPNoPackageKey(t, tc.itemName, parsertest.AssertBucketItemByName(t, got, tc.bucket, tc.itemName))
		})
	}
}
