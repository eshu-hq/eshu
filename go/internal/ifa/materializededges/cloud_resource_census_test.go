// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Sole-creator census for the cloud_resource predicate (#8358).
//
// The predicate (type prefix + evidence_source + CloudResource endpoints)
// is exact only while the three writers are the SOLE creators of edges
// matching it, and while the production retracts — bare `[rel]` +
// evidence_source + scope — reap exactly that set. Both directions rest on
// who can produce a prefixed type and who can stamp the evidence_source:
//
//   - A prefixed TYPE reaches the graph either from a writer's
//     canonical*RelationshipCypherType (which prepends its prefix const) or
//     from a template naming a prefixed type literally. The census pins the
//     prefix string literals to the three writer files, so the only
//     prefixed types are these writers' %s-interpolated ones and other
//     families' literal types (observability's AWS_COVERS_*), which the
//     evidence_source conjunct separates.
//   - An evidence_source STAMP reaches an edge only through a writer's
//     evidenceSource parameter. The census pins each stamp literal and each
//     exported accessor call to its handler plus the wiring/registry files
//     that must name the same value, so no other handler can stamp its
//     edges into (or retract edges out of) this family.
//
// A second creator turning this build red is the route-2 proof obligation
// from the #8358 issue: the predicate removes/asserts exactly the family's
// edges. String literals are read from the AST (code only), so comments
// naming a stamp do not false-positive.

// cloudResourceCensusStamps pairs each registered stamp (via its handler
// accessor, never a copied literal in this test's expectations other than
// the handler file that must define it) with the one file allowed to define
// the literal.
func cloudResourceCensusStamps() map[string]string {
	return map[string]string{
		reducer.AWSRelationshipEvidenceSource():   "go/internal/reducer/aws_relationship_materialization.go",
		reducer.GCPRelationshipEvidenceSource():   "go/internal/reducer/gcp_relationship_materialization.go",
		reducer.AzureRelationshipEvidenceSource(): "go/internal/reducer/azure_relationship_materialization.go",
	}
}

// cloudResourceCensusGoFiles walks go/ (bounded to internal and cmd, where
// every graph writer and handler lives) and returns repo-relative non-test
// .go paths.
func cloudResourceCensusGoFiles(t *testing.T) []string {
	t.Helper()
	root := repoRootDir(t)
	var files []string
	for _, subtree := range []string{"go/internal", "go/cmd"} {
		base := filepath.Join(root, subtree)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", subtree, err)
		}
	}
	if len(files) == 0 {
		t.Fatal("census found no Go files; it would pass vacuously")
	}
	sort.Strings(files)
	return files
}

// cloudResourceCensusParse parses one file's code (comments excluded by
// walking declarations only through BasicLit/CallExpr nodes below).
func cloudResourceCensusParse(t *testing.T, root, rel string) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, rel), nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return parsed
}

