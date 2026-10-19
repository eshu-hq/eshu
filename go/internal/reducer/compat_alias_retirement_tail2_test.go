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

// TestTailBucket2CorrelationProjectFamilyRetired pins the #6626 tail bucket 2
// retirement: the cicdrun, crossrepo, supplychaincore, sharedintent, worker,
// and crossscope aliases and thin forwarders (deleted from
// compat_correlation.go, compat_projection.go, and compat_decode.go) whose
// callers now name the owning subpackage directly must stay absent from the
// whole reducer package directory — a re-declared alias in any file would
// still compile. The canonical subpackage spellings are the only ones.
func TestTailBucket2CorrelationProjectFamilyRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		// cicdrun (compat_correlation.go ci-cd-run stanza).
		"CICDRunCorrelationExact",
		"CICDRunCorrelationDerived",
		"CICDRunCorrelationDecision",
		"CICDRunCorrelationWrite",
		"CICDRunCorrelationWriteResult",
		"CICDRunCorrelationWriter",
		"CICDRunCorrelationHandler",
		"PostgresCICDRunCorrelationWriter",
		"cicdWorkflowImageBuiltFromEvidenceSource",
		// crossrepo (compat_correlation.go cross-repo stanza).
		"CrossRepoEvidenceSource",
		"EvidenceFactLoader",
		"AssertionLoader",
		"ResolutionPersister",
		"ResolvedUpsertResult",
		"CompletionWakeResult",
		"DeployableUnitCorrelationWaker",
		"ScopeRepositoryReader",
		"RepoDependencyIntentWriter",
		"CrossRepoRelationshipHandler",
		// supplychaincore (compat_correlation.go supply-chain stanza).
		"SupplyChainImpactFactFilter",
		"SupplyChainImpactFinding",
		"SupplyChainImpactAffectedExact",
		"SupplyChainImpactWrite",
		"SupplyChainImpactWriteResult",
		"SupplyChainImpactHandler",
		"PostgresSupplyChainImpactWriter",
		"SupplyChainImpactWriter",
		"SupplyChainImpactFencingTokenIssuer",
		"SupplyChainImpactWriteSupersededFailureClass",
		"SupplyChainImpactTx",
		"SupplyChainImpactBeginner",
		"SupplyChainImpactWinnersMaintainer",
		"JVMReachabilityFactFilter",
		// sharedintent (compat_correlation.go + compat_projection.go stanzas).
		"SharedProjectionIntentInput",
		"SharedProjectionAcceptanceKey",
		"SharedProjectionUnroutableRow",
		"SharedProjectionWriteReport",
		"SharedProjectionEdgeWriter",
		"PartitionLeaseManager",
		"AcceptedGenerationLookup",
		"AcceptedGenerationPrefetch",
		"UnroutableReasonMissingRequiredField",
		"UnroutableReasonNoStatementForType",
		"RepoRefreshIntentType",
		"repoRefreshAction",
		"retractViaRefreshKey",
		// worker (compat_correlation.go + compat_projection.go stanzas).
		"FirstProjectionLookup",
		"PartitionProcessorConfig",
		"PartitionProcessResult",
		"defaultBatchLimit",
		"defaultSharedPollInterval",
		"defaultEvidenceSource",
		"DefaultSharedProjectionLeaseOwnerPrefix",
		"sharedProjectionDomains",
		"SharedProjectionRunnerConfig",
		"SharedProjectionRunner",
		"LoadSharedProjectionConfig",
		"SharedProjectionPartitionCandidateReader",
		"SharedProjectionUnhashedCandidateReader",
		"sharedAcceptanceLookupEvent",
		"sharedAcceptanceTelemetry",
		// crossscope (compat_decode.go cross-scope-readiness stanza).
		"CrossScopeProducerNotReadyFailureClass",
		"crossScopeProducerNotReadyError",
		"CrossScopeProducerReadiness",
		"CrossScopeProducerReadinessByDomain",
		// Thin forwarders into the same six families.
		"BuildSharedProjectionIntent",
		"CarriesNoEdge",
		"repoWideRetractRefreshPartitionKey",
		"ProcessPartitionOnce",
		"LatestIntentsByRepoAndPartition",
		"FilterAuthoritativeIntents",
		"RepoWideRetractDomains",
		"deltaScopeRepositorySet",
		"uniqueRepositoryIDs",
		"applyRepoRefreshDeltaScope",
		"sharedProjectionRowRepoID",
		"CrossScopeCompletionEdges",
		"crossScopeDependenciesForRegistration",
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
				if isRetiredTailBucket2(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredTailBucket2(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredTailBucket2(s.Name.Name, retired) {
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

func isRetiredTailBucket2(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}
