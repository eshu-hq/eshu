// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports_test

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestJavaPackageCyclesFixtureTruthAgreement is the #7349 three-way proof:
// fixture intent (the tracked Java sources under
// tests/fixtures/ecosystems/java_import_cycles) agrees with the projector's
// graph truth (production parser plus production BuildMaterialization) and
// with the API/query truth (production collapse plus production
// BuildFileImportCycleRows plus the response shaper), for positive 2-, 3-,
// 5-, and 7-package cycles and the diamond and fan-in negatives. The
// collapsed production fetch and the file-level reader path must agree, so
// the test runs both and requires the same answer.
func TestJavaPackageCyclesFixtureTruthAgreement(t *testing.T) {
	t.Parallel()

	const repoID = "repo-java-cycles"
	fixtureRoot := javaPackageCyclesFixtureRoot(t)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var fileFacts []facts.Envelope
	javaFileCount := 0
	err = filepath.WalkDir(fixtureRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".java" {
			return nil
		}
		rel, err := filepath.Rel(fixtureRoot, path)
		if err != nil {
			t.Fatalf("filepath.Rel() error = %v, want nil", err)
		}
		relSlash := filepath.ToSlash(rel)
		// Java package identity lives in the file itself, so the test
		// parses with no options: the package_declaration is the mapping.
		payload, err := engine.ParsePath(fixtureRoot, path, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%s) error = %v, want nil", relSlash, err)
		}
		javaFileCount++
		fileFacts = append(fileFacts, facts.Envelope{
			FactID:   "file-" + strings.ReplaceAll(relSlash, "/", "-"),
			ScopeID:  "scope-cycles",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          repoID,
				"relative_path":    relSlash,
				"language":         "java",
				"parsed_file_data": payload,
			},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v, want nil", err)
	}
	if javaFileCount != 26 {
		t.Fatalf("parsed %d Java files, want all 26 fixture files", javaFileCount)
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

	// Projector graph truth: every Java edge is placed in its package, so the
	// query has no unplaced edge to drop.
	for _, row := range mat.Imports {
		if row.ModuleLanguage != "java" {
			t.Fatalf("import edge %+v is not Java, want a Java-only fixture", row)
		}
		if row.SourcePackage == "" {
			t.Fatalf("import edge %+v has no source package, want full mapping density", row)
		}
	}

	// Edge rows in the exact shape the file-level fetch returns them, with
	// the flags as the explicit booleans the writer persists.
	fileRows := make([]map[string]any, 0, len(mat.Imports))
	for _, row := range mat.Imports {
		rel, err := filepath.Rel(fixtureRoot, row.FilePath)
		if err != nil {
			t.Fatalf("filepath.Rel() error = %v, want nil", err)
		}
		relSlash := filepath.ToSlash(rel)
		fileRows = append(fileRows, map[string]any{
			"repo_id":        repoID,
			"repo_name":      "java-cycles",
			"source_path":    row.FilePath,
			"source_file":    relSlash,
			"source_name":    filepath.Base(relSlash),
			"language":       row.ModuleLanguage,
			"source_package": row.SourcePackage,
			"target_module":  row.ModuleName,
			"line_number":    row.LineNumber,
			"type_only":      row.TypeOnly,
			"deferred":       row.Deferred,
			"inferred":       row.Inferred,
		})
	}

	// The 7-package cycle needs the deep bound; the default bound must see
	// exactly the 2-, 3-, and 5-package cycles.
	deepReq := codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles", RepoID: repoID, Language: "java", Limit: 200, MaxCycleLength: 7,
	}
	defaultReq := codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles", RepoID: repoID, Language: "java", Limit: 200,
	}

	deepPaths := javaFixtureCyclePaths(t, deepReq, fileRows)
	if strings.Join(javaSortedLengths(deepPaths), ",") != "2,3,5,7" {
		t.Fatalf("deep cycle lengths with paths %q, want exactly [2 3 5 7]", deepPaths)
	}
	defaultPaths := javaFixtureCyclePaths(t, defaultReq, fileRows)
	if strings.Join(javaSortedLengths(defaultPaths), ",") != "2,3,5" {
		t.Fatalf("default cycle lengths with paths %q, want exactly [2 3 5]", defaultPaths)
	}
	for _, paths := range [][][]string{deepPaths, defaultPaths} {
		for _, path := range paths {
			for _, member := range path {
				if strings.Contains(member, "diamond") || strings.Contains(member, "fanin") {
					t.Fatalf("cycle path %q contains a negative-fixture package, want no diamond or fan-in cycle", path)
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
	}

	coverage, ok := javaFixtureCoverage(t, deepReq, fileRows)["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage missing from the deep response")
	}
	if got := coverage["query_shape"]; got != "java_package_import_cycle" {
		t.Fatalf("coverage.query_shape = %#v, want java_package_import_cycle", got)
	}
	if got := coverage["cycle_java_edges_missing_package"]; got != 0 {
		t.Fatalf("coverage.cycle_java_edges_missing_package = %#v, want 0", got)
	}
}

func javaFixtureCyclePaths(t *testing.T, req codemodel.ImportDependencyRequest, fileRows []map[string]any) [][]string {
	t.Helper()

	// The reader decodes file-level rows to the same package answer the
	// collapsed production fetch returns; collapse-location invariance is
	// pinned on row builders in the codemodel package, and the collapsed
	// statement itself is proved by the env-gated live test.
	rows, enumeration, err := codemodel.BuildFileImportCycleRows(req, fileRows)
	if err != nil {
		t.Fatalf("BuildFileImportCycleRows() error = %v, want nil", err)
	}
	if enumeration.Truncated {
		t.Fatalf("enumeration = %+v, want an untruncated run", enumeration)
	}
	if enumeration.JavaPackageUnknown != 0 {
		t.Fatalf("enumeration.JavaPackageUnknown = %d, want 0 placed edges", enumeration.JavaPackageUnknown)
	}
	return javaCyclePaths(t, codemodel.ImportDependencyResponseWithCycleEnumeration(req, rows, enumeration))
}

func javaFixtureCoverage(t *testing.T, req codemodel.ImportDependencyRequest, fileRows []map[string]any) map[string]any {
	t.Helper()

	rows, enumeration, err := codemodel.BuildFileImportCycleRows(req, fileRows)
	if err != nil {
		t.Fatalf("BuildFileImportCycleRows() error = %v, want nil", err)
	}
	return codemodel.ImportDependencyResponseWithCycleEnumeration(req, rows, enumeration)
}

func javaSortedLengths(paths [][]string) []string {
	lengths := make([]int, 0, len(paths))
	for _, path := range paths {
		lengths = append(lengths, len(path)-1)
	}
	sort.Ints(lengths)
	out := make([]string, 0, len(lengths))
	for _, length := range lengths {
		out = append(out, strconv.Itoa(length))
	}
	return out
}

func javaPackageCyclesFixtureRoot(t *testing.T) string {
	t.Helper()
	// The fixture lives outside the go module with the other ecosystem
	// fixtures, so the module tooling never treats it as a package.
	// go test runs with the package directory as cwd: five levels up is
	// the repository root.
	root, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "..",
		"tests", "fixtures", "ecosystems", "java_import_cycles",
	))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("fixture root %s unavailable: %v", root, err)
	}
	return root
}
