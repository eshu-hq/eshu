// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportBoundaryRejectsCoreAndCrossCollectorImports is the CI gate that
// keeps a standalone collector repository standalone: no Eshu internal
// implementation imports and no cross-collector implementation imports. Only
// the published SDK modules plus the standard library and yaml are allowed.
// The self-import prefix is derived from go.mod so renaming the module can
// never desync this allowlist.
func TestImportBoundaryRejectsCoreAndCrossCollectorImports(t *testing.T) {
	t.Parallel()

	allowedPrefixes := []string{
		modulePath(t),
		"github.com/eshu-hq/eshu/sdk/go/collector",
		"github.com/eshu-hq/eshu/sdk/go/factschema",
		"gopkg.in/yaml.v3",
	}
	violations := []string{}
	walk := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" || info.Name() == "compose" || info.Name() == "scripts" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("os.ReadFile(%s) error = %v", path, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parser.ParseFile(%s) error = %v", path, err)
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "C" {
				violations = append(violations, path+": cgo import is not allowed")
				continue
			}
			if isStandardOrRelative(importPath) {
				continue
			}
			allowed := false
			for _, prefix := range allowedPrefixes {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					allowed = true
					break
				}
			}
			if !allowed {
				violations = append(violations, path+": import "+importPath)
			}
		}
		return nil
	}
	if err := filepath.Walk(".", walk); err != nil {
		t.Fatalf("filepath.Walk error = %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("forbidden imports:\n%s", strings.Join(violations, "\n"))
	}
}

// modulePath reads the repository's own module path from go.mod so the
// self-import allowlist entry tracks renames automatically.
func modulePath(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("os.ReadFile(go.mod) error = %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			if strings.TrimSpace(path) == "" {
				break
			}
			return strings.TrimSpace(path)
		}
	}
	t.Fatal("go.mod declares no module path")
	return ""
}

func isStandardOrRelative(importPath string) bool {
	if strings.HasPrefix(importPath, ".") {
		return true
	}
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
