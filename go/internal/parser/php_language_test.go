package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPEmitsFunctionParametersSourceAndContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "functions.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := assertBucketItemByName(t, got, "functions", "greet")
	phpAssertStringSliceFieldValue(t, functionItem, "parameters", []string{"$name", "$count"})
	phpAssertStringFieldContains(t, functionItem, "source", "return $prefix . $name;")

	methodItem := assertBucketItemByName(t, got, "functions", "run")
	phpAssertStringFieldValue(t, methodItem, "class_context", "Application")
	phpAssertStringFieldContains(t, methodItem, "source", "greet($message, 1);")
}

func TestDefaultEngineParsePathPHPEmitsInheritanceAndImportMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "types.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := assertBucketItemByName(t, got, "classes", "Child")
	phpAssertStringSliceFieldValue(t, classItem, "bases", []string{"ParentClass", "Runnable", "JsonSerializable", "Loggable", "Auditable"})

	interfaceItem := assertBucketItemByName(t, got, "interfaces", "Repository")
	phpAssertStringSliceFieldValue(t, interfaceItem, "bases", []string{"Identifiable", "Countable"})

	importItem := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Config")
	phpAssertStringFieldValue(t, importItem, "full_import_name", "use Demo\\Library\\Config as AppConfig;")
	phpAssertStringFieldValue(t, importItem, "alias", "AppConfig")
	phpAssertBoolFieldValue(t, importItem, "is_dependency", false)

	secondImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Service")
	phpAssertStringFieldValue(t, secondImport, "full_import_name", "use Demo\\Library\\Service;")
	if alias, ok := secondImport["alias"]; ok && alias != nil && alias != "" {
		t.Fatalf("alias = %#v, want nil or empty", alias)
	}
}

func TestDefaultEngineParsePathPHPEmitsTraitAdaptationBases(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "trait_adaptation.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := assertBucketItemByName(t, got, "classes", "Child")
	phpAssertStringSliceFieldValue(t, classItem, "bases", []string{"Loggable", "Auditable"})
}

func TestDefaultEngineParsePathPHPEmitsGroupedUseImportMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "grouped_use.php")
	writeTestFile(
		t,
		filePath,
		`<?php
namespace Demo;

use Demo\Library\{Config as AppConfig, Service, Logger\Stream as StreamLogger};

class Child {
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	configImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Config")
	phpAssertStringFieldValue(t, configImport, "alias", "AppConfig")

	serviceImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Service")
	if alias, ok := serviceImport["alias"]; ok && alias != nil && alias != "" {
		t.Fatalf("alias = %#v, want nil or empty", alias)
	}

	streamImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Logger\\Stream")
	phpAssertStringFieldValue(t, streamImport, "alias", "StreamLogger")
}

func TestDefaultEngineParsePathPHPEmitsGroupedUseFunctionAndConstImportKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "grouped_use_kinds.php")
	writeTestFile(
		t,
		filePath,
		`<?php
namespace Demo;

use function Demo\Library\{helper, format as format_value};
use const Demo\Library\{DEFAULT_LIMIT, MAX_VALUE as MAX_LIMIT};
use Demo\Library\Service;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	helperImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\helper")
	phpAssertStringFieldValue(t, helperImport, "import_type", "function")

	formatImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\format")
	phpAssertStringFieldValue(t, formatImport, "import_type", "function")
	phpAssertStringFieldValue(t, formatImport, "alias", "format_value")

	limitImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\DEFAULT_LIMIT")
	phpAssertStringFieldValue(t, limitImport, "import_type", "const")

	maxLimitImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\MAX_VALUE")
	phpAssertStringFieldValue(t, maxLimitImport, "import_type", "const")
	phpAssertStringFieldValue(t, maxLimitImport, "alias", "MAX_LIMIT")

	serviceImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Service")
	phpAssertStringFieldValue(t, serviceImport, "import_type", "use")
}

func TestDefaultEngineParsePathPHPEmitsMagicMethodClassification(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "magic_methods.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	getMethod := assertBucketItemByName(t, got, "functions", "__get")
	phpAssertStringFieldValue(t, getMethod, "class_context", "MagicBox")
	phpAssertStringFieldValue(t, getMethod, "semantic_kind", "magic_method")

	callMethod := assertBucketItemByName(t, got, "functions", "__call")
	phpAssertStringFieldValue(t, callMethod, "class_context", "MagicBox")
	phpAssertStringFieldValue(t, callMethod, "semantic_kind", "magic_method")
}

