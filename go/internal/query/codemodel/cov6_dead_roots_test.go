// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// cov6RootResult builds the minimal candidate row carrying parser metadata
// root kinds, the same shape DeadCodeRootKindsFromMetadata reads.
func cov6RootResult(kind string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"dead_code_root_kinds": []any{kind}},
	}
}

func cov6RootEntity(language string) *querycontract.EntityContent {
	return &querycontract.EntityContent{
		EntityID:   "e1",
		EntityName: "main",
		EntityType: "Function",
		Language:   language,
	}
}

func TestCov6ComplexityListLimitAndTrim(t *testing.T) {
	t.Parallel()
	if got := NormalizeComplexityListLimit(0); got != 10 {
		t.Fatalf("NormalizeComplexityListLimit(0) = %d, want 10", got)
	}
	if got := NormalizeComplexityListLimit(-3); got != 10 {
		t.Fatalf("NormalizeComplexityListLimit(-3) = %d, want 10", got)
	}
	if got := NormalizeComplexityListLimit(500); got != 100 {
		t.Fatalf("NormalizeComplexityListLimit(500) = %d, want 100", got)
	}
	if got := NormalizeComplexityListLimit(25); got != 25 {
		t.Fatalf("NormalizeComplexityListLimit(25) = %d, want 25", got)
	}
	rows := []map[string]any{{"id": "a"}, {"id": "b"}, {"id": "c"}}
	if got, truncated := TrimComplexityResults(rows, 5); truncated || len(got) != 3 {
		t.Fatalf("TrimComplexityResults(fits) = (%d,%v), want (3,false)", len(got), truncated)
	}
	if got, truncated := TrimComplexityResults(rows, 2); !truncated || len(got) != 2 {
		t.Fatalf("TrimComplexityResults(over) = (%d,%v), want (2,true)", len(got), truncated)
	}
}

