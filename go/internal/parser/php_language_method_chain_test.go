package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersMethodReturnCallChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_return_call_chain.php")
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
        $this->factory->createService()->info($message);
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

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$this->factory->createService().info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersMethodReturnPropertyDereferenceReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_return_property_dereference.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Logger {
    public function info(string $message): void {}
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

    public function run(string $message): void {
        $this->factory->createService()->logger->info($message);
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

	loggerCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$this->factory->createService()->logger.info")
	phpAssertStringFieldValue(t, loggerCall, "inferred_obj_type", "Logger")
}