func TestDefaultEngineParsePathPHPEmitsVariableAndCallMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "calls.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	envItem := assertBucketItemByName(t, got, "variables", "$env")
	phpAssertStringFieldValue(t, envItem, "context", "Config")
	phpAssertStringFieldValue(t, envItem, "class_context", "Config")

	serviceItem := assertBucketItemByName(t, got, "variables", "$service")
	phpAssertStringFieldValue(t, serviceItem, "type", "Service")
	phpAssertStringFieldValue(t, serviceItem, "context", "run")
	phpAssertNilField(t, serviceItem, "class_context")

	infoCall := assertBucketItemByName(t, got, "function_calls", "info")
	phpAssertStringFieldValue(t, infoCall, "full_name", "$service.info")
	phpAssertStringSliceFieldValue(t, infoCall, "args", []string{"$greeting"})
	assertCallContextTuple(t, infoCall, "run", "method_declaration", 8)
	phpAssertAnySliceFieldValue(t, infoCall, "class_context", []any{nil, nil})

	warnCall := assertBucketItemByName(t, got, "function_calls", "warn")
	phpAssertStringFieldValue(t, warnCall, "full_name", "Logger.warn")
	phpAssertStringSliceFieldValue(t, warnCall, "args", []string{"\"warn\""})
	phpAssertStringFieldValue(t, warnCall, "inferred_obj_type", "Logger")
	assertCallContextTuple(t, warnCall, "run", "method_declaration", 8)
	phpAssertAnySliceFieldValue(t, warnCall, "class_context", []any{nil, nil})

	newCall := assertBucketItemByName(t, got, "function_calls", "Service")
	phpAssertStringFieldValue(t, newCall, "full_name", "Service")
	phpAssertStringSliceFieldValue(t, newCall, "args", []string{"\"main\""})
	assertCallContextTuple(t, newCall, "run", "method_declaration", 8)
	phpAssertAnySliceFieldValue(t, newCall, "class_context", []any{nil, nil})
}

func TestDefaultEngineParsePathPHPEmitsStaticMethodReceiverMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "static_calls.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	warnCall := assertBucketItemByName(t, got, "function_calls", "warn")
	phpAssertStringFieldValue(t, warnCall, "full_name", "Logger.warn")
	phpAssertStringFieldValue(t, warnCall, "inferred_obj_type", "Logger")

	namespacedCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "Demo\\Logger.warn")
	phpAssertStringFieldValue(t, namespacedCall, "name", "warn")
	phpAssertStringFieldValue(t, namespacedCall, "inferred_obj_type", "Demo\\Logger")
}

func TestDefaultEngineParsePathPHPEmitsNullsafeReceiverMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "nullsafe_calls.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	sessionItem := assertBucketItemByName(t, got, "variables", "$session")
	phpAssertStringFieldValue(t, sessionItem, "type", "Session")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$session->service.info")
	phpAssertStringFieldValue(t, infoCall, "name", "info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersTypedThisPropertyReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "typed_property_calls.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$this->service.info")
	phpAssertStringFieldValue(t, infoCall, "name", "info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPEmitsPropertyTypeInferenceFromDeclaration(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "typed_properties.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	envItem := assertBucketItemByName(t, got, "variables", "$env")
	phpAssertStringFieldValue(t, envItem, "type", "string")

	serviceItem := assertBucketItemByName(t, got, "variables", "$service")
	phpAssertStringFieldValue(t, serviceItem, "type", "Service")

	debugItem := assertBucketItemByName(t, got, "variables", "$debug")
	phpAssertStringFieldValue(t, debugItem, "type", "bool")
}

func TestDefaultEngineParsePathPHPEmitsCallContextLineMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "context.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	greetCall := assertBucketItemByName(t, got, "function_calls", "greet")
	assertCallContextTuple(t, greetCall, "run", "method_declaration", 3)
}

func TestDefaultEngineParsePathPHPMultilineArgumentsAndContextLineMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "multiline.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := assertBucketItemByName(t, got, "functions", "build")
	phpAssertStringSliceFieldValue(t, functionItem, "parameters", []string{"$name", "$options"})

	renderCall := assertBucketItemByName(t, got, "function_calls", "render")
	phpAssertStringSliceFieldValue(
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
