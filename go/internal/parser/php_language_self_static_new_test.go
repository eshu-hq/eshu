package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersSelfAndStaticInstantiationReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "self_static_instantiation.php")
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

    public function run(string $message): void {
        new self()->createService()->info($message);
        new static()->createService()->info($message);
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

	selfInfo := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "new self()->createService().info")
	phpAssertStringFieldValue(t, selfInfo, "inferred_obj_type", "Service")

	staticInfo := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "new static()->createService().info")
	phpAssertStringFieldValue(t, staticInfo, "inferred_obj_type", "Service")
}
