// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// factDeleteStatement matches a statement that removes fact_records rows,
// directly or through the scope_generations / ingestion_scopes cascade.
var factDeleteStatement = regexp.MustCompile(`(?is)DELETE\s+FROM\s+(ONLY\s+)?(fact_records|scope_generations|ingestion_scopes)\b`)

// factKeySidecarKinds are the fact kinds that own rows in the three per-fact
// key sidecars (#7420): relationship_reference_candidate_keys
// (relationshipReferenceFactKind), package_manifest_consumption_keys
// (isPackageManifestConsumptionFact) and package_registry_identity_keys
// (packageRegistryIdentityKeyRows).
var factKeySidecarKinds = []string{"content", "file", "gcp_cloud_relationship", "content_entity", "package_registry.package"}

// factDeleteSite classifies one fact delete statement. A site is safe when
// it deletes only kinds that never own key sidecar rows (kindLiteral, or
// kindParam bound by every caller to kind) or when every function that runs
// it first calls pairedWith, the explicit key sidecar delete.
type factDeleteSite struct {
	kindLiteral string
	kindParam   string
	pairedWith  string
}

// factDeleteSites is every fact delete statement in non-test Go under go/,
// keyed by "<package dir>:<const or func name>". The guard reads single Go
// string literals only: a statement assembled from several literals, or one
// outside go/, is out of its reach. The one known outside delete is
// scripts/lib/golden-corpus-vulnerability-suppression.sh (fact_kind
// vulnerability.suppression, which owns no key sidecar rows).
var factDeleteSites = map[string]factDeleteSite{
	// Retention site A: the cascade from scope_generations.
	"internal/storage/postgres:deleteScopeGenerationsForRetentionQuery": {pairedWith: "DeleteGenerations"},
	// Retention site B: the #7981 over-limit chunk page.
	"internal/storage/postgres:generationRetentionChunkDeleteFactsQuery": {pairedWith: "DeletePage"},
	// Retirement purge site (#7766): the scope-row delete cascades
	// generations and facts; purgeOne prunes the key sidecars first with
	// factkeys.DeleteGenerations and merges the counts into the tombstone
	// ledger.
	"internal/storage/postgres:purgeDeleteScopesQuery": {pairedWith: "DeleteGenerations"},
	// Reducer retires of reducer-owned kinds.
	"internal/reducer/containerimage:containerImageIdentityPublishAndLegacyCleanupQuery": {kindLiteral: "reducer_container_image_identity"},
	"internal/reducer/containerimage:containerImageIdentityCompletedCutoverWriteQuery":   {kindLiteral: "reducer_container_image_identity"},
	"internal/reducer/containerimage:containerImageIdentitySupportPublishQuery":          {kindLiteral: "reducer_container_image_identity"},
	"internal/reducer/eshusearch:eshuSearchDocumentRetireQuery":                          {kindParam: "reducer_eshu_search_document"},
	"internal/reducer/awscloud:awsCloudRuntimeDriftRetireQuery":                          {kindParam: "reducer_aws_cloud_runtime_drift_finding"},
	"internal/reducer/tfconfigstate:terraformConfigStateDriftRetireQuery":                {kindParam: "reducer_terraform_config_state_drift_finding"},
	"internal/reducer/codedivergence:driftedRetireQuery":                                 {kindParam: "reducer_code_drifted_finding"},
}

// guardSource is one Go source file the guard reads: its package directory
// (relative to go/) and its parsed syntax.
type guardSource struct {
	dir  string
	file *ast.File
}

// TestFactRecordDeleteSitesAreKeySidecarSafe fails when a fact delete in
// non-test Go could leave key sidecar rows behind once the per-fact foreign
// keys are dropped (#7420): an unclassified delete, a paired delete whose
// function does not first delete the key sidecars, or a kind-scoped delete
// whose kind owns key sidecar rows.
func TestFactRecordDeleteSitesAreKeySidecarSafe(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	scanned := parseGuardSources(t, fset, "../../..")
	if violations := factDeleteGuardViolations(fset, scanned, scanned, factDeleteSites); len(violations) > 0 {
		t.Fatalf("fact delete sites not key-sidecar safe:\n%s", strings.Join(violations, "\n"))
	}
}

