package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPEmitsAnonymousClassMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "anonymous_class.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Config {
    public function run(string $message): void {
        $logger = new class extends Logger {
            public function info(string $message): void {
                return;
            }
        };
        $logger->info($message);
    }
}

class Logger {
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

	classItem := assertBucketItemByName(t, got, "classes", "anonymous_class_4")
	phpAssertStringSliceFieldValue(t, classItem, "bases", []string{"Logger"})

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	phpAssertStringFieldValue(t, loggerItem, "type", "anonymous_class_4")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "anonymous_class_4")
}
