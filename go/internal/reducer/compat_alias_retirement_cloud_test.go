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

// TestCloudBucketFamilyRetired pins the #6626 cloud bucket retirement: the
// awscloud, containerimage, observability-coverage, cloud-join, platform,
// shell-exec, and partitioning aliases and thin forwarders (deleted from
// compat_cloud.go) whose callers now name the owning subpackage directly must
// stay absent from the whole reducer package directory — a re-declared alias
// in any file would still compile. The canonical subpackage spellings are the
// only ones.
func TestCloudBucketFamilyRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		// awscloud (compat_cloud.go aws-cloud stanza).
		"AWSCloudImageNodesNotReadyFailureClass",
		"awsCloudImageMaterializationDomainDefinition",
		"AWSCloudRuntimeDriftWriteSupersededFailureClass",
		"AWSCloudRuntimeDriftStatePendingFailureClass",
		"CloudResourceContainerImageEdgeWriter",
		"AWSCloudImageMaterializationHandler",
		"AWSCloudRuntimeDriftEvidenceLoader",
		"AWSCloudRuntimeDriftFindingWriter",
		"AWSCloudRuntimeDriftWrite",
		"AWSCloudRuntimeDriftWriteResult",
		"AWSCloudRuntimeDriftHandler",
		"AWSCloudRuntimeDriftReadinessChecker",
		"AWSCloudRuntimeDriftFencingTokenIssuer",
		"AWSCloudRuntimeDriftTx",
		"PostgresAWSCloudRuntimeDriftWriter",
		// containerimage (compat_cloud.go container-image stanza).
		"ContainerImageIdentityWrite",
		"ContainerImageIdentityDecision",
		"ContainerImageIdentityWriteResult",
		"ContainerImageIdentityWriter",
		"ContainerImageIdentityHandler",
		"ContainerImageIdentityPriorSupport",
		"ContainerImageIdentityTransaction",
		"PostgresContainerImageIdentityWriter",
		"PostgresContainerImageIdentitySupportWriter",
		"ContainerImageExistenceLookup",
		"GraphContainerImageExistenceLookup",
		"ContainerImageProvenanceEdgeWriter",
		"ContainerImageDerivedFromEdgeWriter",
		"BuildContainerImageIdentityDecisions",
		"digestFromImageRef",
		"containerImageBuiltFromRows",
		"containerImageDerivedFromRows",
		"containerImageBuiltFromProvenanceEvidenceSource",
		"containerImageDerivedFromProvenanceEvidenceSource",
		// observability-coverage (compat_cloud.go observability stanza).
		"ObservabilityCoverageCorrelationHandler",
		"ObservabilityCoverageCorrelationWriter",
		"ObservabilityCoverageCorrelationWrite",
		"ObservabilityCoverageCorrelationWriteResult",
		"ObservabilityCoverageMaterializationHandler",
		"ObservabilityCoverageEdgeWriter",
		"PostgresObservabilityCoverageCorrelationWriter",
		"ObservabilityCoverageEvidenceSource",
		"observabilityCoverageMaterializationDomainDefinition",
		"ObservabilityCoverageNodesNotReadyFailureClass",
		// cloud-join (compat_cloud.go cloud-resource-join-index stanza).
		"cloudResourceJoinIndex",
		"buildCloudResourceJoinIndex",
		"cloudResourceUID",
		// platform (compat_cloud.go platform stanza).
		"PlatformMaterializationWrite",
		"PlatformMaterializationWriteResult",
		"PlatformMaterializationWriter",
		"PlatformGraphLocker",
		"WorkloadMaterializationReplayer",
		"PlatformMaterializationHandler",
		"PostgresPlatformMaterializationWriter",
		"RuntimeFamilies",
		"LookupRuntimeFamily",
		"InferInfrastructureRuntimeFamilyKind",
		"MatchesServiceModuleSource",
		"TerraformPlatformEvidenceKind",
		// shell-exec (compat_cloud.go shell-exec stanza).
		"ShellExecIntentWriter",
		"ShellExecMaterializationHandler",
		"shellExecMaterializationFactKinds",
		"ExtractShellExecRows",
		"loadShellExecMaterializationFacts",
		"buildShellExecRefreshIntents",
		"buildShellExecSharedIntentRows",
		// partitioning (compat_cloud.go partitioning stanza).
		"PartitionHashForKey",
		"PartitionForKey",
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
				if isRetiredCloudBucket(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredCloudBucket(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredCloudBucket(s.Name.Name, retired) {
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

func isRetiredCloudBucket(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}
