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

// TestDecodeBucketFamilyRetired pins the #6626 decode bucket retirement: the
// schemadecode, factdecode, factwrite, factload, payloadcore, codeowners,
// cross-scope, and graph-projection phase-repair aliases and thin forwarders
// (deleted from compat_decode.go and compat_correlation.go) whose callers now
// name the owning subpackage directly must stay absent from the whole reducer
// package directory — a re-declared alias in any file would still compile.
// The canonical subpackage spellings are the only ones.
func TestDecodeBucketFamilyRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		// schemadecode decode seam (compat_decode.go decode_seam stanzas).
		"decodeAWSRelationship",
		"decodeAWSResource",
		"decodeAzureCloudRelationship",
		"decodeAzureCloudResource",
		"decodeCodegraphFile",
		"decodeDocumentationDocument",
		"decodeDocumentationEntityMention",
		"decodeGCPCloudRelationship",
		"decodeGCPCloudResource",
		"decodeKubernetesLiveNamespace",
		"decodeKubernetesLivePodTemplate",
		"decodeReducerPackageOwnershipCorrelation",
		"decodeReducerPackagePublicationCorrelation",
		"decodeSubmodulePin",
		// factdecode quarantine (compat_decode.go quarantine stanza).
		"QuarantinedFactRecord",
		"QuarantinedFactWriter",
		"quarantinedFact",
		"factDecodeError",
		"WithQuarantineWriter",
		"newFactDecodeError",
		"partitionDecodeFailures",
		"quarantinedAttributeShapeFact",
		"attributeShapeAsFactDecodeError",
		"inputInvalidSubSignals",
		"recordQuarantinedFacts",
		// factwrite batch writers (compat_decode.go reducer_fact_write stanza).
		"workloadIdentityExecer",
		"reducerFactRow",
		"reducerFactVersionedRow",
		"reducerFactBatchInsertQuery",
		"reducerFactBatchSize",
		"reducerFactBatchInsertVersionedQuery",
		"canonicalReducerFactInsertQuery",
		"reducerBatchInsertFacts",
		"reducerBatchInsertVersionedFacts",
		"dedupeReducerFactRowsByFactID",
		"reducerWriterNow",
		"reducerFactCollectorKind",
		// factload scoped loader (compat_decode.go scoped_fact_loader stanza).
		"factKindContentEntity",
		"factKindFile",
		"factKindParsedFile",
		"factKindRepository",
		"factKindCodeownersOwnership",
		"factKindSubmodulePin",
		"loadFactsForKinds",
		// payloadcore deltas (compat_decode.go shared_payload_delta stanza).
		"payloadMap",
		"semanticPayloadString",
		"semanticPayloadStringSlice",
		"semanticQualifyDeltaPath",
		"semanticDeltaPayloadBool",
		// codeowners (compat_decode.go codeowners-ownership stanza).
		"CodeownersOwnershipEdgeMaterializationHandler",
		"codeownersMaterializationFactKinds",
		"loadCodeownersOwnershipMaterializationFacts",
		// cross-scope (compat_decode.go cross_scope_readiness stanza).
		"CrossScopeDependency",
		// phase repair (compat_correlation.go phase-repair stanza).
		"GraphProjectionPhaseRepair",
		"GraphProjectionPhaseRepairQueue",
		"GraphProjectionPhaseRepairerConfig",
		"GraphProjectionPhaseRepairer",
		"GraphProjectionPhaseRepairsFromStates",
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

func isRetiredDecodeBucket(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}
