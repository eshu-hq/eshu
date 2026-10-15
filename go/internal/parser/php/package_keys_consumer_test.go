// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// The #7601 PHP consumer half: `new X(` and a static `X::method(` whose class
// is named by a `use` import or a fully qualified literal get
// package_export_symbol = package:<composer name>#<FQCN>[::<method>], where the
// composer name is the locked package (composer.lock packages or packages-dev)
// whose psr-4 prefix is the longest match for the class. The lock names the
// package that actually provides the class, so the consumer's own classes and
// undeclared namespaces are never keyed.

const phpConsumerComposerLock = `{
  "packages": [
    {"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}},
    {"name": "acme/logging-handlers", "autoload": {"psr-4": {"Acme\\Logging\\Handler\\": "src/"}}},
    {"name": "acme/shared-one", "autoload": {"psr-4": {"Shared\\": "src/"}}},
    {"name": "acme/shared-two", "autoload": {"psr-4": {"Shared\\": "lib/"}}},
    {"name": "Acme/BadName", "autoload": {"psr-4": {"Bad\\": "src/"}}},
    {"name": "acme/fallback", "autoload": {"psr-4": {"": "src/"}}}
  ],
  "packages-dev": [
    {"name": "acme/testkit", "autoload": {"psr-4": {"Acme\\Testkit\\": "src/"}}}
  ]
}`

const phpConsumerService = `<?php
namespace App;

use Acme\Logging\Logger;
use Acme\Logging\Formatter as Fmt;
use Acme\Logging\{Level, Handler\StreamHandler as Stream};
use Acme\Testkit\Fake;
use Psr\Log\InvalidArgumentException;
use Resend\Email as ResendEmail;
use Acme\Logging\Email;
use Shared\Thing;
use Bad\Widget;
use App\Models\User;
use function Acme\Logging\helper;

class Service
{
    public function run(Logger $typed): void
    {
        $created = new Logger();
        $factory = Logger::create();
        $aliased = Fmt::make();
        $grouped = new Stream();
        $level = Level::fromName('x');
        $literal = new \Acme\Logging\Logger();
        $literalStatic = \Acme\Logging\Logger::create();
        $devOnly = new Fake();
        $global = new \InvalidArgumentException('x');
        $relative = new Sub\Logger();
        $renamed = ResendEmail::from();
        $shared = new Thing();
        $badName = new Widget();
        $own = User::find(1);
        $dynamic = Logger::$factoryName();
        $variable = new $className();
        $member = $typed->info('x');
        helper();
        $isLogger = $typed instanceof Logger;
    }
}

class ChildLogger extends Logger
{
    public static function build(): void
    {
        $viaParent = parent::create();
        $viaSelf = self::create();
    }
}
`

// phpCallOnLine returns the function_calls item with fullName whose line holds
// marker in source, failing when the call is missing.
func phpCallOnLine(t *testing.T, payload map[string]any, source string, marker string, fullName string) map[string]any {
	t.Helper()

	line := 0
	for index, text := range strings.Split(source, "\n") {
		if strings.Contains(text, marker) {
			line = index + 1
			break
		}
	}
	if line == 0 {
		t.Fatalf("marker %q not in source", marker)
	}
	calls, _ := payload["function_calls"].([]map[string]any)
	for _, call := range calls {
		name, _ := call["full_name"].(string)
		if name == fullName && call["line_number"] == line {
			return call
		}
	}
	t.Fatalf("no call full_name=%q on line %d (%q) in %#v", fullName, line, marker, calls)
	return nil
}

const phpConsumerComposerJSON = `{"name": "acme/app", "require": {"acme/logging": "^1.0"}, "autoload": {"psr-4": {"App\\": "app/"}}}`

func parsePHPConsumer(t *testing.T, withLock bool) map[string]any {
	t.Helper()

	files := map[string]string{
		"composer.json":   phpConsumerComposerJSON,
		"app/Service.php": phpConsumerService,
	}
	if withLock {
		files["composer.lock"] = phpConsumerComposerLock
	}
	return parsePHPProducerRepo(t, files, "app/Service.php")
}

func TestDefaultEngineParsePathPHPStampsConsumerPackageExportSymbols(t *testing.T) {
	t.Parallel()

	got := parsePHPConsumer(t, true)
	for _, tc := range []struct {
		marker   string
		fullName string
		want     string
	}{
		{"$created = new Logger();", "Logger", `package:acme/logging#Acme\Logging\Logger`},
		{"$factory = Logger::create();", "Logger.create", `package:acme/logging#Acme\Logging\Logger::create`},
		{"$aliased = Fmt::make();", "Formatter.make", `package:acme/logging#Acme\Logging\Formatter::make`},
		// Group use; the longer locked prefix Acme\Logging\Handler\ wins.
		{"$grouped = new Stream();", "StreamHandler", `package:acme/logging-handlers#Acme\Logging\Handler\StreamHandler`},
		{"$level = Level::fromName('x');", "Level.fromName", `package:acme/logging#Acme\Logging\Level::fromName`},
		{`$literal = new \Acme\Logging\Logger();`, "Logger", `package:acme/logging#Acme\Logging\Logger`},
		{`$literalStatic = \Acme\Logging\Logger::create();`, `Acme\Logging\Logger.create`, `package:acme/logging#Acme\Logging\Logger::create`},
		// packages-dev count like require-dev.
		{"$devOnly = new Fake();", "Fake", `package:acme/testkit#Acme\Testkit\Fake`},
	} {
		call := phpCallOnLine(t, got, phpConsumerService, tc.marker, tc.fullName)
		parsertest.AssertStringFieldValue(t, call, "package_export_symbol", tc.want)
	}
}

