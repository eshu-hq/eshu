package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersParentStaticReceiverCallChains(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "parent_static.php")
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

class Child extends Factory {
    public function run(string $message): void {
        parent::instance()->createService()->info($message);
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "parent::instance()->createService().info")
	phpAssertStringFieldValue(t, call, "inferred_obj_type", "Service")
}
