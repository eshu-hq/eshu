// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFactLoaderDomainFamilyRetired pins the #6626 FactLoader/Domain bucket
// retirement: the FactLoader alias, the Domain* const aliases, and the
// allProjectionDomains forwarder (in compat_decode.go) whose callers now name
// [factload.FactLoader] and [reducercontract] directly must stay absent from
// the whole reducer package directory — a re-declared alias in any file would
// still compile. The canonical subpackage spellings are the only ones.
func TestFactLoaderDomainFamilyRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		// scoped fact loader (compat_decode.go scoped_fact_loader stanza).
		"FactLoader",
		// shared-projection domain consts (compat_decode.go
		// shared-projection domains stanza).
		"DomainRepoDependency",
		"DomainWorkloadDependency",
		"DomainCodeCalls",
		"DomainSQLRelationships",
		"DomainShellExec",
		"DomainInheritanceEdges",
		"DomainDocumentationEdges",
		"DomainRationaleEdges",
		"DomainDeployableUnitEdges",
		"DomainHandlesRoute",
		"DomainRunsIn",
		"DomainInvokesCloudAction",
		"DomainCodeownersOwnershipEdges",
		"DomainSubmodulePinEdges",
		// projection domain list forwarder (same stanza).
		"allProjectionDomains",
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed, cannot locate package directory")
	}
	pkgDir := filepath.Dir(thisFile)

	fset := token.NewFileSet()
	// Walk every file in the package directory (parser.ParseFile per
	// file — parser.ParseDir is deprecated since Go 1.25) so a
	// re-declared alias in any sibling compat file is still seen.
	files, err := filepath.Glob(filepath.Join(pkgDir, "*.go"))
	if err != nil {
		t.Fatalf("filepath.Glob(%q) error = %v", pkgDir, err)
	}

	declared := map[string]string{}
	for _, fileName := range files {
		f, err := parser.ParseFile(fset, fileName, nil, 0)
		if err != nil {
			t.Fatalf("parser.ParseFile(%q) error = %v", fileName, err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if isRetiredDecodeBucket(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredDecodeBucket(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredDecodeBucket(s.Name.Name, retired) {
							declared[s.Name.Name] = fileName
						}
					}
				}
			}
		}
	}

	for _, name := range retired {
		if fileName, found := declared[name]; found {
			t.Errorf("%s still declares %q, want it retired (callers name the owning subpackage directly)",
				filepath.Base(fileName), name)
		}
	}
}