// TestFactRecordDeleteGuardCatchesPlantedViolations is the guard's seeded
// RED: each planted source must be reported, and the clean twin must not.
func TestFactRecordDeleteGuardCatchesPlantedViolations(t *testing.T) {
	t.Parallel()

	const clean = `package p
const kind = "reducer_x"
const pairedDelete = "DELETE FROM fact_records WHERE fact_id = ANY($1)"
const retire = "DELETE FROM fact_records WHERE fact_kind = $1 AND scope_id = $2"
func run() { deleteKeys(); exec(pairedDelete); exec(retire, kind) }
`
	sites := map[string]factDeleteSite{
		"p:pairedDelete": {pairedWith: "deleteKeys"},
		"p:retire":       {kindParam: "reducer_x"},
	}
	for _, tc := range []struct {
		name, source string
		wantClean    bool
	}{
		{"clean", clean, true},
		{"unclassified", strings.Replace(clean, "func run()", "const planted = \"DELETE FROM fact_records WHERE fact_id = $1\"\nfunc run()", 1), false},
		{"unpaired", strings.Replace(clean, "deleteKeys(); ", "", 1), false},
		{"paired-after", strings.Replace(clean, "deleteKeys(); exec(pairedDelete);", "exec(pairedDelete); deleteKeys();", 1), false},
		{"key-kind-bound", strings.Replace(clean, `"reducer_x"`, `"content"`, 1), false},
		{"cascade-unclassified", strings.Replace(clean, "func run()", "const reap = \"DELETE FROM scope_generations WHERE generation_id = $1\"\nfunc run()", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "p.go", tc.source, 0)
			if err != nil {
				t.Fatalf("parse planted source: %v", err)
			}
			siteSet := sites
			if tc.name == "key-kind-bound" {
				siteSet = map[string]factDeleteSite{"p:pairedDelete": {pairedWith: "deleteKeys"}, "p:retire": {kindParam: "content"}}
			}
			sources := []guardSource{{dir: "p", file: file}}
			violations := factDeleteGuardViolations(fset, sources, sources, siteSet)
			if tc.wantClean && len(violations) > 0 {
				t.Fatalf("clean source reported %v", violations)
			}
			if !tc.wantClean && len(violations) == 0 {
				t.Fatalf("planted %s violation not reported", tc.name)
			}
		})
	}
}

func parseGuardSources(t *testing.T, fset *token.FileSet, root string) []guardSource {
	t.Helper()
	var sources []guardSource
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Parse only files that can hold a delete or a kind constant: the
		// whole go/ tree is too large to parse on every run.
		if !bytes.Contains(bytes.ToUpper(source), []byte("DELETE")) && !bytes.Contains(source, []byte("FactKind")) {
			return nil
		}
		file, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			return err
		}
		abs, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return err
		}
		_, rel, ok := strings.Cut(filepath.ToSlash(abs), "/go/")
		if !ok {
			rel = filepath.ToSlash(filepath.Dir(path))
		}
		sources = append(sources, guardSource{dir: rel, file: file})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return sources
}

// factDeleteGuardViolations checks every fact delete string literal in
// scanned against sites; resolve supplies the constants a kind argument may
// name.
func factDeleteGuardViolations(fset *token.FileSet, scanned, resolve []guardSource, sites map[string]factDeleteSite) []string {
	constants := guardStringConstants(resolve)
	var violations []string
	found := map[string]bool{}
	for _, source := range scanned {
		for _, site := range guardFactDeleteLiterals(source.file) {
			key := source.dir + ":" + site.name
			found[key] = true
			class, ok := sites[key]
			switch {
			case !ok:
				violations = append(violations, fmt.Sprintf("%s: unclassified fact delete %s", fset.Position(site.pos), key))
			case class.kindLiteral != "":
				if !strings.Contains(site.text, "fact_kind = '"+class.kindLiteral+"'") || slices.Contains(factKeySidecarKinds, class.kindLiteral) {
					violations = append(violations, fmt.Sprintf("%s: %s is not scoped to key-free kind %q", fset.Position(site.pos), key, class.kindLiteral))
				}
			case class.kindParam != "":
				violations = append(violations, guardKindParamViolations(fset, scanned, source.dir, site.name, class.kindParam, constants)...)
			case class.pairedWith != "":
				violations = append(violations, guardPairViolations(fset, scanned, source.dir, site.name, class.pairedWith)...)
			}
		}
	}
	for key := range sites {
		if !found[key] {
			violations = append(violations, "stale classification with no fact delete: "+key)
		}
	}
	slices.Sort(violations)
	return violations
}

