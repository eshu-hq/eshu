package parser

import (
	"path/filepath"
	"testing"
)

func TestGoFunctionRowsCarryPackageImportPathWhenKnown(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

func handle(x string) string { return x }
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{GoPackageImportPath: "example.com/repo/handlers"})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	handle := goFunctionRowByName(t, got, "handle")
	if got, want := handle["package_import_path"], "example.com/repo/handlers"; got != want {
		t.Fatalf("package_import_path = %#v, want %#v", got, want)
	}
}

func TestGoFunctionRowsOmitBlankPackageImportPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

func handle(x string) string { return x }
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	handle := goFunctionRowByName(t, got, "handle")
	if _, present := handle["package_import_path"]; present {
		t.Fatalf("package_import_path present without package identity: %+v", handle)
	}
}

func goFunctionRowByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()

	rows, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions bucket missing or wrong type: %T", payload["functions"])
	}
	for _, row := range rows {
		if got, _ := row["name"].(string); got == name {
			return row
		}
	}
	t.Fatalf("function row for %q not found: %+v", name, rows)
	return nil
}
