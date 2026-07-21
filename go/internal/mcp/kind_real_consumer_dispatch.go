// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// factsDispatchedKinds scans every non-test .go file under each dir in dirs
// for a switch `case facts.<Kind>:` clause or an `== facts.<Kind>` /
// `facts.<Kind> ==` equality comparison, and returns the set of wire
// fact-kind strings dispatched on, resolved through factsConstValues.
//
// This is the raw-envelope sibling of the decode-seam and direct-decode-call
// signals: several reducer handlers switch on envelope.FactKind and process
// the envelope's payload fields directly
// (facts.SecretsIAMCoverageWarningFactKind in
// go/internal/reducer/secrets_iam_trust_chain_build.go's
// `case facts.SecretsIAMCoverageWarningFactKind:` and
// facts.ObservabilitySourceInstanceFactKind in
// go/internal/reducer/observability_coverage_metadata.go's
// `envelope.FactKind == facts.ObservabilitySourceInstanceFactKind` are the
// concrete cases that motivated this) without ever calling a typed
// factschema.Decode<Kind> function.
//
// Deliberately narrower than "the identifier is referenced anywhere"
// (factsPackageIdentRefKinds, used only for the query layer): a reducer
// domain's FactKinds() load-list function references every kind it fetches
// whether or not the handler goes on to do anything with a given kind — the
// #5474 P0 concrete counter-example is serviceCatalogCorrelationFactKinds()
// in go/internal/reducer/service_catalog_correlation.go, which loads
// service_catalog.api_link/dependency/scorecard_definition/scorecard_result/
// warning into the envelope batch, but
// buildServiceCatalogCorrelationIndexWithQuarantine's switch
// (service_catalog_correlation_index.go) only cases on entity/ownership/
// repository_link — those five loaded-but-never-cased kinds are genuinely
// unconsumed (and are disclosed as such). Requiring an actual case clause or
// equality dispatch, not mere presence in a load list, keeps this signal
// from re-introducing that same false-green class.
//
// Callers restrict dirs to go/internal/reducer only, never the projector.
// go/internal/projector/runtime_phase.go has a
// `case facts.TerraformStateSnapshotFactKind, facts.TerraformStateWarningFactKind:`
// clause that dispatches purely on the fact's KIND IDENTITY to publish a
// graph-projection-readiness phase marker (canonicalGraphPhaseStates) — it
// never reads the fact's Payload fields. Scanning the projector would make
// terraform_state_warning look consumed through that readiness bookkeeping,
// reintroducing the exact false-green class this gate exists to close (the
// disclosed terraform_state trio must stay classified unconsumed). The
// reducer package's case/equality dispatches, by contrast, are consistently
// payload-processing sites (addAWSEnvelope, addKubernetesEnvelope, and
// similar helpers actually decode or read the matched envelope), so scoping
// this signal to the reducer keeps it precise.
func factsDispatchedKinds(dirs []string, factsConstValues map[string]string) (map[string]bool, error) {
	kinds := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: glob %s: %w", dir, err)
		}
		for _, path := range matches {
			if isGoTestFile(path) {
				continue
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CaseClause:
					for _, expr := range node.List {
						if wire, ok := factsSelectorWireKind(expr, factsConstValues); ok {
							kinds[wire] = true
						}
					}
				case *ast.BinaryExpr:
					if node.Op != token.EQL {
						return true
					}
					if wire, ok := factsSelectorWireKind(node.X, factsConstValues); ok {
						kinds[wire] = true
					}
					if wire, ok := factsSelectorWireKind(node.Y, factsConstValues); ok {
						kinds[wire] = true
					}
				}
				return true
			})
		}
	}
	return kinds, nil
}

// factsSelectorWireKind reports whether expr is a `facts.<Ident>` selector
// naming a known FactKind constant, resolving it to its wire string through
// factsConstValues.
func factsSelectorWireKind(expr ast.Expr, factsConstValues map[string]string) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok || pkgIdent.Name != "facts" {
		return "", false
	}
	wire, ok := factsConstValues[sel.Sel.Name]
	return wire, ok
}
