// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPEmitsAnonymousClassMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "anonymous_class.php")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := parsertest.AssertBucketItemByName(t, got, "classes", "anonymous_class_4")
	parsertest.AssertStringSliceEquals(t, classItem, "bases", []string{"Logger"})

	loggerItem := parsertest.AssertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	parsertest.AssertStringFieldValue(t, loggerItem, "type", "anonymous_class_4")

	infoCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "anonymous_class_4")
}
