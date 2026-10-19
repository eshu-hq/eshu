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

// TestTailBucket1ServiceCatalogFamilyRetired pins the #6626 tail bucket 1
// retirement: the servicecatalog, sbomattest, summary (code/function/summary),
// secretsiam, iamcan, and iamescalation aliases (deleted from
// compat_correlation.go, compat_decode.go, and compat_projection.go) whose
// callers now name the owning subpackage directly must stay absent from the
// whole reducer package directory — a re-declared alias in any file would
// still compile. The canonical subpackage spellings are the only ones.
func TestTailBucket1ServiceCatalogFamilyRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		// servicecatalog (compat_correlation.go service-catalog stanza).
		"ServiceCatalogCorrelationOutcome",
		"ServiceCatalogCorrelationExact",
		"ServiceCatalogCorrelationDerived",
		"ServiceCatalogCorrelationAmbiguous",
		"ServiceCatalogCorrelationUnresolved",
		"ServiceCatalogCorrelationStale",
		"ServiceCatalogCorrelationRejected",
		"ServiceCatalogCorrelationDecision",
		"ServiceCatalogCorrelationWrite",
		"ServiceCatalogCorrelationWriteResult",
		"ServiceCatalogCorrelationWriter",
		"ServiceCatalogCorrelationHandler",
		"PostgresServiceCatalogCorrelationWriter",
		"BuildServiceCatalogCorrelationDecisions",
		"ServiceMaterializationWrite",
		"ServiceOwnershipEvidence",
		"ServiceEvidenceFamilyOwnership",
		"ServiceEvidenceFamilyRuntime",
		"ServiceMaterializationWriter",
		"ServiceMaterializationTx",
		"ServiceMaterializationRow",
		"PostgresServiceMaterializationWriter",
		"ServiceMaterializationGenerationID",
		"ServiceOwnershipEvidenceKey",
		"ServiceEvidencePayloadHash",
		"ServiceMaterializationStatusPending",
		"ServiceMaterializationStatusActive",
		"ServiceMaterializationStatusSuperseded",
		"RepositoryScopedRuntimeInstanceLoader",
		"ServiceRuntimeProducerReadiness",
		"ServiceRuntimeInstance",
		"ServiceRuntimeEvidenceKey",
		"ServiceScopedDocumentationEvidenceLoader",
		"ServiceScopedIncidentEvidenceLoader",
		"ServiceVulnerabilityAdvisoryLoader",
		"ServiceDocumentationRecord",
		"ServiceIncidentRecord",
		"ServiceVulnerabilityRecord",
		// sbomattest (compat_correlation.go sbom-attestation stanza).
		"SBOMAttachmentAttachedVerified",
		"SBOMAttestationAttachmentDecision",
		"SBOMAttestationAttachmentWrite",
		"SBOMAttestationAttachmentWriter",
		"SBOMAttestationAttachmentHandler",
		"PostgresSBOMAttestationAttachmentWriter",
		// summary (compat_decode.go code-function-summary stanza).
		"CodeFunctionSummaryLoader",
		"CodeFunctionSummaryWriter",
		"CodeFunctionSourceLoader",
		"CodeFunctionSourceWriter",
		"CodeFunctionGraphIDLoader",
		"CodeFunctionGraphIDWriter",
		"ValueFlowFixpointProjector",
		"CodeFunctionSummaryMaterializationHandler",
		"codeFunctionSummaryDomainDefinition",
		// secretsiam (compat_projection.go secrets-iam stanza).
		"SecretsIAMTrustChainLoadStats",
		"SecretsIAMTrustChainEvidenceLoader",
		"SecretsIAMTrustChainWriter",
		"PostgresSecretsIAMTrustChainWriter",
		"SecretsIAMTrustChainHandler",
		"SecretsIAMGraphWriter",
		"SecretsIAMGraphProjectionHandler",
		"SecretsIAMEndpointNotReadyFailureClass",
		"secretsIAMTrustChainDomainDefinition",
		"secretsIAMGraphProjectionDomainDefinition",
		// iamcan (compat_projection.go iam-can stanza).
		"IAMCanAssumeEdgeWriter",
		"IAMCanAssumeMaterializationHandler",
		"IAMCanPerformEdgeWriter",
		"IAMCanPerformMaterializationHandler",
		"IAMCanPerformTargetNotReadyFailureClass",
		"IAMCanAssumeNodesNotReadyFailureClass",
		"IAMCanPerformNodesNotReadyFailureClass",
		// iamescalation (compat_projection.go iam-escalation stanza).
		"IAMEscalationEdgeWriter",
		"IAMEscalationNodesNotReadyFailureClass",
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
				if isRetiredTailBucket1(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredTailBucket1(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredTailBucket1(s.Name.Name, retired) {
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

func isRetiredTailBucket1(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}
