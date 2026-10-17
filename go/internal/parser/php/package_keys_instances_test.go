// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// These tests pin the #8280 PHP instance keys. A public instance method of a
// keyed class carries export_name <FQCN>-><method>. A consumer's `$v->m(` keys
// only when the same variable scope writes $v once, by `new` of a keyable
// class, and reads it only as a member receiver; `(new X())->m(` keys
// directly.

const phpMemberProducerComposerJSON = `{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}`

const phpMemberProducerLogger = `<?php
namespace Acme\Logging;

class Logger
{
    public static function create(): self { return new self(); }
    public function info(string $message): void {}
    function implicitInstance(): void {}
    final public function sealed(): void {}
    protected function guarded(): void {}
    private function hidden(): void {}
    public function __construct() {}
    public function __call($name, $args) {}
}
`

func parsePHPMemberProducer(t *testing.T, relativePath string) map[string]any {
	t.Helper()

	return parsePHPProducerRepo(t, map[string]string{
		"composer.json":  phpMemberProducerComposerJSON,
		"src/Logger.php": phpMemberProducerLogger,
		"src/Base.php": `<?php
namespace Acme\Logging;

abstract class Base
{
    abstract public function run(): void;
    public function shared(): void {}
}
`,
		"src/Contract.php": `<?php
namespace Acme\Logging;

interface Contract
{
    public function info(): void;
}
`,
		"src/Helper.php": `<?php
namespace Acme\Logging;

trait Helper
{
    public function assist(): void {}
}
`,
	}, relativePath)
}

func TestDefaultEngineParsePathPHPStampsProducerInstanceMethodKeys(t *testing.T) {
	t.Parallel()

	got := parsePHPMemberProducer(t, "src/Logger.php")
	for _, method := range []string{"info", "implicitInstance", "sealed"} {
		item := parsertest.AssertFunctionByNameAndClass(t, got, method, "Logger")
		parsertest.AssertStringFieldValue(t, item, "package_id", "acme/logging")
		parsertest.AssertStringFieldValue(t, item, "export_name", `Acme\Logging\Logger->`+method)
	}
	// The static key keeps its #7601 :: form.
	create := parsertest.AssertFunctionByNameAndClass(t, got, "create", "Logger")
	parsertest.AssertStringFieldValue(t, create, "export_name", `Acme\Logging\Logger::create`)
	// Protected, private, and magic methods are not callable as $v->m() from
	// another package by name.
	for _, method := range []string{"guarded", "hidden", "__construct", "__call"} {
		assertPHPNoPackageKey(t, "method "+method, parsertest.AssertFunctionByNameAndClass(t, got, method, "Logger"))
	}

	base := parsePHPMemberProducer(t, "src/Base.php")
	shared := parsertest.AssertFunctionByNameAndClass(t, base, "shared", "Base")
	parsertest.AssertStringFieldValue(t, shared, "export_name", `Acme\Logging\Base->shared`)
	assertPHPNoPackageKey(t, "abstract method run", parsertest.AssertFunctionByNameAndClass(t, base, "run", "Base"))

	// Interface and trait methods are never stamped: a receiver is only ever
	// proven to be an instance of a concrete class.
	contract := parsePHPMemberProducer(t, "src/Contract.php")
	assertPHPNoPackageKey(t, "interface method", parsertest.AssertFunctionByNameAndClass(t, contract, "info", "Contract"))
	helper := parsePHPMemberProducer(t, "src/Helper.php")
	assertPHPNoPackageKey(t, "trait method", parsertest.AssertFunctionByNameAndClass(t, helper, "assist", "Helper"))
}

const phpMemberConsumerLock = `{
  "packages": [
    {"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}
  ]
}`

