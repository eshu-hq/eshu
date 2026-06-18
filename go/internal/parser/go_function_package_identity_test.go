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
	if got, want := handle["scip_symbol"], "scip-go gomod example.com/repo/handlers handle()."; got != want {
		t.Fatalf("scip_symbol = %#v, want %#v", got, want)
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
	if _, present := handle["scip_symbol"]; present {
		t.Fatalf("scip_symbol present without package identity: %+v", handle)
	}
}

func TestGoMethodRowsCarryReceiverScopedSCIPSymbolWhenPackageKnown(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "client.go")
	writeTestFile(t, filePath, `package client

type Client struct{}

func (c *Client) Request() error { return nil }
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{GoPackageImportPath: "github.com/acme/lib/client"})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	request := goFunctionRowByName(t, got, "Request")
	if got, want := request["class_context"], "Client"; got != want {
		t.Fatalf("class_context = %#v, want %#v", got, want)
	}
	if got, want := request["scip_symbol"], "scip-go gomod github.com/acme/lib/client Client#Request()."; got != want {
		t.Fatalf("scip_symbol = %#v, want %#v", got, want)
	}
}

func TestGoPackageQualifiedCallsCarryStableSymbolKey(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.go")
	writeTestFile(t, filePath, `package main

import client "github.com/acme/lib/client"

func main() {
	_ = client.Request
	client.Request()
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{GoPackageImportPath: "github.com/acme/app"})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	call := goFunctionCallRowByFullName(t, got, "client.Request")
	if got, want := call["stable_symbol_key"], "scip-go gomod github.com/acme/lib/client Request()."; got != want {
		t.Fatalf("stable_symbol_key = %#v, want %#v; call=%+v", got, want, call)
	}
}

func TestGoPackageSemanticRootsDeriveNestedModuleImportPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	apiRoot := filepath.Join(repoRoot, "services", "api")
	filePath := filepath.Join(apiRoot, "handlers", "handler.go")
	writeTestFile(t, filepath.Join(apiRoot, "go.mod"), `module example.com/services/api

go 1.24
`)
	writeTestFile(t, filePath, `package handlers

func handle(x string) string { return x }
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	packageTargets, err := engine.PreScanGoPackageSemanticRoots(repoRoot, []string{filePath})
	if err != nil {
		t.Fatalf("PreScanGoPackageSemanticRoots() error = %v", err)
	}

	got := packageTargets[filepath.Dir(filePath)].ImportPath
	if want := "example.com/services/api/handlers"; got != want {
		t.Fatalf("ImportPath = %q, want %q", got, want)
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

func goFunctionCallRowByFullName(t *testing.T, payload map[string]any, fullName string) map[string]any {
	t.Helper()

	rows, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls bucket missing or wrong type: %T", payload["function_calls"])
	}
	for _, row := range rows {
		if got, _ := row["full_name"].(string); got == fullName {
			return row
		}
	}
	t.Fatalf("function call row for %q not found: %+v", fullName, rows)
	return nil
}
