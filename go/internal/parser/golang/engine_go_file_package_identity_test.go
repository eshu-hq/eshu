// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestDefaultEngineParsePathGoEmitsFilePackageIdentity is the #7347
// projector-mapping source: the file's own package import path, taken from
// the collector's per-directory prescan (Options.GoPackageImportPath), is
// reported at the payload top level so the projector can place the file in
// its package without inventing grouping. Files parsed without package
// identity carry no key, keeping those payloads byte-identical.
func TestDefaultEngineParsePathGoEmitsFilePackageIdentity(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	parsertest.WriteFile(t, filePath, `package handlers

import "fmt"

func Handle() { fmt.Println("x") }
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	withIdentity, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{
		GoPackageImportPath: "example.com/repo/handlers",
	})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	if got := withIdentity["package_import_path"]; got != "example.com/repo/handlers" {
		t.Fatalf("package_import_path = %#v, want the file's package import path", got)
	}

	withoutIdentity, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	if value, present := withoutIdentity["package_import_path"]; present {
		t.Fatalf("package_import_path = %#v without package identity, want absent", value)
	}
}