const phpMemberConsumer = `<?php
namespace App;

use Acme\Logging\Logger;
use Acme\Logging\Logger as AliasLogger;

class Service
{
    public function run(Logger $typed): void
    {
        $logger = new Logger();
        $logger->level = 1;
        $logger->info('direct');
        (new Logger())->info('parenthesized');
        (new AliasLogger())->info('aliased');
        $literal = new \Acme\Logging\Logger();
        $literal->info('literal');
        $reassigned = new Logger();
        $reassigned = make();
        $reassigned->info('reassigned');
        $passed = new Logger();
        consume($passed);
        $passed->info('passed');
        $typed->info('typed');
        $this->logger->info('property');
        $nullsafe = new Logger();
        $nullsafe?->info('nullsafe');
        $own = new Local();
        $own->info('own');
        $looped = new Logger();
        foreach ([] as $looped) {}
        $looped->info('looped');
        if ($flag) {
            $branched = new Local();
        } else {
            $branched = new Logger();
        }
        $branched->info('branched');
        $arrowed = new Logger();
        $call = fn () => $arrowed->info('arrow');
    }

    public function otherScope(): void
    {
        $logger->info('other scope');
        $captured = new Logger();
        $fn = function () use ($captured) { $captured->info('closure'); };
        $byRef = new Logger();
        $set = function () use (&$byRef) { $byRef = make(); };
        $byRef->info('by reference');
    }

    public function dynamicScope(array $vars): void
    {
        $extracted = new Logger();
        extract($vars);
        $extracted->info('extract');
    }
}
`

// phpCallByNameOnLine returns the function_calls item named name on the line
// holding marker.
func phpCallByNameOnLine(t *testing.T, payload map[string]any, source string, marker string, name string) map[string]any {
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
		if call["name"] == name && call["line_number"] == line {
			return call
		}
	}
	t.Fatalf("no call name=%q on line %d (%q) in %#v", name, line, marker, calls)
	return nil
}

func parsePHPMemberConsumer(t *testing.T) map[string]any {
	t.Helper()

	return parsePHPProducerRepo(t, map[string]string{
		"composer.json":   phpConsumerComposerJSON,
		"composer.lock":   phpMemberConsumerLock,
		"app/Service.php": phpMemberConsumer,
	}, "app/Service.php")
}

func TestDefaultEngineParsePathPHPStampsConsumerInstanceKeys(t *testing.T) {
	t.Parallel()

	got := parsePHPMemberConsumer(t)
	const logger = `package:acme/logging#Acme\Logging\Logger->info`
	for _, tc := range []struct {
		marker string
		name   string
		want   string
	}{
		{"'direct'", "info", logger},
		{"'parenthesized'", "info", logger},
		{"'aliased'", "info", logger},
		{"'literal'", "info", logger},
		// An arrow function shares its parent's variable scope.
		{"'arrow'", "info", logger},
	} {
		call := phpCallByNameOnLine(t, got, phpMemberConsumer, tc.marker, tc.name)
		parsertest.AssertStringFieldValue(t, call, "package_export_symbol", tc.want)
	}
}

func TestDefaultEngineParsePathPHPSkipsUnprovableInstanceKeys(t *testing.T) {
	t.Parallel()

	got := parsePHPMemberConsumer(t)
	for _, tc := range []struct {
		why    string
		marker string
		name   string
	}{
		{"variable written twice", "'reassigned'", "info"},
		{"two new writes, one of another class", "'branched'", "info"},
		{"variable passed as an argument (a by-reference write)", "'passed'", "info"},
		{"typed parameter", "'typed'", "info"},
		{"typed property chain", "'property'", "info"},
		{"nullsafe call", "'nullsafe'", "info"},
		{"consumer's own class", "'own'", "info"},
		{"foreach rebinds the variable", "'looped'", "info"},
		{"variable written in another method", "'other scope'", "info"},
		{"closure scope: the use clause copies, the closure never writes new", "'closure'", "info"},
		{"extract() can write any variable", "'extract'", "info"},
		{"a closure captures the variable by reference", "'by reference'", "info"},
	} {
		call := phpCallByNameOnLine(t, got, phpMemberConsumer, tc.marker, tc.name)
		if value, ok := call["package_export_symbol"]; ok {
			t.Errorf("%s: package_export_symbol = %#v, want absent", tc.why, value)
		}
	}
}