// TestDefaultEngineParsePathPHPSkipsUnprovableConsumerKeys pins the false-link
// patterns measured on the public corpus and the misses that keep the rule
// unique-or-unresolved.
func TestDefaultEngineParsePathPHPSkipsUnprovableConsumerKeys(t *testing.T) {
	t.Parallel()

	got := parsePHPConsumer(t, true)
	for _, tc := range []struct {
		why      string
		marker   string
		fullName string
	}{
		// The parser reduces `new \InvalidArgumentException` to its last
		// segment, which equals an imported Psr\Log class: binding by that
		// name would link the global class to psr/log.
		{"fully qualified global class with an imported last segment", "$global = new", "InvalidArgumentException"},
		// App\Sub\Logger, not the imported Acme\Logging\Logger.
		{"relative qualified name with an imported last segment", "$relative = new", "Logger"},
		// The receiver normalizes to "Email", the short name of a different
		// import (Acme\Logging\Email); the alias binds to Resend\Email.
		{"alias whose target shares a short name with another import", "$renamed = ResendEmail::from();", "Email.from"},
		{"prefix provided by two locked packages", "$shared = new Thing();", "Thing"},
		{"locked name outside the composer grammar", "$badName = new Widget();", "Widget"},
		// Only the empty fallback prefix covers App\Models\User.
		{"consumer's own class", "$own = User::find(1);", "User.find"},
		{"variable method name", "$dynamic = Logger::$factoryName();", "Logger.$factoryName"},
		{"instance method call", "$member = $typed->info('x');", "$typed.info"},
		{"use function", "helper();", "helper"},
		{"parent:: receiver", "$viaParent = parent::create();", "Logger.create"},
		{"self:: receiver", "$viaSelf = self::create();", "ChildLogger.create"},
	} {
		call := phpCallOnLine(t, got, phpConsumerService, tc.marker, tc.fullName)
		if value, ok := call["package_export_symbol"]; ok {
			t.Errorf("%s: package_export_symbol = %#v, want absent", tc.why, value)
		}
	}
	calls, _ := got["function_calls"].([]map[string]any)
	for _, call := range calls {
		if strings.Contains(strings.ToLower(call["full_name"].(string)), "classname") {
			if value, ok := call["package_export_symbol"]; ok {
				t.Errorf("new $className(): package_export_symbol = %#v, want absent", value)
			}
		}
	}
}

func TestDefaultEngineParsePathPHPSkipsConsumerKeysWithoutComposerLock(t *testing.T) {
	t.Parallel()

	assertPHPNoConsumerKeys(t, parsePHPConsumer(t, false))
}

func TestDefaultEngineParsePathPHPSkipsConsumerKeysInMultiNamespaceFile(t *testing.T) {
	t.Parallel()

	got := parsePHPProducerRepo(t, map[string]string{
		"composer.json": phpConsumerComposerJSON,
		"composer.lock": phpConsumerComposerLock,
		"app/Two.php":   "<?php\nnamespace App;\nuse Acme\\Logging\\Logger;\nfunction a() { return new Logger(); }\n\nnamespace App\\Other;\nfunction b() { return new Logger(); }\n",
	}, "app/Two.php")
	assertPHPNoConsumerKeys(t, got)
}

// TestDefaultEngineParsePathPHPKeysConsumerFromNearestCoveringLock pins the
// lock walk: the nearest composer.lock that covers the class decides.
func TestDefaultEngineParsePathPHPKeysConsumerFromNearestCoveringLock(t *testing.T) {
	t.Parallel()

	source := "<?php\nnamespace App;\n\nuse Acme\\Logging\\Logger;\nuse Acme\\Testkit\\Fake;\n\nfunction run() {\n    $a = new Logger();\n    $b = new Fake();\n}\n"
	got := parsePHPProducerRepo(t, map[string]string{
		"composer.json": phpConsumerComposerJSON,
		"composer.lock": phpConsumerComposerLock,
		"apps/web/composer.lock": `{"packages": [
			{"name": "acme/logging-fork", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}
		]}`,
		"apps/web/src/run.php": source,
	}, "apps/web/src/run.php")
	parsertest.AssertStringFieldValue(t, phpCallOnLine(t, got, source, "$a = new Logger();", "Logger"),
		"package_export_symbol", `package:acme/logging-fork#Acme\Logging\Logger`)
	// The nested lock does not cover Acme\Testkit\, so the root lock decides.
	parsertest.AssertStringFieldValue(t, phpCallOnLine(t, got, source, "$b = new Fake();", "Fake"),
		"package_export_symbol", `package:acme/testkit#Acme\Testkit\Fake`)
}

func assertPHPNoConsumerKeys(t *testing.T, payload map[string]any) {
	t.Helper()
	calls, _ := payload["function_calls"].([]map[string]any)
	if len(calls) == 0 {
		t.Fatalf("function_calls empty; the fixture must emit calls")
	}
	for _, call := range calls {
		if value, ok := call["package_export_symbol"]; ok {
			t.Errorf("call %v: package_export_symbol = %#v, want absent", call["full_name"], value)
		}
	}
}
