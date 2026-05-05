package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersDirectSelfAndStaticReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "self_static_direct.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Config {
    public static function emit(string $message): void {}

    public function run(): void {
        self::emit("hello");
        static::emit("world");
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

	found := 0
	for _, item := range got["function_calls"].([]map[string]any) {
		fullName, _ := item["full_name"].(string)
		if fullName != "Config.emit" {
			continue
		}
		phpAssertStringFieldValue(t, item, "inferred_obj_type", "Config")
		found++
	}
	if found != 2 {
		t.Fatalf("found %d Config.emit calls, want 2 in %#v", found, got["function_calls"])
	}
}
