// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// forbiddenHostImports names the module paths the host-owned scheduling
// vocabulary must never resolve to. Claim and scheduling state lives here so
// the planner, the trigger store, and intake keep working after the AWS
// collector tree is deleted (Lane 2); any production import below would drag
// that tree back in. Direct satellite imports are rejected for the same
// reason (arbiter Q2 ruling on #9296): the satellite meets the host through
// the versioned intake contract and the factschema schemas, never through a
// Go import.
var forbiddenHostImports = []string{
	"collector/awscloud",
	"eshu-collector-aws-cloud",
}

// TestProductionFilesHaveNoCollectorImports parses every non-test Go file in
// this package and fails on any import of the collector tree or the
// satellite. Test files are excluded: transition pins (notably the registry
// parity test in servicekinds_test.go) may import the registry until the
// scanner tree is deleted.
func TestProductionFilesHaveNoCollectorImports(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(packageDir(t))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		path := filepath.Join(packageDir(t), name)
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range forbiddenHostImports {
				if strings.Contains(importPath, forbidden) {
					t.Errorf("%s imports %q: host scheduling state must not resolve to the collector tree or the satellite", name, importPath)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no production Go files found in the freshness package")
	}
}

// packageDir resolves this package's directory from the test binary's own
// location so the guard does not depend on the go test working directory.
func packageDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Dir(currentFile)
}
