// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Truth-preservation scope for the workflow extraction (#8875 C6).
//
// Code-inspection verdict (2026-11-05, worktree drive/8875-truth-census):
// the claim path cannot affect reducer/graph/API/MCP fact truth because
//   - the moving leaf (go/internal/workflow, go/cmd/workflow-coordinator)
//     emits no facts: zero production imports of the facts packages;
//   - coordinator planners use the facts package as a deterministic
//     identity function only (facts.StableID for workflow generation IDs);
//     no production file constructs a fact envelope or writes fact records;
//   - no reducer projection or graph builder reads workflow state; the
//     only reducer touch is the retirement disposition census, and the
//     query/API touches are read-only collection-state explanations plus
//     one config-shape parse — all transport, not truth content.
// The one write coupling (HeartbeatClaim in the ingestion transaction,
// storage/postgres/ingestion.go) is a commit gate, not content: it can
// refuse a stale commit but cannot alter admitted facts. Its replacement
// (ValidateAndPinClaimForCommit) is proven under #9376.
//
// These tests fail closed: any future change introducing such a coupling
// must update the test and the census note together.
//
// No-Observability-Change: static source scan, no runtime signals.

// truthScopeRepoRoot returns the repository root for the worktree under test.
func truthScopeRepoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// This file lives at go/internal/workflow/.
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		// go.mod lives under go/ in this repo.
		root = filepath.Join(root, "go")
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
			t.Fatalf("cannot locate go.mod from %s: %v", file, err)
		}
		return filepath.Dir(root)
	}
	return root
}

// truthScopeGoFiles lists non-test .go files under relDir (repo-relative).
func truthScopeGoFiles(t *testing.T, root, relDir string) []string {
	t.Helper()

	var files []string
	err := filepath.Walk(filepath.Join(root, relDir), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", relDir, err)
	}
	sort.Strings(files)
	return files
}

// TestClaimPathEmitsNoFacts guards the scoping verdict: the moving leaf
// emits no facts. Any production import of a fact-definition, decode, or
// fact-persistence package from go/internal/workflow or
// go/cmd/workflow-coordinator fails this test.
func TestClaimPathEmitsNoFacts(t *testing.T) {
	t.Parallel()

	root := truthScopeRepoRoot(t)
	denySubstrings := []string{
		"github.com/eshu-hq/eshu/go/internal/facts",
		"factschema",
		"storage/postgres/facts",
	}

	fset := token.NewFileSet()
	var violations []string
	for _, dir := range []string{"go/internal/workflow", "go/cmd/workflow-coordinator"} {
		for _, rel := range truthScopeGoFiles(t, root, dir) {
			f, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", rel, err)
			}
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, deny := range denySubstrings {
					if strings.Contains(path, deny) {
						violations = append(violations, rel+": imports "+path)
					}
				}
			}
		}
	}
	if len(violations) != 0 {
		t.Errorf("claim path gained fact emission coupling:\n%s", strings.Join(violations, "\n"))
	}
}

// TestCoordinatorFactsUsageIsIdentityOnly guards the planner half of the
// verdict: coordinator production code may mint deterministic workflow
// generation IDs via facts.StableID, but must not construct fact values.
// Any other selector on the facts package fails this test.
func TestCoordinatorFactsUsageIsIdentityOnly(t *testing.T) {
	t.Parallel()

	root := truthScopeRepoRoot(t)
	fset := token.NewFileSet()
	var violations []string
	for _, rel := range truthScopeGoFiles(t, root, "go/internal/coordinator") {
		f, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		local := ""
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/eshu-hq/eshu/go/internal/facts" {
				if imp.Name != nil && imp.Name.Name == "." {
					pos := fset.Position(imp.Pos())
					violations = append(violations, rel+":"+pos.String()+" dot-imports facts (all selectors unqualified)")
					continue
				}
				local = "facts"
				if imp.Name != nil {
					local = imp.Name.Name
				}
			}
		}
		if local == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != local || sel.Sel.Name == "StableID" {
				return true
			}
			pos := fset.Position(sel.Pos())
			violations = append(violations, rel+":"+pos.String()+" uses facts."+sel.Sel.Name)
			return true
		})
	}
	if len(violations) != 0 {
		t.Errorf("coordinator gained fact-content coupling (StableID-only rule broken):\n%s", strings.Join(violations, "\n"))
	}
}

