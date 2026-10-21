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

// TestValueflowFamilyRetired pins the #6626 value-flow lane retirement: the
// value-flow fixpoint aliases and thin forwarders plus the code-call family
// aliases and thin forwarders (deleted from compat_projection.go) whose
// callers now name [value]/[cleanup], [codecall]/[materialization]/
// [projection], [schemadecode], and [payloadcore] directly must stay absent
// from the whole reducer package directory — a re-declared alias in any file
// would still compile. The canonical subpackage spellings are the only ones.
func TestValueflowFamilyRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		// value-flow fixpoint (compat_projection.go value_flow stanza).
		"CodeValueFlowBackfillStateMarker",
		"CodeValueFlowStaleCleanupRunner",
		"CodeValueFlowStaleCleanupRunnerConfig",
		"CodeValueFlowCurrentGeneration",
		"CodeTaintStaleEvidenceRetractor",
		"CodeInterprocStaleEvidenceRetractor",
		"GraphValueFlowCloudSinkTargetLoader",
		"ValueFlowCloudSinkWorkloadRowsCypher",
		"ValueFlowCloudSinkTargetsByPairCypher",
		"ValueFlowFixpointComponentStore",
		"NewValueFlowFixpointCache",
		"ValueFlowProgramInput",
		"ValueFlowCallEdge",
		"BuildValueFlowProgram",
		"FunctionSummarySnapshotLoader",
		"FunctionSourceSnapshotLoader",
		"FunctionGraphIDSnapshotLoader",
		"ValueFlowFixpointEvidenceLoader",
		"ValueFlowFixpointEvidenceProjector",
		// code-call family (compat_projection.go code-call stanza).
		"CodeCallProjectionRunner",
		"CodeCallProjectionRunnerConfig",
		"DefaultCodeCallProjectionLeaseOwnerPrefix",
		"DefaultCodeCallAcceptanceScanLimit",
		"CodeCallProjectionFilePartitionKeyPrefix",
		"CodeCallProjectionPartitionCandidateReader",
		"CodeCallProjectionUnhashedCandidateReader",
		"CodeCallProjectionRefreshFenceLookup",
		"CodeCallMaterializationHandler",
		"CodeCallIntentWriter",
		"ExtractSymbolRuntimeIntentRows",
		"BuildHandlesRouteIntentRowsForQueryProof",
		// Thin helpers into the same leaves.
		"buildCodeCallProjectionContexts",
		"mapSlice",
		"pythonMetaclassEvidenceSource",
		"maxSharedSelectionScanLimit",
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
				if isRetiredValueflow(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredValueflow(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredValueflow(s.Name.Name, retired) {
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

func isRetiredValueflow(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}
