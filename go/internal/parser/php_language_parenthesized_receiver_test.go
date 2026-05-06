package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersParenthesizedMethodReturnCallChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "parenthesized_method_return_call_chain.php")
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
        ($this->factory->createService())->info($message);
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
