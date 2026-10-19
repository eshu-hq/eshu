// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestGoPackageCyclesFixtureTruthAgreement is the #7347 three-way proof:
// fixture intent (the tracked Go module under
// tests/fixtures/ecosystems/go_import_cycles) agrees with the projector's
// graph truth (production parser plus production BuildMaterialization) and
// with the API/query truth (production BuildFileImportCycleRows plus the
// response shaper), for positive 2-, 3-, and 5-package cycles and the
// diamond negative. The type-only negative has no fixture: the Go parser
// never emits a type-only import (#7344), so it is covered by
// TestBuildFileImportCycleRowsGoTypeOnlyEdgeClosesNoCycle with a synthetic
// row instead.
func TestGoPackageCyclesFixtureTruthAgreement(t *testing.T) {
	t.Parallel()

	const repoID = "repo-go-cycles"
	fixtureRoot := goPackageCyclesFixtureRoot(t)
	modulePath := goPackageCyclesModulePath(t, fixtureRoot)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var fileFacts []facts.Envelope
	goFileCount := 0
	err = filepath.WalkDir(fixtureRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(fixtureRoot, path)
		if err != nil {
			t.Fatalf("filepath.Rel() error = %v, want nil", err)
		}
		relSlash := filepath.ToSlash(rel)
		// The test plays the collector's prescan: each directory's import
		// path is the module path joined with its fixture-relative dir.
		importPath := modulePath + "/" + filepath.ToSlash(filepath.Dir(rel))
		payload, err := engine.ParsePath(fixtureRoot, path, false, parser.Options{
			GoPackageImportPath: importPath,
		})
		if err != nil {
			t.Fatalf("ParsePath(%s) error = %v, want nil", relSlash, err)
		}
		goFileCount++
		fileFacts = append(fileFacts, facts.Envelope{
			FactID:   "file-" + strings.ReplaceAll(relSlash, "/", "-"),
			ScopeID:  "scope-cycles",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          repoID,
				"relative_path":    relSlash,
				"language":         "go",
				"parsed_file_data": payload,
			},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v, want nil", err)
	}
	if goFileCount != 15 {
		t.Fatalf("parsed %d Go files, want all 15 fixture files", goFileCount)
	}

	envelopes := append([]facts.Envelope{{
		FactID:   "repo-1",
		ScopeID:  "scope-cycles",
		FactKind: "repository",
		Payload:  map[string]any{"repo_id": repoID, "path": fixtureRoot},
	}}, fileFacts...)

	mat, quarantined, _ := canonical.BuildMaterialization(
		scope.IngestionScope{
			ScopeID:      "scope-cycles",
			SourceSystem: "test",
			ScopeKind:    scope.KindRepository,
			Metadata:     map[string]string{"repo_id": repoID, "repo_path": fixtureRoot},
		},
		scope.ScopeGeneration{GenerationID: "gen-1", ScopeID: "scope-cycles", Status: scope.GenerationStatusActive},
		envelopes,
	)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0: %#v", len(quarantined), quarantined)
	}

	// Projector graph truth: every Go edge is placed in its package, so the
	// query has no unplaced edge to drop.
	for _, row := range mat.Imports {
		if row.ModuleLanguage != "go" {
			t.Fatalf("import edge %+v is not Go, want a Go-only fixture", row)
		}
		if row.SourcePackage == "" {
			t.Fatalf("import edge %+v has no source package, want full mapping density", row)
		}
	}

	// Edge rows in the exact shape the graph fetch returns them, with the
	// flags as the explicit booleans the writer persists.
	edgeRows := make([]map[string]any, 0, len(mat.Imports))
	for _, row := range mat.Imports {
		rel, err := filepath.Rel(fixtureRoot, row.FilePath)
		if err != nil {
			t.Fatalf("filepath.Rel() error = %v, want nil", err)
		}
		relSlash := filepath.ToSlash(rel)
		edgeRows = append(edgeRows, map[string]any{
			"repo_id":        repoID,
			"repo_name":      "go-cycles",
			"source_path":    row.FilePath,
			"source_file":    relSlash,
			"source_name":    filepath.Base(relSlash),
			"language":       row.ModuleLanguage,
			"target_module":  row.ModuleName,
			"line_number":    row.LineNumber,
			"type_only":      row.TypeOnly,
			"deferred":       row.Deferred,
			"inferred":       row.Inferred,
			"source_package": row.SourcePackage,
		})
	}

	req := codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    repoID,
		Language:  "go",
		Limit:     200,
	}
	rows, enumeration, err := codemodel.BuildFileImportCycleRows(req, edgeRows)
	if err != nil {
		t.Fatalf("BuildFileImportCycleRows() error = %v, want nil", err)
	}
	if enumeration.Truncated {
		t.Fatalf("enumeration = %+v, want an untruncated run", enumeration)
	}
	if enumeration.GoPackageUnknown != 0 {
		t.Fatalf("enumeration.GoPackageUnknown = %d, want 0 placed edges", enumeration.GoPackageUnknown)
	}
	resp := codemodel.ImportDependencyResponseWithCycleEnumeration(req, rows, enumeration)

	cycles, ok := resp["cycles"].([]map[string]any)
	if !ok {
		t.Fatalf("cycles = %#v, want cycle rows", resp["cycles"])
	}
	lengths := make([]int, 0, len(cycles))
	paths := make([][]string, 0, len(cycles))
	for _, cycle := range cycles {
		lengths = append(lengths, codequery.IntVal(cycle, "cycle_length"))
		paths = append(paths, cycleStrings(t, cycle["cycle_path"]))
	}
	sort.Ints(lengths)
	if strings.Join(intsToStrings(lengths), ",") != "2,3,5" {
		t.Fatalf("cycle lengths = %v with paths %q, want exactly [2 3 5]", lengths, paths)
	}
	for _, path := range paths {
		for _, member := range path {
			if strings.Contains(member, "diamond") {
				t.Fatalf("cycle path %q contains a diamond package, want no diamond cycle", path)
			}
		}
		// Rotation normalization: the lexicographically smallest package leads.
		withoutClose := append([]string{}, path[:len(path)-1]...)
		smallest := withoutClose[0]
		for _, member := range withoutClose[1:] {
			if member < smallest {
				smallest = member
			}
		}
		if path[0] != smallest {
			t.Fatalf("cycle path %q does not start at its smallest package %q", path, smallest)
		}
	}

	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage = %#v, want a coverage map", resp["coverage"])
	}
	if got := coverage["query_shape"]; got != "go_package_import_cycle" {
		t.Fatalf("coverage.query_shape = %#v, want go_package_import_cycle", got)
	}
	if got := coverage["cycle_go_edges_missing_package"]; got != 0 {
		t.Fatalf("coverage.cycle_go_edges_missing_package = %#v, want 0", got)
	}
}

func goPackageCyclesFixtureRoot(t *testing.T) string {
	t.Helper()
	// The fixture lives outside the go module with the other ecosystem
	// fixtures, so the module tooling never treats it as a package.
	// go test runs with the package directory as cwd: five levels up is
	// the repository root.
	root, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "..",
		"tests", "fixtures", "ecosystems", "go_import_cycles",
	))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("fixture root %s unavailable: %v", root, err)
	}
	return root
}

func goPackageCyclesModulePath(t *testing.T, fixtureRoot string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v, want nil", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if module, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
			return strings.TrimSpace(module)
		}
	}
	t.Fatal("go.mod names no module")
	return ""
}

func intsToStrings(values []int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprint(value))
	}
	return out
}
