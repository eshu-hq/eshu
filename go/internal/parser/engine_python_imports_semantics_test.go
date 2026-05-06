package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonEmitsModuleImportSourceIdentity(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("sample_projects", "sample_project")
	filePath := filepath.Join(repoRoot, "module_a.py")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	assertPythonImportEntry(t, got, "module_b", "mb", "./module_b")
	assertPythonImportEntry(t, got, "process_data", "", "./module_b")
}

func TestDefaultEngineParsePathPythonEmitsRelativeImportSourceIdentity(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "python_comprehensive")
	filePath := filepath.Join(repoRoot, "complex_imports.py")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	assertPythonImportEntry(t, got, "basic", "", "./basic")
	assertPythonImportEntry(t, got, "timer", "", "./decorators")
	assertPythonImportEntry(t, got, "retry", "", "./decorators")
	assertPythonImportEntry(t, got, "Animal", "", "./inheritance")
	assertPythonImportEntry(t, got, "Dog", "DogClass", "./inheritance")
}

func assertPythonImportEntry(
	t *testing.T,
	payload map[string]any,
	name string,
	alias string,
	source string,
) {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		if got, _ := item["name"].(string); got != name {
			continue
		}
		if got, _ := item["source"].(string); got != source {
			continue
		}
		if alias == "" {
			if _, ok := item["alias"]; ok {
				if got, _ := item["alias"].(string); got != "" {
					continue
				}
			}
		} else if got, _ := item["alias"].(string); got != alias {
			continue
		}
		return
	}
	t.Fatalf("imports missing name=%q alias=%q source=%q in %#v", name, alias, source, items)
}
