package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoFunctionRowsCarryPackageImportPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handler.go")
	writeTestFile(t, filePath, `package handlers

type Server struct{}

func (s *Server) Handle() {}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{
		GoPackageImportPath: "example.com/service/handlers",
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	handle := assertBucketItemByFieldValue(t, got, "functions", "name", "Handle")
	assertStringFieldValue(t, handle, "class_context", "Server")
	assertStringFieldValue(t, handle, "package_import_path", "example.com/service/handlers")
}

func TestDefaultEngineParsePathGoFunctionRowsOmitBlankPackageImportPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handler.go")
	writeTestFile(t, filePath, `package handlers

func Handle() {}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	handle := assertBucketItemByFieldValue(t, got, "functions", "name", "Handle")
	if _, ok := handle["package_import_path"]; ok {
		t.Fatalf("package_import_path = %#v, want omitted", handle["package_import_path"])
	}
}
