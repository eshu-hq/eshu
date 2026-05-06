package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPEmitsTraitAdaptationMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "trait_adaptation_metadata.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classItem := assertBucketItemByName(t, got, "classes", "Child")
	phpAssertStringSliceFieldValue(t, classItem, "bases", []string{"Loggable", "Auditable"})
	phpAssertStringSliceFieldValue(t, classItem, "trait_adaptations", []string{
		"Auditable::record insteadof Loggable",
		"Loggable::record as private logRecord",
	})
}
