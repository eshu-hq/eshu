// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPEmitsFunctionParametersSourceAndContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "functions.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace Demo;

function greet(string $name, int $count): string {
    $prefix = "Hello";
    return $prefix . $name;
}

class Application {
    public function run(string $message): void {
        greet($message, 1);
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := parsertest.AssertBucketItemByName(t, got, "functions", "greet")
	parsertest.AssertStringSliceEquals(t, functionItem, "parameters", []string{"$name", "$count"})
	phpAssertStringFieldContains(t, functionItem, "source", "return $prefix . $name;")

	methodItem := parsertest.AssertBucketItemByName(t, got, "functions", "run")
	parsertest.AssertStringFieldValue(t, methodItem, "class_context", "Application")
	phpAssertStringFieldContains(t, methodItem, "source", "greet($message, 1);")
}

func TestDefaultEngineParsePathPHPEmitsInheritanceAndImportMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "types.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace Demo;

use Demo\Library\Config as AppConfig;
use Demo\Library\Service;

class Child extends ParentClass implements Runnable, JsonSerializable {
    use Loggable, Auditable;
}

interface Repository extends Identifiable, Countable {
}

trait Loggable {
}

trait Auditable {
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := parsertest.AssertBucketItemByName(t, got, "classes", "Child")
	parsertest.AssertStringSliceEquals(t, classItem, "bases", []string{"ParentClass", "Runnable", "JsonSerializable", "Loggable", "Auditable"})

	interfaceItem := parsertest.AssertBucketItemByName(t, got, "interfaces", "Repository")
	parsertest.AssertStringSliceEquals(t, interfaceItem, "bases", []string{"Identifiable", "Countable"})

	importItem := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\Config")
	parsertest.AssertStringFieldValue(t, importItem, "full_import_name", "use Demo\\Library\\Config as AppConfig;")
	parsertest.AssertStringFieldValue(t, importItem, "alias", "AppConfig")
	phpAssertBoolFieldValue(t, importItem, "is_dependency", false)

	secondImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\Service")
	parsertest.AssertStringFieldValue(t, secondImport, "full_import_name", "use Demo\\Library\\Service;")
	if alias, ok := secondImport["alias"]; ok && alias != nil && alias != "" {
		t.Fatalf("alias = %#v, want nil or empty", alias)
	}
}

func TestDefaultEngineParsePathPHPEmitsTraitAdaptationBases(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "trait_adaptation.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace Demo;

class Child {
    use Loggable, Auditable {
        Auditable::record insteadof Loggable;
        Loggable::record as private logRecord;
    }
}

trait Loggable {
}

trait Auditable {
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := parsertest.AssertBucketItemByName(t, got, "classes", "Child")
	parsertest.AssertStringSliceEquals(t, classItem, "bases", []string{"Loggable", "Auditable"})
}

func TestDefaultEngineParsePathPHPEmitsGroupedUseImportMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "grouped_use.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace Demo;

use Demo\Library\{Config as AppConfig, Service, Logger\Stream as StreamLogger};

class Child {
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	configImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\Config")
	parsertest.AssertStringFieldValue(t, configImport, "alias", "AppConfig")

	serviceImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\Service")
	if alias, ok := serviceImport["alias"]; ok && alias != nil && alias != "" {
		t.Fatalf("alias = %#v, want nil or empty", alias)
	}

	streamImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\Logger\\Stream")
	parsertest.AssertStringFieldValue(t, streamImport, "alias", "StreamLogger")
}

func TestDefaultEngineParsePathPHPEmitsGroupedUseFunctionAndConstImportKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "grouped_use_kinds.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace Demo;

use function Demo\Library\{helper, format as format_value};
use const Demo\Library\{DEFAULT_LIMIT, MAX_VALUE as MAX_LIMIT};
use Demo\Library\Service;
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	helperImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\helper")
	parsertest.AssertStringFieldValue(t, helperImport, "import_type", "function")

	formatImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\format")
	parsertest.AssertStringFieldValue(t, formatImport, "import_type", "function")
	parsertest.AssertStringFieldValue(t, formatImport, "alias", "format_value")

	limitImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\DEFAULT_LIMIT")
	parsertest.AssertStringFieldValue(t, limitImport, "import_type", "const")

	maxLimitImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\MAX_VALUE")
	parsertest.AssertStringFieldValue(t, maxLimitImport, "import_type", "const")
	parsertest.AssertStringFieldValue(t, maxLimitImport, "alias", "MAX_LIMIT")

	serviceImport := parsertest.AssertBucketItemByName(t, got, "imports", "Demo\\Library\\Service")
	parsertest.AssertStringFieldValue(t, serviceImport, "import_type", "use")
}

func TestDefaultEngineParsePathPHPEmitsMagicMethodClassification(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "magic_methods.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class MagicBox {
    public function __get(string $name): mixed {
        return null;
    }

    public function __call(string $name, array $arguments): mixed {
        return null;
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	getMethod := parsertest.AssertBucketItemByName(t, got, "functions", "__get")
	parsertest.AssertStringFieldValue(t, getMethod, "class_context", "MagicBox")
	parsertest.AssertStringFieldValue(t, getMethod, "semantic_kind", "magic_method")

	callMethod := parsertest.AssertBucketItemByName(t, got, "functions", "__call")
	parsertest.AssertStringFieldValue(t, callMethod, "class_context", "MagicBox")
	parsertest.AssertStringFieldValue(t, callMethod, "semantic_kind", "magic_method")
}

func TestDefaultEngineParsePathPHPEmitsVariableAndCallMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "calls.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace Demo;

class Config {
    private string $env;
    private bool $debug;

    public function run(string $message): void {
        $service = new Service("main");
        $greeting = greet($message);
        $service->info($greeting);
        Logger::warn("warn");
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	envItem := parsertest.AssertBucketItemByName(t, got, "variables", "$env")
	parsertest.AssertStringFieldValue(t, envItem, "context", "Config")
	parsertest.AssertStringFieldValue(t, envItem, "class_context", "Config")

	serviceItem := parsertest.AssertBucketItemByName(t, got, "variables", "$service")
	parsertest.AssertStringFieldValue(t, serviceItem, "type", "Service")
	parsertest.AssertStringFieldValue(t, serviceItem, "context", "run")
	phpAssertNilField(t, serviceItem, "class_context")

	infoCall := parsertest.AssertBucketItemByName(t, got, "function_calls", "info")
	parsertest.AssertStringFieldValue(t, infoCall, "full_name", "$service.info")
	parsertest.AssertStringSliceEquals(t, infoCall, "args", []string{"$greeting"})
	assertCallContextTuple(t, infoCall, "run", "method_declaration", 8)
	phpAssertAnySliceFieldValue(t, infoCall, "class_context", []any{nil, nil})

	warnCall := parsertest.AssertBucketItemByName(t, got, "function_calls", "warn")
	parsertest.AssertStringFieldValue(t, warnCall, "full_name", "Logger.warn")
	parsertest.AssertStringSliceEquals(t, warnCall, "args", []string{"\"warn\""})
	parsertest.AssertStringFieldValue(t, warnCall, "inferred_obj_type", "Logger")
	assertCallContextTuple(t, warnCall, "run", "method_declaration", 8)
	phpAssertAnySliceFieldValue(t, warnCall, "class_context", []any{nil, nil})

	newCall := parsertest.AssertBucketItemByName(t, got, "function_calls", "Service")
	parsertest.AssertStringFieldValue(t, newCall, "full_name", "Service")
	parsertest.AssertStringSliceEquals(t, newCall, "args", []string{"\"main\""})
	assertCallContextTuple(t, newCall, "run", "method_declaration", 8)
	phpAssertAnySliceFieldValue(t, newCall, "class_context", []any{nil, nil})
}

func TestDefaultEngineParsePathPHPEmitsStaticMethodReceiverMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "static_calls.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace Demo;

class Logger {
    public static function warn(string $message): void {}
}

class Config {
    public function run(): void {
        Logger::warn("warn");
        \Demo\Logger::warn("namespaced");
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	warnCall := parsertest.AssertBucketItemByName(t, got, "function_calls", "warn")
	parsertest.AssertStringFieldValue(t, warnCall, "full_name", "Logger.warn")
	parsertest.AssertStringFieldValue(t, warnCall, "inferred_obj_type", "Logger")

	namespacedCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "Demo\\Logger.warn")
	parsertest.AssertStringFieldValue(t, namespacedCall, "name", "warn")
	parsertest.AssertStringFieldValue(t, namespacedCall, "inferred_obj_type", "Demo\\Logger")
}

func TestDefaultEngineParsePathPHPEmitsNullsafeReceiverMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "nullsafe_calls.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Session {
    public Service $service;
}

class Config {
    public function run(string $message): void {
        $session = new Session();
        $session?->service?->info($message);
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	sessionItem := parsertest.AssertBucketItemByName(t, got, "variables", "$session")
	parsertest.AssertStringFieldValue(t, sessionItem, "type", "Session")

	infoCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$session->service.info")
	parsertest.AssertStringFieldValue(t, infoCall, "name", "info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersTypedThisPropertyReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "typed_property_calls.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Config {
    private Service $service;

    public function run(string $message): void {
        $this->service->info($message);
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	infoCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$this->service.info")
	parsertest.AssertStringFieldValue(t, infoCall, "name", "info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPEmitsPropertyTypeInferenceFromDeclaration(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "typed_properties.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Config {
    private string $env;
    private ?Service $service = null;
    private bool $debug = false;
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	envItem := parsertest.AssertBucketItemByName(t, got, "variables", "$env")
	parsertest.AssertStringFieldValue(t, envItem, "type", "string")

	serviceItem := parsertest.AssertBucketItemByName(t, got, "variables", "$service")
	parsertest.AssertStringFieldValue(t, serviceItem, "type", "Service")

	debugItem := parsertest.AssertBucketItemByName(t, got, "variables", "$debug")
	parsertest.AssertStringFieldValue(t, debugItem, "type", "bool")
}

func TestDefaultEngineParsePathPHPEmitsCallContextLineMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "context.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Demo {
    public function run(string $message): void {
        greet($message);
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	greetCall := parsertest.AssertBucketItemByName(t, got, "function_calls", "greet")
	assertCallContextTuple(t, greetCall, "run", "method_declaration", 3)
}

func TestDefaultEngineParsePathPHPMultilineArgumentsAndContextLineMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "multiline.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php

class Demo {
    public function build(
        string $name,
        array $options = [
            'flags' => ['cache' => true, 'retry' => false],
        ],
    ): void {
        render(
            title: "Hello",
            options: [
                'greeting' => greet($name),
                'service' => $this->service,
            ],
        );
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := parsertest.AssertBucketItemByName(t, got, "functions", "build")
	parsertest.AssertStringSliceEquals(t, functionItem, "parameters", []string{"$name", "$options"})

	renderCall := parsertest.AssertBucketItemByName(t, got, "function_calls", "render")
	parsertest.AssertStringSliceEquals(
		t,
		renderCall,
		"args",
		[]string{
			`title: "Hello"`,
			`options: [
                'greeting' => greet($name),
                'service' => $this->service,
            ]`,
		},
	)
	assertCallContextTuple(t, renderCall, "build", "method_declaration", 4)
}