func TestCov6ComplexityAmbiguousErrorShape(t *testing.T) {
	t.Parallel()
	err := ComplexityAmbiguousError{
		FunctionName: "run",
		RepoID:       "repo-1",
		Candidates:   []map[string]any{{"entity_id": "e1"}},
		Truncated:    true,
	}
	if !strings.Contains(err.Error(), `"run"`) || !strings.Contains(err.Error(), `"repo-1"`) {
		t.Fatalf("Error() = %q, want function and repo quoted", err.Error())
	}
	details := err.Details()
	if details["status"] != "ambiguous" || details["function_name"] != "run" || details["truncated"] != true {
		t.Fatalf("Details() = %v, want status/function/truncated", details)
	}
	if firstStringOrEmpty(nil) != "" || firstStringOrEmpty([]string{"Function"}) != "Function" {
		t.Fatal("firstStringOrEmpty mismatch")
	}

	rows := []map[string]any{
		{"id": "e2", "name": "run", "labels": []any{"Function"}, "file_path": "b.go", "repo_id": "repo-1", "start_line": 3, "end_line": 9},
		{"id": "e1", "name": "run", "labels": []any{"Function"}, "file_path": "a.go", "repo_id": "repo-1", "start_line": 1, "end_line": 5},
	}
	mapped := ComplexityCandidateMaps(rows)
	if len(mapped) != 2 {
		t.Fatalf("ComplexityCandidateMaps = %d rows, want 2", len(mapped))
	}
	if mapped[0]["entity_id"] != "e1" || mapped[0]["handle"] != "entity:e1" || mapped[0]["entity_type"] != "Function" {
		t.Fatalf("first candidate = %v, want sorted a.go row with handle and type", mapped[0])
	}

	// The truth envelope needs the capability row; stub it the way the
	// call-graph metrics tests do when the contract package is not linked.
	derived := querycontract.TruthLevelDerived
	querycontract.SetCapabilitySupport("code_quality.complexity", querycontract.CapabilitySupport{
		LocalLightweightMax:   &derived,
		LocalAuthoritativeMax: &derived,
		LocalFullStackMax:     &derived,
		ProductionMax:         &derived,
	})

	// Envelope-accepting callers get the truth-envelope refusal.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/complexity", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	WriteComplexityAmbiguousError(rec, req, err, querycontract.ProfileLocalAuthoritative)
	if rec.Code != http.StatusConflict {
		t.Fatalf("envelope refusal status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ambiguous") {
		t.Fatalf("envelope refusal body = %q, want ambiguity marker", rec.Body.String())
	}
	// Plain callers get the legacy details map with error text.
	plain := httptest.NewRecorder()
	WriteComplexityAmbiguousError(plain, httptest.NewRequest(http.MethodGet, "/complexity", nil), err, querycontract.ProfileLocalAuthoritative)
	if plain.Code != http.StatusConflict {
		t.Fatalf("legacy refusal status = %d, want 409", plain.Code)
	}
	if !strings.Contains(plain.Body.String(), "Conflict") {
		t.Fatalf("legacy refusal body = %q, want http status text", plain.Body.String())
	}
}

func TestCov6CodeFlowLanguageAndFloat(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"C#": "csharp", "cs": "csharp", "JS": "javascript", "ts": "typescript", "Go": "go",
	} {
		if got := normalizeCodeFlowLanguage(raw); got != want {
			t.Fatalf("normalizeCodeFlowLanguage(%q) = %q, want %q", raw, got, want)
		}
	}
	row := map[string]any{
		"f64": float64(1.5), "f32": float32(2.5), "i64": int64(3), "i": 4,
		"s": "nope", "n": nil,
	}
	for key, want := range map[string]float64{"f64": 1.5, "f32": 2.5, "i64": 3, "i": 4, "s": 0, "n": 0, "missing": 0} {
		if got := floatVal(row, key); got != want {
			t.Fatalf("floatVal(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestCov6ImportCycleFlagStates(t *testing.T) {
	t.Parallel()
	runtime := map[string]any{"type_only": false, "deferred": false, "inferred": false}
	if got := importCycleEdgeStateFromRow(runtime); got != edgeStateRuntime {
		t.Fatalf("runtime row state = %v, want runtime", got)
	}
	if got := importCycleEdgeStateFromRow(map[string]any{"type_only": true, "deferred": false, "inferred": false}); got != edgeStateTypeOnly {
		t.Fatalf("type-only row state = %v, want type_only", got)
	}
	if got := importCycleEdgeStateFromRow(map[string]any{"type_only": false, "deferred": true, "inferred": false}); got != edgeStateDeferred {
		t.Fatalf("deferred row state = %v, want deferred", got)
	}
	if got := importCycleEdgeStateFromRow(map[string]any{"type_only": false, "deferred": false, "inferred": true}); got != edgeStateInferred {
		t.Fatalf("inferred row state = %v, want inferred", got)
	}
	// A legacy edge predating the flags reads as unknown, never runtime.
	if got := importCycleEdgeStateFromRow(map[string]any{}); got != edgeStateUnknown {
		t.Fatalf("legacy row state = %v, want unknown", got)
	}
	if got := importCycleEdgeStateFromRow(map[string]any{"type_only": "yes", "deferred": false, "inferred": false}); got != edgeStateUnknown {
		t.Fatalf("non-bool flag state = %v, want unknown", got)
	}
	// Folding keeps the strongest proof regardless of order.
	if got := foldImportCycleEdgeState(edgeStateInferred, edgeStateRuntime); got != edgeStateRuntime {
		t.Fatalf("fold(inferred,runtime) = %v, want runtime", got)
	}
	if got := foldImportCycleEdgeState(edgeStateTypeOnly, edgeStateUnknown); got != edgeStateUnknown {
		t.Fatalf("fold(type_only,unknown) = %v, want unknown", got)
	}
	kept, counts := partitionImportCycleEdges([]importCycleEdge{
		{state: edgeStateRuntime},
		{state: edgeStateTypeOnly},
		{state: edgeStateDeferred},
		{state: edgeStateInferred},
		{state: edgeStateUnknown},
	})
	if len(kept) != 3 || counts.Considered != 5 || counts.TypeOnlyExcluded != 1 || counts.DeferredExcluded != 1 || counts.Inferred != 1 || counts.Unknown != 1 {
		t.Fatalf("partition = (%d kept, %+v), want (3, 5/1/1/1/1)", len(kept), counts)
	}
	for state, want := range map[importCycleEdgeState]string{
		edgeStateRuntime: "runtime", edgeStateInferred: "inferred", edgeStateUnknown: "unknown",
		edgeStateTypeOnly: "type_only", edgeStateDeferred: "deferred",
	} {
		if got := importCycleFlagStateName(state); got != want {
			t.Fatalf("flagStateName(%v) = %q, want %q", state, got, want)
		}
	}
	if got := importCycleLabelFor([]importCycleEdgeState{edgeStateRuntime}); got != CycleLabelRuntime {
		t.Fatalf("label(runtime) = %q, want runtime", got)
	}
	if got := importCycleLabelFor([]importCycleEdgeState{edgeStateRuntime, edgeStateUnknown}); got != CycleLabelFlagsUnknown {
		t.Fatalf("label(runtime,unknown) = %q, want flags_unknown", got)
	}
	if got := importCycleLabelFor([]importCycleEdgeState{edgeStateUnknown, edgeStateInferred}); got != CycleLabelAmbiguous {
		t.Fatalf("label(unknown,inferred) = %q, want ambiguous", got)
	}
}

func TestCov6HybridRerankNoopPaths(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{{"id": "e1"}, {"id": "e2"}}
	var nilRanker *CodeHybridRanker
	if _, applied := nilRanker.Rerank(context.Background(), "repo-1", "query", rows); applied {
		t.Fatal("nil ranker Rerank applied = true, want false")
	}
	disabled := NewCodeHybridRanker(false)
	if _, applied := disabled.Rerank(context.Background(), "repo-1", "query", rows); applied {
		t.Fatal("disabled ranker Rerank applied = true, want false")
	}
	enabled := NewCodeHybridRanker(true)
	if _, applied := enabled.Rerank(context.Background(), "", "query", rows); applied {
		t.Fatal("empty repo Rerank applied = true, want false")
	}
	if _, applied := enabled.Rerank(context.Background(), "repo-1", "query", rows[:1]); applied {
		t.Fatal("single row Rerank applied = true, want false")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, applied := enabled.Rerank(cancelled, "repo-1", "query", rows); applied {
		t.Fatal("cancelled ctx Rerank applied = true, want false")
	}
}

func TestCov6DeadCodeLanguageRoots(t *testing.T) {
	t.Parallel()
	// Every classifier rejects a foreign language before touching metadata.
	foreign := &querycontract.EntityContent{EntityID: "e9", EntityName: "main", Language: "cobol"}
	stats := &DeadCodePolicyStats{}
	if DeadCodeIsCRoot(nil, foreign, stats) || DeadCodeIsCPPRoot(nil, foreign, stats) ||
		DeadCodeIsCSharpRoot(nil, foreign, stats) || DeadCodeIsDartRoot(nil, foreign, stats) ||
		DeadCodeIsElixirRoot(nil, foreign, stats) || DeadCodeIsGroovyRoot(nil, foreign, stats) ||
		DeadCodeIsHaskellRoot(nil, foreign, stats) || DeadCodeIsJavaRoot(nil, foreign, stats) ||
		DeadCodeIsKotlinRoot(nil, foreign, stats) || DeadCodeIsPerlRoot(nil, foreign, stats) ||
		DeadCodeIsPHPRoot(nil, foreign, stats) || DeadCodeIsRubyRoot(nil, foreign, stats, nil) ||
		DeadCodeIsRustRoot(nil, foreign, stats) || DeadCodeIsScalaRoot(nil, foreign, stats) ||
		DeadCodeIsSwiftRoot(nil, foreign, stats) || DeadCodeIsPythonFrameworkRoot(nil, foreign, stats) ||
		DeadCodeIsJavaScriptFrameworkRoot(nil, foreign, stats) ||
		DeadCodeIsPythonAnonymousLambda(map[string]any{"name": "main"}, foreign) ||
		DeadCodeIsRustCargoAuxiliaryTarget(nil, foreign) {
		t.Fatal("a foreign-language candidate must never be a language root")
	}

	// Parser-metadata kinds admit each language's entrypoint root.
	admissions := []struct {
		name     string
		language string
		kind     string
		isRoot   func(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool
	}{
		{"c", "c", "c.main_function", DeadCodeIsCRoot},
		{"cpp", "cpp", "cpp.main_function", DeadCodeIsCPPRoot},
		{"csharp", "c_sharp", "csharp.main_method", DeadCodeIsCSharpRoot},
		{"dart", "dart", "dart.main_function", DeadCodeIsDartRoot},
		{"elixir", "elixir", "elixir.application_start", DeadCodeIsElixirRoot},
		{"groovy", "groovy", "groovy.jenkins_pipeline_entrypoint", DeadCodeIsGroovyRoot},
		{"haskell", "haskell", "haskell.main_function", DeadCodeIsHaskellRoot},
		{"java", "java", "java.main_method", DeadCodeIsJavaRoot},
		{"kotlin", "kotlin", "kotlin.main_function", DeadCodeIsKotlinRoot},
		{"perl", "perl", "perl.script_entrypoint", DeadCodeIsPerlRoot},
		{"php", "php", "php.script_entrypoint", DeadCodeIsPHPRoot},
		{"rust", "rust", "rust.main_function", DeadCodeIsRustRoot},
		{"scala", "scala", "scala.main_method", DeadCodeIsScalaRoot},
		{"swift", "swift", "swift.main_function", DeadCodeIsSwiftRoot},
		{"python", "python", "python.fastapi_route_decorator", DeadCodeIsPythonFrameworkRoot},
		{"javascript", "javascript", "javascript.node_package_export", DeadCodeIsJavaScriptFrameworkRoot},
	}
	for _, tc := range admissions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Fresh counters per language: sibling subtests run in parallel.
			admission := &DeadCodePolicyStats{}
			if !tc.isRoot(cov6RootResult(tc.kind), cov6RootEntity(tc.language), admission) {
				t.Fatalf("%s metadata kind %q not admitted as root", tc.name, tc.kind)
			}
			if admission.ParserMetadataFrameworkRoots != 1 {
				t.Fatalf("%s admission did not record parser-metadata provenance", tc.name)
			}
			// An unrelated kind in the right language stays a non-root.
			if tc.isRoot(cov6RootResult("unrelated.kind"), cov6RootEntity(tc.language), &DeadCodePolicyStats{}) {
				t.Fatalf("%s unrelated kind admitted as root", tc.name)
			}
		})
	}

	if !DeadCodeIsRubyRoot(cov6RootResult("ruby.rails_controller_action"), cov6RootEntity("ruby"), &DeadCodePolicyStats{}, nil) {
		t.Fatal("ruby rails_controller_action without a downgrade row must stay a root")
	}
	if !DeadCodeIsPythonAnonymousLambda(
		map[string]any{"metadata": map[string]any{"semantic_kind": "lambda"}},
		&querycontract.EntityContent{EntityName: "lambda@3", Language: "python"},
	) {
		t.Fatal("python lambda@ entity with lambda semantic kind must be a root")
	}
	aux := &querycontract.EntityContent{EntityName: "bench", Language: "rust", RelativePath: "benches/parse.rs"}
	if !DeadCodeIsRustCargoAuxiliaryTarget(map[string]any{"path": "benches/parse.rs"}, aux) {
		t.Fatal("rust benches/ target must be a cargo auxiliary target")
	}
	// TypeScript public API re-exports count as framework roots.
	if !deadCodeHasTypeScriptPublicAPIRoot([]string{"typescript.public_api_reexport"}) {
		t.Fatal("typescript.public_api_reexport must be a public API root")
	}
	if deadCodeHasTypeScriptPublicAPIRoot([]string{"typescript.something_else"}) {
		t.Fatal("unrelated typescript kind must not be a public API root")
	}
}

func TestCov6DeadCodeGoFrameworkRoot(t *testing.T) {
	t.Parallel()
	stats := &DeadCodePolicyStats{}
	foreign := NewDeadCodeGoPolicyContext(map[string]any{}, &querycontract.EntityContent{Language: "python"})
	if DeadCodeIsGoFrameworkRoot(map[string]any{}, foreign, stats) {
		t.Fatal("non-go policy must never be a go framework root")
	}
	handler := NewDeadCodeGoPolicyContext(
		map[string]any{"labels": []any{"Function"}},
		&querycontract.EntityContent{
			Language:    "go",
			SourceCache: "func Serve(w http.ResponseWriter, r *http.Request) {}",
		},
	)
	if handler.Language != "go" || len(handler.NormalizedSource) == 0 {
		t.Fatalf("policy context = %+v, want normalized go source", handler)
	}
	if !DeadCodeIsGoFrameworkRoot(map[string]any{"labels": []any{"Function"}}, handler, stats) {
		t.Fatal("http handler signature must be a go framework root via source fallback")
	}
	if stats.SourceFallbackFrameworkRoots != 1 {
		t.Fatalf("source fallback count = %d, want 1", stats.SourceFallbackFrameworkRoots)
	}
	metadataPolicy := DeadCodeGoPolicyContext{
		Language:  "go",
		RootKinds: []string{"go.cobra_run_signature"},
	}
	if !DeadCodeIsGoFrameworkRoot(map[string]any{"labels": []any{"Function"}}, metadataPolicy, stats) {
		t.Fatal("cobra run signature must be a go framework root via parser metadata")
	}
	if stats.ParserMetadataFrameworkRoots != 1 {
		t.Fatalf("parser metadata count = %d, want 1", stats.ParserMetadataFrameworkRoots)
	}
	plain := DeadCodeGoPolicyContext{Language: "go"}
	if DeadCodeIsGoFrameworkRoot(map[string]any{}, plain, stats) {
		t.Fatal("plain go function with no signals must not be a framework root")
	}
	if got := deadCodeNormalizedSource(nil); got != "" {
		t.Fatalf("deadCodeNormalizedSource(nil) = %q, want empty", got)
	}
}
