package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPEmitsInheritanceAndImportMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "types.php")
	writeTestFile(
		t,
		filePath,
		`<?php
namespace Demo;

use Demo\Library\Config as AppConfig;
use Demo\Library\Service;

class Child extends ParentClass implements Runnable, JsonSerializable {
    use Loggable, Auditable;
}

interface Repository extends Identifiable, Countable {
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
	phpAssertStringSliceFieldValue(t, classItem, "bases", []string{"ParentClass", "Runnable", "JsonSerializable", "Loggable", "Auditable"})
	phpAssertStringSliceFieldValue(t, classItem, "implemented_interfaces", []string{"Runnable", "JsonSerializable"})

	interfaceItem := assertBucketItemByName(t, got, "interfaces", "Repository")
	phpAssertStringSliceFieldValue(t, interfaceItem, "bases", []string{"Identifiable", "Countable"})

	importItem := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Config")
	phpAssertStringFieldValue(t, importItem, "full_import_name", "use Demo\\Library\\Config as AppConfig;")
	phpAssertStringFieldValue(t, importItem, "alias", "AppConfig")
	phpAssertBoolFieldValue(t, importItem, "is_dependency", false)

	secondImport := assertBucketItemByName(t, got, "imports", "Demo\\Library\\Service")
	phpAssertStringFieldValue(t, secondImport, "full_import_name", "use Demo\\Library\\Service;")
	if alias, ok := secondImport["alias"]; ok && alias != nil && alias != "" {
		t.Fatalf("alias = %#v, want nil or empty", alias)
	}
}

func TestDefaultEngineParsePathPHPEmitsConstructorIntent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "construct.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Service {
}

function build(): Service {
    return new Service();
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "Service")
	phpAssertStringFieldValue(t, call, "call_kind", "constructor_call")
}

func TestDefaultEngineParsePathPHPEmitsTraitAdaptationBases(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "trait_adaptation.php")
	writeTestFile(
		t,
		filePath,
		`<?php
namespace Demo;

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
}