// TestCloudResourcePredicateSoleCreatorCensus is the exactness census: a
// second creator of predicate-matching edges must turn the build red here,
// in a cheap unit run, not on a live gate.
func TestCloudResourcePredicateSoleCreatorCensus(t *testing.T) {
	t.Parallel()

	root := repoRootDir(t)
	stamps := cloudResourceCensusStamps()
	// Extra READ-SIDE files allowed to name a stamp: the graphdump GCP scope
	// integrity check asserts (never writes) that GCP_ edges carry the
	// handler's stamp. A new file naming a stamp still reds here.
	stampLiteralReaders := map[string]map[string]struct{}{
		reducer.GCPRelationshipEvidenceSource(): {
			"go/internal/ifa/graphdump/gcp_project_scope_integrity.go": {},
		},
	}
	prefixLiterals := map[string]string{
		"AWS_":   "go/internal/storage/cypher/cloud_resource_edge_writer.go",
		"GCP_":   "go/internal/storage/cypher/gcp_cloud_resource_edge_writer.go",
		"AZURE_": "go/internal/storage/cypher/azure_cloud_resource_edge_writer.go",
	}
	// Extra READ-SIDE files allowed to name a prefix literal: the graphdump
	// integrity check above and the cross-repo evidence classifier, which
	// maps an existing edge's type prefix to a provider label and creates
	// nothing. Only the writer files may CONSTRUCT a type from the prefix;
	// neither reader concatenates it onto a verb.
	prefixLiteralReaders := map[string]map[string]struct{}{
		"GCP_": {
			"go/internal/ifa/graphdump/gcp_project_scope_integrity.go":  {},
			"go/internal/reducer/crossrepo/cross_repo_evidence_type.go": {},
		},
	}
	// Files allowed to CALL an evidence accessor: the defining handler, the
	// reducer binary's backfiller wiring (which re-derives the same stamp
	// for ledger replay, never a graph write of its own), the predicate
	// registry, and this family's guard.
	accessorCallers := map[string]map[string]struct{}{
		"AWSRelationshipEvidenceSource": {
			"go/internal/reducer/aws_relationship_materialization.go":            {},
			"go/cmd/reducer/canonical_graph_writers.go":                          {},
			"go/internal/storage/cypher/edge/materialized/predicate_families.go": {},
			"go/internal/ifa/materializededges/cloud_resource.go":                {},
		},
		"GCPRelationshipEvidenceSource": {
			"go/internal/reducer/gcp_relationship_materialization.go":            {},
			"go/cmd/reducer/canonical_graph_writers.go":                          {},
			"go/internal/storage/cypher/edge/materialized/predicate_families.go": {},
			"go/internal/ifa/materializededges/cloud_resource.go":                {},
		},
		"AzureRelationshipEvidenceSource": {
			"go/internal/reducer/azure_relationship_materialization.go":          {},
			"go/cmd/reducer/canonical_graph_writers.go":                          {},
			"go/internal/storage/cypher/edge/materialized/predicate_families.go": {},
			"go/internal/ifa/materializededges/cloud_resource.go":                {},
		},
	}

	sawStampLiteral := map[string]bool{}
	sawPrefixLiteral := map[string]bool{}
	for _, rel := range cloudResourceCensusGoFiles(t) {
		parsed := cloudResourceCensusParse(t, root, rel)
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(n.Value)
				if err != nil {
					return true
				}
				if wantFile, isStamp := stamps[value]; isStamp {
					if rel == wantFile {
						sawStampLiteral[value] = true
					} else if _, isReader := stampLiteralReaders[value][rel]; !isReader {
						t.Errorf("evidence_source literal %q defined in %s, want only its handler %s (plus allowlisted readers); a second definition can stamp foreign edges into the predicate", value, rel, wantFile)
					}
				}
				if wantFile, isPrefix := prefixLiterals[value]; isPrefix {
					if rel == wantFile {
						sawPrefixLiteral[value] = true
					} else if _, isReader := prefixLiteralReaders[value][rel]; !isReader {
						t.Errorf("relationship-type prefix literal %q in %s, want only its writer %s (plus allowlisted readers); a second prefixing site creates types the retract/predicate pairing does not own", value, rel, wantFile)
					}
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if allowed, isAccessor := accessorCallers[sel.Sel.Name]; isAccessor {
					if _, ok := allowed[rel]; !ok {
						t.Errorf("%s called from %s, which is not an allowlisted caller; a new caller can pass the stamp to a graph write the predicate does not own", sel.Sel.Name, rel)
					}
				}
			}
			return true
		})
	}
	for stamp := range stamps {
		if !sawStampLiteral[stamp] {
			t.Errorf("evidence_source literal %q found in no file; the census is not seeing the handler definition it pins", stamp)
		}
	}
	for prefix := range prefixLiterals {
		if !sawPrefixLiteral[prefix] {
			t.Errorf("prefix literal %q found in no file; the census is not seeing the writer const it pins", prefix)
		}
	}
}

// TestCloudResourceWritePortHasExactlyThreeWriters pins the writer side of
// the pairing: WriteCloudResourceEdges is implemented by exactly the AWS,
// GCP, and Azure writers the predicate registers. A fourth implementation
// would write edges through the same port — under one of the three handlers'
// stamps — with a type vocabulary this registration never reviewed.
func TestCloudResourceWritePortHasExactlyThreeWriters(t *testing.T) {
	t.Parallel()

	root := repoRootDir(t)
	want := map[string]struct{}{
		"go/internal/storage/cypher/cloud_resource_edge_writer.go":       {},
		"go/internal/storage/cypher/gcp_cloud_resource_edge_writer.go":   {},
		"go/internal/storage/cypher/azure_cloud_resource_edge_writer.go": {},
	}
	got := map[string]struct{}{}
	for _, rel := range cloudResourceCensusGoFiles(t) {
		if !strings.HasPrefix(rel, "go/internal/storage/cypher/") {
			continue
		}
		parsed := cloudResourceCensusParse(t, root, rel)
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv != nil && fn.Name.Name == "WriteCloudResourceEdges" {
				got[rel] = struct{}{}
			}
		}
	}
	for rel := range want {
		if _, ok := got[rel]; !ok {
			t.Errorf("writer %s no longer implements WriteCloudResourceEdges; the predicate registers a pairing nobody writes", rel)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Errorf("WriteCloudResourceEdges implemented in %s, which the cloud_resource predicate does not register; register its pairing before it writes under a handler stamp", rel)
		}
	}
}
