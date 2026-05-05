package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersDirectFreeFunctionReturnReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_call_chain.php")
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
        createService()->info($message);
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

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "createService().info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersFreeFunctionReturnCallChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_call_chain.php")
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

function createFactory(): Factory {
    return new Factory();
}

class Config {
    public function run(string $message): void {
        createFactory()->createService()->info($message);
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

	factoryCall := assertBucketItemByName(t, got, "functions", "createFactory")
	phpAssertStringFieldValue(t, factoryCall, "return_type", "Factory")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "name", "info")
	phpAssertStringFieldContains(t, infoCall, "full_name", "createFactory")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}
