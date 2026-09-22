// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// factsDispatchedKinds scans every non-test .go file under each dir in dirs
// for a switch `case facts.<Kind>:` clause, an `== facts.<Kind>` /
// `facts.<Kind> ==` equality comparison, or a `!= facts.<Kind>` /
// `facts.<Kind> !=` inequality comparison, and returns the set of wire
// fact-kind strings dispatched on, resolved through factsConstValues.
//
// The `!=` form is the "skip-unless-this-kind" idiom — round 2 of the #5474
// review found ~50 occurrences of
// `if envelope.FactKind != facts.<Kind>FactKind { continue }` (or `return
// false`) in go/internal/reducer alone, immediately followed by real payload
// field reads. go/internal/reducer/packages/correlation/source.go
// (`extractPackageSourceHints`)'s `if envelope.FactKind != facts.PackageRegistrySourceHintFactKind {
// continue }` — followed by payload reads of "normalized_url"
// and friends — is the concrete case that was missed when this scan only
// matched token.EQL: package_registry.source_hint was wrongly disclosed as
// unconsumed despite being read here and wired live through
// BuildPackageSourceDecisions
// (packages/correlation/source_handler.go,
// DomainPackageSourceCorrelation).
//
// This is the raw-envelope sibling of the decode-seam and direct-decode-call
// signals: several reducer handlers switch on envelope.FactKind and process
// the envelope's payload fields directly
// (facts.SecretsIAMCoverageWarningFactKind in
// go/internal/reducer/secretsiam/secrets_iam_trust_chain_build.go's
// `case facts.SecretsIAMCoverageWarningFactKind:` and
// facts.ObservabilitySourceInstanceFactKind in
// go/internal/reducer/obscoverage/observability_coverage_metadata.go's
// `envelope.FactKind == facts.ObservabilitySourceInstanceFactKind` are the
// concrete cases that motivated this) without ever calling a typed
// factschema.Decode<Kind> function.
//
// Deliberately narrower than "the identifier is referenced anywhere"
// (factsPackageIdentRefKinds, used only for the query layer): a reducer
// domain's FactKinds() load-list function references every kind it fetches
// whether or not the handler goes on to do anything with a given kind — the
// #5474 P0 concrete counter-example is serviceCatalogCorrelationFactKinds()
// in go/internal/reducer/servicecatalog/service_catalog_correlation.go, which
// loads service_catalog.api_link/dependency/scorecard_definition/
// scorecard_result/warning into the envelope batch, but
// buildServiceCatalogCorrelationIndexWithQuarantine's switch
// (service_catalog_correlation_index.go) only cases on entity/ownership/
// repository_link — those five loaded-but-never-cased kinds are genuinely
// unconsumed (and are disclosed as such). Requiring an actual case clause or
// equality dispatch, not mere presence in a load list, keeps this signal
// from re-introducing that same false-green class.
//
// Callers restrict dirs to go/internal/reducer only, never the projector.
// go/internal/projector/runtime/phase.go has a
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
					if node.Op != token.EQL && node.Op != token.NEQ {
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

// reducerSeamDir is the one entry in realConsumerDecodeSeamDirs that names a
// package tree rather than a single package. Issue #6061 moved the reducer's
// domain families into subpackages, so a dispatch or decode seam now lives in
// a directory like go/internal/reducer/obscoverage while remaining reducer
// code. Scanning only the root stops seeing it, and the kind it consumes then
// reports as having no consumer anywhere in the repository.
const reducerSeamDir = "go/internal/reducer"

// reducerTreeDirs returns the reducer package directory and every package
// directory beneath it.
func reducerTreeDirs(repoRoot string) ([]string, error) {
	root := filepath.Join(repoRoot, reducerSeamDir)
	subs, err := packageDirsUnder(root)
	if err != nil {
		return nil, err
	}
	return append([]string{root}, subs...), nil
}

// queryTreeDirs returns the query package directory and every package
// directory beneath it, so #6060 family leaves (supply/chain/advisory and
// later siblings) keep their decode seams discoverable after moving out of
// root. Same shape as reducerTreeDirs above.
func queryTreeDirs(repoRoot string) ([]string, error) {
	root := filepath.Join(repoRoot, realConsumerRawSQLDir)
	subs, err := packageDirsUnder(root)
	if err != nil {
		return nil, err
	}
	return append([]string{root}, subs...), nil
}

// realConsumerScanDirs resolves the seam dirs to absolute paths, expanding the
// reducer tree and the query tree into their family subpackages and leaving
// every other root as itself.
func realConsumerScanDirs(repoRoot string) ([]string, error) {
	dirs := make([]string, 0, len(realConsumerDecodeSeamDirs))
	for _, dir := range realConsumerDecodeSeamDirs {
		if dir == reducerSeamDir {
			reducerDirs, err := reducerTreeDirs(repoRoot)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, reducerDirs...)
			continue
		}
		if dir == realConsumerRawSQLDir {
			queryDirs, err := queryTreeDirs(repoRoot)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, queryDirs...)
			continue
		}
		dirs = append(dirs, filepath.Join(repoRoot, dir))
	}
	return dirs, nil
}