func TestDefaultEngineParsePathPHPKeysTopLevelScriptInstanceCalls(t *testing.T) {
	t.Parallel()

	const script = `<?php
use Acme\Logging\Logger;

$early->info('before the write');
$top = new Logger();
$top->info('top');
if ($ready) {
    $branch = new Logger();
    $branch->info('same branch');
}
$branch->info('after the branch');
$early = new Logger();

function local_fn(): void
{
    $top->info('inside');
}
`
	got := parsePHPProducerRepo(t, map[string]string{
		"composer.json": phpConsumerComposerJSON,
		"composer.lock": phpMemberConsumerLock,
		"bin/run.php":   script,
	}, "bin/run.php")
	top := phpCallByNameOnLine(t, got, script, "'top'", "info")
	parsertest.AssertStringFieldValue(t, top, "package_export_symbol", `package:acme/logging#Acme\Logging\Logger->info`)
	branch := phpCallByNameOnLine(t, got, script, "'same branch'", "info")
	parsertest.AssertStringFieldValue(t, branch, "package_export_symbol", `package:acme/logging#Acme\Logging\Logger->info`)
	// A script's variables can come from the file that includes it, so a
	// script-scope receiver is proven only where its one `new` write runs
	// first on every path: not before the write, and not after a branch that
	// may skip it.
	for _, marker := range []string{"'inside'", "'before the write'", "'after the branch'"} {
		call := phpCallByNameOnLine(t, got, script, marker, "info")
		if value, ok := call["package_export_symbol"]; ok {
			t.Errorf("%s: package_export_symbol = %#v, want absent", marker, value)
		}
	}
}

// TestDefaultEngineParsePathPHPSkipsConditionalAndAnonymousClassKeys pins the
// class-shape rules: a class declared inside a condition (the
// `if (!class_exists(...))` polyfill pattern) may never be the class composer
// loads, so neither it nor its methods get a key; an anonymous class has no
// name to key; a final class keys like any other.
func TestDefaultEngineParsePathPHPSkipsConditionalAndAnonymousClassKeys(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"composer.json": phpMemberProducerComposerJSON,
		"src/Conditional.php": `<?php
namespace Acme\Logging;

if (!class_exists('Acme\Logging\Conditional')) {
    class Conditional
    {
        public function info(): void {}
        public static function make(): void {}
    }
}
`,
		"src/Braced.php": "<?php\nnamespace Acme\\Logging {\n    class Braced\n    {\n        public function info(): void {}\n    }\n}\n",
		"src/Sealed.php": `<?php
namespace Acme\Logging;

final class Sealed
{
    public function info(): void
    {
        $helper = new class {
            public function assist(): void {}
        };
    }
}
`,
	}
	conditional := parsePHPProducerRepo(t, files, "src/Conditional.php")
	assertPHPNoPackageKey(t, "conditional class", parsertest.AssertBucketItemByName(t, conditional, "classes", "Conditional"))
	assertPHPNoPackageKey(t, "conditional class instance method", parsertest.AssertFunctionByNameAndClass(t, conditional, "info", "Conditional"))
	assertPHPNoPackageKey(t, "conditional class static method", parsertest.AssertFunctionByNameAndClass(t, conditional, "make", "Conditional"))

	braced := parsePHPProducerRepo(t, files, "src/Braced.php")
	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, braced, "classes", "Braced"), "export_name", `Acme\Logging\Braced`)

	sealed := parsePHPProducerRepo(t, files, "src/Sealed.php")
	info := parsertest.AssertFunctionByNameAndClass(t, sealed, "info", "Sealed")
	parsertest.AssertStringFieldValue(t, info, "export_name", `Acme\Logging\Sealed->info`)
	for _, item := range sealed["functions"].([]map[string]any) {
		if item["name"] == "assist" {
			assertPHPNoPackageKey(t, "anonymous class method", item)
		}
	}
}

