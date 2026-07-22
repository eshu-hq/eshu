// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// structFieldTypeNames parses path and returns the field type names declared
// on the named struct (e.g. "InfraHandler" -> ["GraphQuery",
// "InfraResourceAggregateStore", "QueryProfile", "*telemetry.Instruments",
// "sync.Once", ...]), stringified via exprString so a caller can
// substring-match a target type name regardless of pointer/selector
// wrapping.
func structFieldTypeNames(t *testing.T, path, structName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			tspec, ok := spec.(*ast.TypeSpec)
			if !ok || tspec.Name.Name != structName {
				continue
			}
			structType, ok := tspec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			names := make([]string, 0, len(structType.Fields.List))
			for _, field := range structType.Fields.List {
				names = append(names, exprString(field.Type))
			}
			return names
		}
	}
	t.Fatalf("struct %q not found in %s", structName, path)
	return nil
}

// exprString renders an AST type expression back to source-shaped text
// (e.g. "*telemetry.Instruments", "KubernetesCorrelationStore") without
// pulling in go/printer for one field-type stringification.
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	default:
		return ""
	}
}

// methodBodySource parses path, finds the method with the given pointer
// receiver type (e.g. "*InfraHandler") and name, and returns the exact
// source text of its body (the literal bytes between the body's opening and
// closing brace) so a caller can substring-search it for evidence of what
// the method actually touches.
func methodBodySource(t *testing.T, path, receiverType, methodName string) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	src, err := os.ReadFile(path) // #nosec G304 -- path is a fixed, committed repo file
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != methodName || fn.Body == nil {
			continue
		}
		if len(fn.Recv.List) != 1 || exprString(fn.Recv.List[0].Type) != receiverType {
			continue
		}
		start := fset.Position(fn.Body.Pos()).Offset
		end := fset.Position(fn.Body.End()).Offset
		return string(src[start:end])
	}
	t.Fatalf("method (%s) %s not found in %s", receiverType, methodName, path)
	return ""
}

// TestRouteServesData_CloudResourcesStructurallyExcludesKubernetesCorrelation
// is the #5474 D1 route-serves-data gate's map-INDEPENDENT BITES proof
// (PR #5583 round-3 P1b, codex). resolveRouteServesData and
// routeServesDataBackingMap are self-certifying for every route: nothing
// cross-checks the map's claims against the real handler/read-model wiring,
// so the documented remediation for a real registry mismatch ("add the
// domain to routeServesDataBackingMap[route].ServedDomains") could paper
// over the exact #5480 misrouting the gate exists to catch, and every
// existing test — including TestRouteServesDataBITES_KubernetesLiveCloudResourcesMismatch —
// would still pass, because they only check the map against itself, never
// against real handler code.
//
// This test proves the ONE historical #5480 pair — kubernetes_live's route
// must not resolve to GET /api/v0/cloud/resources — is structurally
// impossible to reintroduce via a map edit alone, by inspecting the REAL
// registered handlers instead of routeServesDataBackingMap:
//
//  1. InfraHandler (go/internal/query/infra.go), which backs
//     GET /api/v0/cloud/resources via listCloudResources, has no field typed
//     KubernetesCorrelationStore, and listCloudResources's body never
//     mentions "kubernetes" or "Correlations".
//  2. KubernetesHandler (go/internal/query/kubernetes.go), which backs
//     GET /api/v0/kubernetes/correlations via listCorrelations, DOES have a
//     Correlations field typed KubernetesCorrelationStore, and
//     listCorrelations's body calls h.Correlations.ListKubernetesCorrelations.
//
// Even if a future change poisoned
// routeServesDataBackingMap["GET /api/v0/cloud/resources"] to include
// "kubernetes_correlation" (the exact bypass codex flagged), this test would
// still fail, because it never reads that map — its oracle is the real
// handler source.
//
// Scope: this closes the #5480 regression class for ONE pair. The #5584
// generalization (route_serves_data_registry.go and its gate in
// route_serves_data_registry_test.go) now covers every route with a
// handler-derived, source-verified registry; this test remains as the
// dedicated, independent proof of the historical pair.
func TestRouteServesData_CloudResourcesStructurallyExcludesKubernetesCorrelation(t *testing.T) {
	repoRoot := kindConsumerGateRepoRoot(t)
	infraPath := filepath.Join(repoRoot, "go/internal/query/infra.go")
	cloudResourcesPath := filepath.Join(repoRoot, "go/internal/query/cloud_resources.go")
	kubernetesPath := filepath.Join(repoRoot, "go/internal/query/kubernetes.go")

	t.Run("InfraHandler_struct_has_no_KubernetesCorrelationStore_field", func(t *testing.T) {
		for _, fieldType := range structFieldTypeNames(t, infraPath, "InfraHandler") {
			if strings.Contains(fieldType, "KubernetesCorrelationStore") {
				t.Fatalf("BITES FAILED: InfraHandler has a field typed %q — GET /api/v0/cloud/resources could serve kubernetes_correlation data", fieldType)
			}
		}
	})

	t.Run("listCloudResources_body_never_mentions_kubernetes", func(t *testing.T) {
		body := methodBodySource(t, cloudResourcesPath, "*InfraHandler", "listCloudResources")
		if strings.Contains(strings.ToLower(body), "kubernetes") || strings.Contains(body, "Correlations") {
			t.Fatalf("BITES FAILED: InfraHandler.listCloudResources references kubernetes/Correlations — it should only read CloudResource graph nodes via h.Neo4j")
		}
	})

	t.Run("KubernetesHandler_struct_has_KubernetesCorrelationStore_field", func(t *testing.T) {
		found := false
		for _, fieldType := range structFieldTypeNames(t, kubernetesPath, "KubernetesHandler") {
			if strings.Contains(fieldType, "KubernetesCorrelationStore") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("KubernetesHandler has no field typed KubernetesCorrelationStore — test premise broken or the handler was refactored; update this test's oracle")
		}
	})

	t.Run("listCorrelations_body_reads_Correlations_store", func(t *testing.T) {
		body := methodBodySource(t, kubernetesPath, "*KubernetesHandler", "listCorrelations")
		if !strings.Contains(body, "h.Correlations") {
			t.Fatalf("KubernetesHandler.listCorrelations does not reference h.Correlations — test premise broken or the handler was refactored; update this test's oracle")
		}
	})
}
