// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func goRootTestResult() map[string]any {
	return map[string]any{"labels": []any{"Function"}, "language": "go"}
}

func goRootTestEntity(source string) *querycontract.EntityContent {
	return &querycontract.EntityContent{Language: "go", SourceCache: source, RelativePath: "main.go"}
}

// TestNewDeadCodeGoPolicyContext proves the policy context resolves
// the language, normalizes source with comments stripped, and reads
// root kinds from result metadata first.
func TestNewDeadCodeGoPolicyContext(t *testing.T) {
	t.Parallel()

	result := goRootTestResult()
	result["metadata"] = map[string]any{"dead_code_root_kinds": []any{"go.cobra_run_signature"}}
	policy := NewDeadCodeGoPolicyContext(result, goRootTestEntity("// Serve handles requests\nfunc Serve(w http.ResponseWriter) {}"))
	if policy.Language != "go" {
		t.Errorf("Language = %q, want go", policy.Language)
	}
	if len(policy.RootKinds) != 1 || policy.RootKinds[0] != "go.cobra_run_signature" {
		t.Errorf("RootKinds = %v, want the metadata kinds", policy.RootKinds)
	}
}

// TestDeadCodeGoRootPredicates proves HTTP-handler, CLI-command, and
// framework-callback roots match on metadata kinds and on source
// signatures, and reject other languages and labels.
func TestDeadCodeGoRootPredicates(t *testing.T) {
	t.Parallel()

	handler := NewDeadCodeGoPolicyContext(
		map[string]any{"labels": []any{"Function"}, "metadata": map[string]any{"dead_code_root_kinds": []string{"go.net_http_handler_signature"}}},
		goRootTestEntity("func h() {}"),
	)
	if !deadCodeIsGoHTTPHandlerRoot(goRootTestResult(), handler) {
		t.Error("metadata handler kind must read as an HTTP handler root")
	}
	sourceHandler := DeadCodeGoPolicyContext{Language: "go"}
	if !deadCodeIsGoHTTPHandlerRoot(
		goRootTestResult(),
		DeadCodeGoPolicyContext{Language: "go", NormalizedSource: "func h(w http.responsewriter, r *http.request) {}"},
	) {
		_ = sourceHandler
		t.Error("http.ResponseWriter source signature must read as a handler root")
	}

	cli := DeadCodeGoPolicyContext{Language: "go", NormalizedSource: "run := func(cmd *cobra.command, args []string) {}"}
	if !deadCodeIsGoCLICommandRoot(goRootTestResult(), cli) {
		t.Error("cobra source signature must read as a CLI command root")
	}

	reconcile := map[string]any{"labels": []any{"Function"}, "name": "Reconcile"}
	callback := DeadCodeGoPolicyContext{
		Language:         "go",
		NormalizedSource: "func (r *r) reconcile(ctx context.context, req ctrl.request) (ctrl.result, error)",
	}
	if !deadCodeIsGoFrameworkCallbackRoot(reconcile, callback) {
		t.Error("controller-runtime reconcile signature must read as a callback root")
	}

	python := DeadCodeGoPolicyContext{Language: "python", NormalizedSource: "def h(): pass"}
	if deadCodeIsGoHTTPHandlerRoot(map[string]any{"labels": []any{"Function"}}, python) ||
		deadCodeIsGoCLICommandRoot(map[string]any{"labels": []any{"Function"}}, python) ||
		deadCodeIsGoFrameworkCallbackRoot(map[string]any{"labels": []any{"Function"}}, python) {
		t.Error("non-Go candidates must never read as Go roots")
	}
	variable := map[string]any{"labels": []any{"Variable"}}
	if deadCodeIsGoHTTPHandlerRoot(variable, handler) {
		t.Error("non-function labels must not read as handler roots")
	}
}

// TestDeadCodeIsGoSemanticRoot proves metadata semantic kinds keep a
// candidate rooted and count it once in the scan stats.
func TestDeadCodeIsGoSemanticRoot(t *testing.T) {
	t.Parallel()

	stats := &DeadCodePolicyStats{}
	policy := DeadCodeGoPolicyContext{Language: "go", RootKinds: []string{"go.type_reference"}}
	if !DeadCodeIsGoSemanticRoot(goRootTestResult(), policy, stats) {
		t.Error("semantic root kind must keep the candidate rooted")
	}
	if stats.GoSemanticRootsFromMetadata != 1 {
		t.Errorf("GoSemanticRootsFromMetadata = %d, want 1", stats.GoSemanticRootsFromMetadata)
	}
	plain := DeadCodeGoPolicyContext{Language: "go"}
	if DeadCodeIsGoSemanticRoot(goRootTestResult(), plain, stats) {
		t.Error("kind-less candidate must not read as a semantic root")
	}
}