// TestDefaultEngineParsePathPHPSkipsReceiversNonLocalCodeCanRewrite pins the
// #8280 review F2 counterexamples. `$GLOBALS` and `eval` can write a variable
// without naming it, and in a script scope any code that runs between the
// write and the call (a function call, a constructor, an echo that may call
// __toString, a read of another variable) can reach the script's variables
// through `global` or `$GLOBALS`, even from another file. Each such call
// stays unkeyed; a script call with only literals and calls on the receiver
// itself in between still keys.
func TestDefaultEngineParsePathPHPSkipsReceiversNonLocalCodeCanRewrite(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		source  string
		marker  string
		wantKey bool
	}{
		{"GLOBALS write in the script scope", "<?php\nuse Acme\\Logging\\Logger;\n\n$log = new Logger();\n$GLOBALS['log'] = new \\Other\\Thing();\n$log->info('globals');\n", "'globals'", false},
		{"same-file function with global", "<?php\nuse Acme\\Logging\\Logger;\n\nfunction swap(): void\n{\n    global $log;\n    $log = new \\Other\\Thing();\n}\n\n$log = new Logger();\nswap();\n$log->info('swapped');\n", "'swapped'", false},
		{"eval in a function scope", "<?php\nuse Acme\\Logging\\Logger;\n\nfunction run(): void\n{\n    $v = new Logger();\n    eval('$v = new \\Other\\Thing();');\n    $v->info('eval');\n}\n", "'eval'", false},
		{"intervening constructor", "<?php\nuse Acme\\Logging\\Logger;\n\n$log = new Logger();\n$other = new \\Other\\Thing();\n$log->info('constructor');\n", "'constructor'", false},
		{"intervening echo of another variable", "<?php\nuse Acme\\Logging\\Logger;\n\n$log = new Logger();\necho $title;\n$log->info('echo');\n", "'echo'", false},
		{"intervening concat of another variable (may call __toString)", "<?php\nuse Acme\\Logging\\Logger;\n\n$log = new Logger();\n$label = 'x' . $other;\n$log->info('concat');\n", "'concat'", false},
		// Review F7: control flow and ticks that the dominance rule does not
		// see. A goto can skip the write, so $log is the includer's value; a
		// tick function runs after the write statement.
		{"goto over the write", "<?php\nuse Acme\\Logging\\Logger;\n\ngoto skip;\n$log = new Logger();\nskip:\n$log->info('goto');\n", "'goto'", false},
		{"declare ticks with a tick function", "<?php\ndeclare(ticks=1);\nuse Acme\\Logging\\Logger;\n\nregister_tick_function('external_fn');\n$log = new Logger();\n$log->info('ticks');\n", "'ticks'", false},
		// Review F9: implicit code between the write and the call. Inline HTML
		// is output (an output buffer callback may run), and a class constant
		// may trigger an autoloader.
		{"inline HTML in between", "<?php\nuse Acme\\Logging\\Logger;\n\n$log = new Logger();\n?>\n<p>hello</p>\n<?php\n$log->info('html');\n", "'html'", false},
		{"class constant in between", "<?php\nuse Acme\\Logging\\Logger;\n\n$log = new Logger();\n$log->setLevel(\\Other\\Thing::LEVEL);\n$log->info('constant');\n", "'constant'", false},
		{"only receiver calls and literals in between", "<?php\nuse Acme\\Logging\\Logger;\n\n$log = new Logger();\n$log->level = 3;\n$log->setName('app');\n$log->info('clean');\n", "'clean'", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := parsePHPProducerRepo(t, map[string]string{
				"composer.json": phpConsumerComposerJSON,
				"composer.lock": phpMemberConsumerLock,
				"bin/run.php":   tc.source,
			}, "bin/run.php")
			call := phpCallByNameOnLine(t, got, tc.source, tc.marker, "info")
			value, ok := call["package_export_symbol"]
			if ok != tc.wantKey {
				t.Errorf("package_export_symbol = %#v (present %v), want present %v", value, ok, tc.wantKey)
			}
		})
	}
}
