package parser

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersAliasedNewExpressionReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "aliased_new.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Config {
    public function run(string $message): void {
        $service = new Service();
        $logger = $service;
        $logger->info($message);
        new Service()->info($message);
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

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	phpAssertStringFieldValue(t, loggerItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")

	newServiceCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "new Service().info")
	phpAssertStringFieldValue(t, newServiceCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersAliasedThisPropertyReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "aliased_property.php")
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
        $logger = $this->service;
        $logger->info($message);
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

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	phpAssertStringFieldValue(t, loggerItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersPropertyChainAliasReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "property_chain_alias.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Logger {
    public function info(string $message): void {}
}

class Container {
    public Logger $logger;
}

class Config {
    private Container $container;

    public function run(string $message): void {
        $logger = $this->container->logger;
        $logger->info($message);
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

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	phpAssertStringFieldValue(t, loggerItem, "type", "Logger")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Logger")
}

func TestDefaultEngineParsePathPHPInfersMethodReturnTypeAliasedReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "return_type_alias.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Factory {
    public function createService(): Service {
        return new Service();
    }
}

class Config {
    private Factory $factory;

    public function run(string $message): void {
        $service = $this->factory->createService();
        $service->info($message);
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

	factoryMethod := assertBucketItemByName(t, got, "functions", "createService")
	phpAssertStringFieldValue(t, factoryMethod, "return_type", "Service")

	serviceItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$service")
	phpAssertStringFieldValue(t, serviceItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$service.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersFreeFunctionReturnTypeAliasedReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_return_alias.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

function createService(): Service {
    return new Service();
}

class Config {
    public function run(string $message): void {
        $service = createService();
        $service->info($message);
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

	createService := assertBucketItemByName(t, got, "functions", "createService")
	phpAssertStringFieldValue(t, createService, "return_type", "Service")

	serviceItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$service")
	phpAssertStringFieldValue(t, serviceItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$service.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersMethodReturnPropertyChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_return_property_chain.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Logger {
    public function info(): void {}
}

class Service {
    public Logger $logger;
}

class Factory {
    public function createService(): Service {
        return new Service();
    }
}

class Config {
    private Factory $factory;

    public function run(): void {
        $logger = $this->factory->createService()->logger;
        $logger->info();
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

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	phpAssertStringFieldValue(t, loggerItem, "type", "Logger")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Logger")
}

func TestDefaultEngineParsePathPHPInfersChainedStaticFactoryReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "chained_factory.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Factory {
    public static function instance(): Factory {
        return new Factory();
    }

    public function createService(): Service {
        return new Service();
    }
}

class Config {
    public function run(string $message): void {
        Factory::instance()->createService()->info($message);
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

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "name", "info")
	phpAssertStringFieldContains(t, infoCall, "full_name", "createService")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersImportedTypeAliasReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imported_alias.php")
	writeTestFile(
		t,
		filePath,
		`<?php
namespace Demo;

use Demo\Library\Config as AppConfig;

class ConfigRunner {
    public function run(string $message): void {
        $config = new AppConfig();
        $config->info($message);
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

	configItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$config")
	phpAssertStringFieldValue(t, configItem, "type", "Config")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$config.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Config")
}

func TestDefaultEngineParsePathPHPInfersImportedStaticTypeAliasReceiverChains(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imported_static_alias.php")
	writeTestFile(
		t,
		filePath,
		`<?php
namespace Demo;

use Demo\Library\Factory as AppFactory;

class Service {
    public function info(string $message): void {}
}

class Factory {
    public static function instance(): Factory {
        return new Factory();
    }

    public function createService(): Service {
        return new Service();
    }
}

class ConfigRunner {
    public function run(string $message): void {
        AppFactory::instance()->createService()->info($message);
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

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "AppFactory::instance()->createService().info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func phpAssertStringFieldValue(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func phpAssertStringFieldContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if !strings.Contains(got, want) {
		t.Fatalf("%s = %#v, want to contain %#v", field, got, want)
	}
}

func phpAssertBoolFieldValue(t *testing.T, item map[string]any, field string, want bool) {
	t.Helper()

	got, ok := item[field].(bool)
	if !ok {
		t.Fatalf("%s = %T, want bool", field, item[field])
	}
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func phpAssertStringSliceFieldValue(t *testing.T, item map[string]any, field string, want []string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func phpAssertAnySliceFieldValue(t *testing.T, item map[string]any, field string, want []any) {
	t.Helper()

	got, ok := item[field].([]any)
	if !ok {
		t.Fatalf("%s = %T, want []any", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func phpAssertNilField(t *testing.T, item map[string]any, field string) {
	t.Helper()

	if value, ok := item[field]; ok && value != nil {
		t.Fatalf("%s = %#v, want nil", field, value)
	}
}

func assertBucketItemByFieldValue(
	t *testing.T,
	payload map[string]any,
	bucket string,
	field string,
	want string,
) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		value, _ := item[field].(string)
		if value == want {
			return item
		}
	}
	t.Fatalf("%s missing %s=%q in %#v", bucket, field, want, items)
	return nil
}

func assertCallContextTuple(
	t *testing.T,
	item map[string]any,
	wantName string,
	wantType string,
	wantLine int,
) {
	t.Helper()

	context, ok := item["context"].([]any)
	if !ok {
		t.Fatalf("context = %T, want []any", item["context"])
	}
	if len(context) < 3 {
		t.Fatalf("context = %#v, want at least 3 items", context)
	}
	if got, _ := context[0].(string); got != wantName {
		t.Fatalf("context[0] = %#v, want %#v", got, wantName)
	}
	if got, _ := context[1].(string); got != wantType {
		t.Fatalf("context[1] = %#v, want %#v", got, wantType)
	}
	if got, ok := context[2].(int); !ok || got != wantLine {
		t.Fatalf("context[2] = %#v, want %#v", context[2], wantLine)
	}
}