// packageDirsUnder returns every directory beneath root, skipping testdata and
// dot-directories at any depth. An oversized family may land pre-split into
// nested subdirectories, so this recurses rather than reading one level.
func packageDirsUnder(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: read %s: %w", root, err)
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" {
			continue
		}
		child := filepath.Join(root, entry.Name())
		nested, err := packageDirsUnder(child)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, child)
		dirs = append(dirs, nested...)
	}
	return dirs, nil
}

// factsPackageConstDir is the go/internal/facts package directory, whose files
// declare the package's own FactKind-suffixed wire-string constants (e.g.
// AWSResourceFactKind = "aws_resource").
//
// It names a package TREE, not one directory. Issue #6776 nested the cloud,
// code, documentation and supply-chain fact families into subpackages, so
// AzureResourceChangeFactKind's string literal now lives in
// go/internal/facts/cloud/azure.go and DocumentationLinkFactKind's in
// go/internal/facts/docs/documentation.go. The root still spells both names,
// but only as compat aliases (`AzureResourceChangeFactKind =
// cloud.AzureResourceChangeFactKind`), and an alias is a selector expression
// rather than a string literal, so factKindConstantValues cannot read a wire
// string out of it. Globbing only the root therefore resolves the identifier
// to nothing and every kind it names reports as having no consumer anywhere
// in the repository — the same shape of failure #6061 caused for the reducer,
// recorded at reducerSeamDir.
const factsPackageConstDir = "go/internal/facts"

// factsPackageConstGlobs returns a *.go glob for the facts package directory
// and one for each package directory beneath it.
func factsPackageConstGlobs(repoRoot string) ([]string, error) {
	root := filepath.Join(repoRoot, factsPackageConstDir)
	subs, err := packageDirsUnder(root)
	if err != nil {
		return nil, err
	}
	globs := make([]string, 0, len(subs)+1)
	globs = append(globs, filepath.Join(root, "*.go"))
	for _, dir := range subs {
		globs = append(globs, filepath.Join(dir, "*.go"))
	}
	return globs, nil
}

// factsPackageConstantValues merges factKindConstantValues across the facts
// package tree. A subdirectory that declares no fact-kind constant at all
// contributes nothing rather than failing: factKindConstantValues treats an
// empty glob match as an error, which is the right signal for the root but
// not for a leaf that only holds helpers.
func factsPackageConstantValues(repoRoot string) (map[string]string, error) {
	globs, err := factsPackageConstGlobs(repoRoot)
	if err != nil {
		return nil, err
	}
	merged := map[string]string{}
	for _, glob := range globs {
		values, err := factKindConstantValues(glob)
		if err != nil {
			if matches, globErr := filepath.Glob(glob); globErr == nil && len(matches) == 0 {
				continue
			}
			return nil, err
		}
		for ident, wire := range values {
			merged[ident] = wire
		}
	}
	if len(merged) == 0 {
		return nil, fmt.Errorf("kind_real_consumer: no fact-kind constants found under %s", factsPackageConstDir)
	}
	if err := resolveFactsCompatAliases(filepath.Join(repoRoot, factsPackageConstDir), merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// resolveFactsCompatAliases teaches the derivation to read the facts root's
// compat surface. When a family moved to a subpackage its exported names were
// destuttered (docs/internal/naming.md rule 4), so the root spells
// DocumentationLinkFactKind while the docs package declares LinkFactKind. The
// root keeps the pre-move spelling alive as `DocumentationLinkFactKind =
// docs.LinkFactKind`, and every caller outside the facts tree still writes
// facts.DocumentationLinkFactKind.
//
// Without this pass the root spelling resolves to no wire string, so a query
// layer that references it reads as having no consumer. Each alias is mapped
// to the wire string its target already resolved to, which leaves the literals
// authoritative: an alias can only ever echo a value some leaf declared.
func resolveFactsCompatAliases(rootDir string, values map[string]string) error {
	matches, err := filepath.Glob(filepath.Join(rootDir, "*.go"))
	if err != nil {
		return fmt.Errorf("kind_real_consumer: glob facts compat files %s: %w", rootDir, err)
	}
	sort.Strings(matches)
	fset := token.NewFileSet()
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vspec.Names {
					if i >= len(vspec.Values) {
						continue
					}
					sel, ok := vspec.Values[i].(*ast.SelectorExpr)
					if !ok {
						continue
					}
					if _, already := values[name.Name]; already {
						continue
					}
					if wire, ok := values[sel.Sel.Name]; ok {
						values[name.Name] = wire
					}
				}
			}
		}
	}
	return nil
}
