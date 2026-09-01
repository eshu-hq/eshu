// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPInfersAliasedNewExpressionReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "aliased_new.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	parsertest.AssertStringFieldValue(t, loggerItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")

	newServiceCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "new Service().info")
	parsertest.AssertStringFieldValue(t, newServiceCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersAliasedThisPropertyReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "aliased_property.php")
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
        $logger = $this->service;
        $logger->info($message);
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

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	parsertest.AssertStringFieldValue(t, loggerItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersPropertyChainAliasReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "property_chain_alias.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	parsertest.AssertStringFieldValue(t, loggerItem, "type", "Logger")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Logger")
}

func TestDefaultEngineParsePathPHPInfersMethodReturnTypeAliasedReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "return_type_alias.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	factoryMethod := parsertest.AssertBucketItemByName(t, got, "functions", "createService")
	parsertest.AssertStringFieldValue(t, factoryMethod, "return_type", "Service")

	serviceItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$service")
	parsertest.AssertStringFieldValue(t, serviceItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$service.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersFreeFunctionReturnTypeAliasedReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_return_alias.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	createService := parsertest.AssertBucketItemByName(t, got, "functions", "createService")
	parsertest.AssertStringFieldValue(t, createService, "return_type", "Service")

	serviceItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$service")
	parsertest.AssertStringFieldValue(t, serviceItem, "type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$service.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersMethodReturnPropertyChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_return_property_chain.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	parsertest.AssertStringFieldValue(t, loggerItem, "type", "Logger")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Logger")
}

func TestDefaultEngineParsePathPHPInfersChainedStaticFactoryReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "chained_factory.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "name", "info")
	phpAssertStringFieldContains(t, infoCall, "full_name", "createService")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersImportedTypeAliasReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imported_alias.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	configItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$config")
	parsertest.AssertStringFieldValue(t, configItem, "type", "Config")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$config.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Config")
}

func TestDefaultEngineParsePathPHPInfersImportedStaticTypeAliasReceiverChains(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imported_static_alias.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "AppFactory::instance()->createService().info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}