// TestCoordinatorImportsNoFactPersistence guards the coordinator half of
// the no-emission verdict: go/internal/coordinator production code must
// not import fact-definition or fact-persistence packages (factschema,
// storage/postgres/facts). The facts package itself is governed by
// TestCoordinatorFactsUsageIsIdentityOnly; this test closes the sibling
// coupling classes a future change could otherwise add silently.
func TestCoordinatorImportsNoFactPersistence(t *testing.T) {
	t.Parallel()

	root := truthScopeRepoRoot(t)
	denySubstrings := []string{
		"factschema",
		"storage/postgres/facts",
	}

	fset := token.NewFileSet()
	var violations []string
	for _, rel := range truthScopeGoFiles(t, root, "go/internal/coordinator") {
		abs := filepath.Join(root, rel)
		f, err := parser.ParseFile(fset, abs, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, deny := range denySubstrings {
				if strings.Contains(path, deny) {
					violations = append(violations, rel+": imports "+path)
				}
			}
		}
		// No direct SQL writes either: a fact_records reference in
		// coordinator source fails this test even with no new import.
		src, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if strings.Contains(string(src), "fact_records") {
			violations = append(violations, rel+": references fact_records")
		}
	}
	if len(violations) != 0 {
		t.Errorf("coordinator gained fact-persistence coupling:\n%s", strings.Join(violations, "\n"))
	}
}

// truthScopeKnownReaders enumerates the non-test files outside the moving
// tree that may reference workflow state, with the reason each is
// transport (read-only reporting or config shape) rather than truth
// content. Any new file outside this set fails the test.
var truthScopeKnownReaders = map[string]string{
	// Retirement disposition census: documents that workflow_work_items is
	// kept because a later claim for a retired repo is refused at commit.
	"go/internal/reducer/retirement/census.go": "retirement census",
	// Collection-state explanation queries (read-only SQL over scheduling
	// tables for readiness/evidence surfaces).
	"go/internal/query/container_image_candidate_explanation.go":                "collection-state explanation",
	"go/internal/query/openapi/components_active_work_source.go":                "status description text",
	"go/internal/query/security_alert_collection_state_postgres.go":             "collection-state explanation",
	"go/internal/query/supply/chain/impact/readiness_postgres_query_targets.go": "readiness explanation",
	// Parses DesiredCollectorInstance JSON for metrics-source selection;
	// config shape only, no workflow state read.
	"go/cmd/api/metrics_source.go": "config-shape parse",
}

// truthScopePinnedReaderHits pins the per-marker workflow-state reference
// counts inside each allowlisted reader: a new coupling inside a known file
// changes a marker count and fails the test, including a substitution that
// keeps the file total unchanged. Markers absent from a file pin to zero.
// Refresh deliberately with the test when the reader's legitimate
// references change.
var truthScopePinnedReaderHits = map[string]map[string]int{
	"go/internal/reducer/retirement/census.go": {
		"workflow_runs": 2, "workflow_work_items": 4,
	},
	"go/internal/query/container_image_candidate_explanation.go": {
		"workflow_work_items": 1,
	},
	"go/internal/query/openapi/components_active_work_source.go": {
		"workflow_work_items": 1,
	},
	"go/internal/query/security_alert_collection_state_postgres.go": {
		"workflow_work_items": 5,
	},
	"go/internal/query/supply/chain/impact/readiness_postgres_query_targets.go": {
		"workflow_runs": 1,
	},
	"go/cmd/api/metrics_source.go": {
		"go/internal/workflow\"": 1,
	},
}

// TestTruthSurfacesReadNoWorkflowState guards the consumer half of the
// verdict: no reducer projection, graph builder, MCP tool, query handler,
// or API command reads workflow claim state except the known read-only
// readers above. A new reference fails this test, as does a substitution
// that keeps the file total.
func TestTruthSurfacesReadNoWorkflowState(t *testing.T) {
	t.Parallel()

	root := truthScopeRepoRoot(t)
	dirs := []string{
		"go/internal/reducer",
		"go/internal/graph",
		"go/internal/mcp",
		"go/internal/query",
		"go/cmd/api",
	}
	markers := []string{
		"go/internal/workflow\"",
		"go/internal/workflow/",
		"workflow_runs",
		"workflow_work_items",
		"workflow_claims",
		"workflow_claim_pacing",
		"workflow_run_completeness",
	}

	var violations []string
	for _, dir := range dirs {
		for _, rel := range truthScopeGoFiles(t, root, dir) {
			raw, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			counts := make(map[string]int, len(markers))
			hits := 0
			var hitMarkers []string
			for _, marker := range markers {
				n := strings.Count(string(raw), marker)
				counts[marker] = n
				if n > 0 {
					hits += n
					hitMarkers = append(hitMarkers, marker)
				}
			}
			if hits == 0 {
				continue
			}
			want, known := truthScopeKnownReaders[rel]
			if !known || want == "" {
				violations = append(violations, rel+": new workflow-state reference ("+strings.Join(hitMarkers, ", ")+")")
				continue
			}
			// Allowlisted files are pinned by per-marker reference counts:
			// a new coupling inside a known reader changes a marker count
			// and fails, including a substitution that keeps the file total.
			pinned, ok := truthScopePinnedReaderHits[rel]
			if !ok {
				violations = append(violations, rel+": workflow-state reference count changed (have new total)")
				continue
			}
			for _, marker := range markers {
				if counts[marker] != pinned[marker] {
					violations = append(violations, rel+": workflow-state marker count changed for "+marker)
					break
				}
			}
		}
	}
	if len(violations) != 0 {
		t.Errorf("truth surface gained workflow-state coupling:\n%s", strings.Join(violations, "\n"))
	}
}