type guardLiteral struct {
	name, text string
	pos        token.Pos
}

// guardFactDeleteLiterals returns each fact delete string literal with the
// const/var it initializes, or else the function it appears in.
func guardFactDeleteLiterals(file *ast.File) []guardLiteral {
	var literals []guardLiteral
	visit := func(name string, node ast.Node) {
		ast.Inspect(node, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err == nil && factDeleteStatement.MatchString(text) {
				literals = append(literals, guardLiteral{name: name, text: text, pos: lit.Pos()})
			}
			return true
		})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			visit(d.Name.Name, d)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if value, ok := spec.(*ast.ValueSpec); ok && len(value.Names) > 0 {
					visit(value.Names[0].Name, value)
				}
			}
		}
	}
	return literals
}

// guardStringConstants maps each const/var name to its string value,
// following one-level aliases such as `const k = facts.Kind`.
func guardStringConstants(sources []guardSource) map[string]string {
	values := map[string]string{}
	aliases := map[string]string{}
	for _, source := range sources {
		for _, decl := range source.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range value.Names {
					if i >= len(value.Values) {
						continue
					}
					switch v := value.Values[i].(type) {
					case *ast.BasicLit:
						if text, err := strconv.Unquote(v.Value); err == nil && v.Kind == token.STRING {
							values[name.Name] = text
						}
					case *ast.SelectorExpr:
						aliases[name.Name] = v.Sel.Name
					case *ast.Ident:
						aliases[name.Name] = v.Name
					}
				}
			}
		}
	}
	for name, target := range aliases {
		if text, ok := values[target]; ok {
			if _, own := values[name]; !own {
				values[name] = text
			}
		}
	}
	return values
}

// guardKindParamViolations requires every call in dir that passes the site
// const to bind the very next argument to kind, a key-free kind.
func guardKindParamViolations(fset *token.FileSet, sources []guardSource, dir, site, kind string, constants map[string]string) []string {
	if slices.Contains(factKeySidecarKinds, kind) {
		return []string{fmt.Sprintf("%s:%s binds key sidecar kind %q", dir, site, kind)}
	}
	var violations []string
	calls := 0
	for _, source := range sources {
		if source.dir != dir {
			continue
		}
		ast.Inspect(source.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for i, arg := range call.Args {
				if ident, ok := arg.(*ast.Ident); !ok || ident.Name != site {
					continue
				}
				calls++
				if i+1 >= len(call.Args) || guardExprString(call.Args[i+1], constants) != kind {
					violations = append(violations, fmt.Sprintf("%s: %s:%s kind argument is not %q", fset.Position(call.Pos()), dir, site, kind))
				}
			}
			return true
		})
	}
	if calls == 0 {
		violations = append(violations, fmt.Sprintf("%s:%s has no call binding its kind", dir, site))
	}
	return violations
}

func guardExprString(expr ast.Expr, constants map[string]string) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		text, _ := strconv.Unquote(e.Value)
		return text
	case *ast.Ident:
		return constants[e.Name]
	case *ast.SelectorExpr:
		return constants[e.Sel.Name]
	}
	return ""
}

// guardPairViolations requires every function in dir that references the
// site const to call pair before its first reference.
func guardPairViolations(fset *token.FileSet, sources []guardSource, dir, site, pair string) []string {
	var violations []string
	users := 0
	for _, source := range sources {
		if source.dir != dir {
			continue
		}
		for _, decl := range source.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			firstUse, firstPair := token.NoPos, token.NoPos
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.Ident:
					if node.Name == site && firstUse == token.NoPos {
						firstUse = node.Pos()
					}
				case *ast.CallExpr:
					name := ""
					switch fun := node.Fun.(type) {
					case *ast.Ident:
						name = fun.Name
					case *ast.SelectorExpr:
						name = fun.Sel.Name
					}
					if name == pair && firstPair == token.NoPos {
						firstPair = node.Pos()
					}
				}
				return true
			})
			if firstUse == token.NoPos {
				continue
			}
			users++
			if firstPair == token.NoPos || firstPair > firstUse {
				violations = append(violations, fmt.Sprintf("%s: %s runs %s without first calling %s", fset.Position(firstUse), fn.Name.Name, site, pair))
			}
		}
	}
	if users == 0 {
		violations = append(violations, fmt.Sprintf("%s:%s is never run", dir, site))
	}
	return violations
}
