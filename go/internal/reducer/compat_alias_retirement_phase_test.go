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

// TestBucket9PhaseRetired pins the #6626 batch 6 bucket 9 retirement: the
// graph-projection phase alias block (10 type aliases + 20 const aliases,
// deleted with graph_projection_phase.go, canonical in gpphase) must stay
// absent from the whole reducer package directory — a re-declared alias in
// any file would still compile. The canonical gpphase spellings are the
// only ones. The Repair family (compat_correlation.go stanza, canonical in
// gpphase.PhaseRepair* and repair.*) is a different bucket and out of scope.
func TestBucket9PhaseRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		"GraphProjectionKeyspace",
		"GraphProjectionPhase",
		"GraphProjectionPhaseKey",
		"GraphProjectionPhaseState",
		"GraphProjectionReadinessLookup",
		"GraphProjectionReadinessPrefetch",
		"GraphProjectionPhasePublisher",
		"EndpointPresenceRow",
		"EndpointPresenceWriter",
		"EndpointPresenceLookup",
		"GraphProjectionKeyspaceAPIEndpointRepoPath",
		"GraphProjectionKeyspaceCloudResourceUID",
		"GraphProjectionKeyspaceCodeEntitiesUID",
		"GraphProjectionKeyspaceCrossRepoEvidence",
		"GraphProjectionKeyspaceDeployableUnitUID",
		"GraphProjectionKeyspaceKubernetesWorkloadUID",
		"GraphProjectionKeyspaceRepoWorkloadPresence",
		"GraphProjectionKeyspaceSecurityGroupEndpointUID",
		"GraphProjectionKeyspaceSecurityGroupRuleUID",
		"GraphProjectionKeyspaceServiceUID",
		"GraphProjectionKeyspaceTerraformModuleUID",
		"GraphProjectionKeyspaceTerraformResourceUID",
		"GraphProjectionKeyspaceWebhookEventUID",
		"GraphProjectionPhaseBackwardEvidenceCommitted",
		"GraphProjectionPhaseCanonicalNodesCommitted",
		"GraphProjectionPhaseCrossSourceAnchorReady",
		"GraphProjectionPhaseDeployableUnitCorrelation",
		"GraphProjectionPhaseDeploymentMapping",
		"GraphProjectionPhaseSemanticNodesCommitted",
		"GraphProjectionPhaseWorkloadMaterialization",
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
				if isRetiredBucket9(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredBucket9(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredBucket9(s.Name.Name, retired) {
							declared[s.Name.Name] = fileName
						}
					}
				}
			}
		}
	}

	for _, name := range retired {
		if fileName, found := declared[name]; found {
			t.Errorf("%s still declares %q, want it retired (callers name gpphase directly)",
				filepath.Base(fileName), name)
		}
	}
}

func isRetiredBucket9(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}
