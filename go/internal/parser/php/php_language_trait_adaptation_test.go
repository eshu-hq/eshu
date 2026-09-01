// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPEmitsTraitAdaptationMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "trait_adaptation_metadata.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Child {
    use Loggable, Auditable {
        Auditable::record insteadof Loggable;
        Loggable::record as private logRecord;
    }
}

trait Loggable {
}

trait Auditable {
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

	classItem := parsertest.AssertBucketItemByName(t, got, "classes", "Child")
	parsertest.AssertStringSliceEquals(t, classItem, "bases", []string{"Loggable", "Auditable"})
	parsertest.AssertStringSliceEquals(t, classItem, "trait_adaptations", []string{
		"Auditable::record insteadof Loggable",
		"Loggable::record as private logRecord",
	})
}
