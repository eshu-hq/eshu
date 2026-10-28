// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func cov6MetricFunction(key, id, path, language, name string, start int) callGraphMetricFunction {
	return callGraphMetricFunction{
		key: key, id: id, path: path, language: language, name: name, startLine: start, endLine: start + 5,
	}
}

func TestCov6HubCallGraphMetricRankLess(t *testing.T) {
	t.Parallel()
	base := hubCallGraphMetricRank{function: cov6MetricFunction("k", "e1", "a.go", "go", "run", 1)}
	hotter := hubCallGraphMetricRank{function: cov6MetricFunction("k", "e1", "a.go", "go", "run", 1), incomingCalls: 3}
	if !hubCallGraphMetricRankLess(hotter, base) || hubCallGraphMetricRankLess(base, hotter) {
		t.Fatal("total-degree ordering mismatch")
	}
	incoming := hubCallGraphMetricRank{function: base.function, incomingCalls: 2, outgoingCalls: 1}
	outgoing := hubCallGraphMetricRank{function: base.function, incomingCalls: 1, outgoingCalls: 2}
	if !hubCallGraphMetricRankLess(incoming, outgoing) {
		t.Fatal("equal totals must prefer more incoming calls")
	}
	early := hubCallGraphMetricRank{function: cov6MetricFunction("k", "e1", "a.go", "go", "run", 1)}
	late := hubCallGraphMetricRank{function: cov6MetricFunction("k", "e1", "a.go", "go", "run", 9)}
	if !hubCallGraphMetricRankLess(early, late) {
		t.Fatal("equal calls must prefer the earlier start line")
	}
	alpha := hubCallGraphMetricRank{function: cov6MetricFunction("k", "e1", "a.go", "go", "alpha", 1)}
	beta := hubCallGraphMetricRank{function: cov6MetricFunction("k", "e1", "a.go", "go", "beta", 1)}
	if !hubCallGraphMetricRankLess(alpha, beta) {
		t.Fatal("equal calls and lines must prefer the earlier name")
	}
	if hubCallGraphMetricRankLess(base, base) {
		t.Fatal("identical ranks must not order")
	}
}

func TestCov6HubCallGraphMetricRows(t *testing.T) {
	t.Parallel()
	functions := map[string]callGraphMetricFunction{
		"a": cov6MetricFunction("ka", "a", "a.go", "go", "alpha", 1),
		"b": cov6MetricFunction("kb", "b", "b.go", "go", "beta", 1),
		"c": cov6MetricFunction("kc", "c", "c.py", "python", "gamma", 1),
	}
	edges := map[callGraphMetricEdgeKey]struct{}{
		{sourceKey: "a", targetKey: "b"}: {},
		{sourceKey: "c", targetKey: "b"}: {},
	}
	rows := hubCallGraphMetricRows(CallGraphMetricsRequest{RepoID: "repo-1"}, edges, functions)
	if len(rows) != 3 {
		t.Fatalf("hub rows = %d, want 3", len(rows))
	}
	// b has two incoming calls and must rank first.
	if rows[0]["function_id"] != "b" || rows[0]["incoming_calls"] != 2 || rows[0]["total_degree"] != 2 {
		t.Fatalf("first hub row = %v, want b with 2 incoming", rows[0])
	}
	goOnly := hubCallGraphMetricRows(CallGraphMetricsRequest{RepoID: "repo-1", Language: "go"}, edges, functions)
	if len(goOnly) != 2 {
		t.Fatalf("go-filtered hub rows = %d, want 2", len(goOnly))
	}
	pastEnd := hubCallGraphMetricRows(CallGraphMetricsRequest{RepoID: "repo-1", Offset: 99}, edges, functions)
	if len(pastEnd) != 0 {
		t.Fatalf("offset past end = %d rows, want 0", len(pastEnd))
	}
}

func TestCov6RecursiveCallGraphMetricRows(t *testing.T) {
	t.Parallel()
	functions := map[string]callGraphMetricFunction{
		"a": cov6MetricFunction("ka", "a", "a.go", "go", "alpha", 1),
		"b": cov6MetricFunction("kb", "b", "b.go", "go", "beta", 5),
		"c": cov6MetricFunction("kc", "c", "c.go", "go", "gamma", 9),
	}
	edges := map[callGraphMetricEdgeKey]struct{}{
		{sourceKey: "a", targetKey: "b"}: {},
		{sourceKey: "b", targetKey: "a"}: {},
		{sourceKey: "b", targetKey: "c"}: {},
	}
	rows := recursiveCallGraphMetricRows(CallGraphMetricsRequest{RepoID: "repo-1"}, edges, functions)
	if len(rows) != 1 {
		t.Fatalf("recursive rows = %d, want 1 reciprocal pair", len(rows))
	}
	if rows[0]["partner_name"] != "beta" {
		t.Fatalf("reciprocal row = %v, want partner beta", rows[0])
	}
	// Row ordering falls back across every key level.
	left := map[string]any{"file_path": "a.go", "start_line": 1}
	right := map[string]any{"file_path": "b.go", "start_line": 2}
	if !callGraphMetricFunctionRowLess(left, right, "partner_", "partner_") {
		t.Fatal("row less must prefer the earlier file path")
	}
	if less, decided := callGraphMetricValueLess(left, right, "start_line", "start_line"); !decided || !less {
		t.Fatal("value less must decide numeric start_line")
	}
	if _, decided := callGraphMetricValueLess(left, left, "file_path", "file_path"); decided {
		t.Fatal("identical values must not decide")
	}
}

func TestCov6DeadCodeMaturityReports(t *testing.T) {
	t.Parallel()
	maturity := DeadCodeLanguageMaturityReport()
	if len(maturity) == 0 {
		t.Fatal("maturity report is empty")
	}
	maturity["go"] = "mutated"
	if again := DeadCodeLanguageMaturityReport(); again["go"] == "mutated" {
		t.Fatal("maturity report must be a copy, not the live table")
	}
	blockers := DeadCodeLanguageExactnessBlockerReport()
	if len(blockers) == 0 {
		t.Fatal("blocker report is empty")
	}

	// gin is observed with no root model; net_http is modeled; cobol has no entry.
	results := []map[string]any{
		{"language": "go", "metadata": map[string]any{"framework": "gin"}},
		{"language": "go", "metadata": map[string]any{"framework": "net_http"}},
		{"language": "cobol", "metadata": map[string]any{"framework": "something"}},
		{"language": "", "metadata": map[string]any{"framework": "gin"}},
		{"language": "go"},
	}
	unmodeled := deadCodeFrameworksWithoutRootModel(results)
	if len(unmodeled["go"]) != 1 || unmodeled["go"][0] != "gin" {
		t.Fatalf("unmodeled frameworks = %v, want map[go:[gin]]", unmodeled)
	}
	note := deadCodeNoRootModelNote(map[string][]string{"python": {"django"}, "go": {"gin"}})
	if !strings.Contains(note, "go(gin)") || !strings.Contains(note, "python(django)") {
		t.Fatalf("no-root-model note = %q, want sorted language pairs", note)
	}
	if !strings.Contains(note, "may be misreported as dead") {
		t.Fatalf("no-root-model note = %q, want the misreport warning", note)
	}
}

func TestCov6BuildDeadCodeAnalysis(t *testing.T) {
	t.Parallel()
	stats := DeadCodePolicyStats{ParserMetadataFrameworkRoots: 2, RootsSkippedMissingSource: 1}
	analysis := BuildDeadCodeAnalysis(nil, nil, stats)
	if analysis["framework_roots_from_parser_metadata"] != 2 {
		t.Fatalf("analysis stats = %v, want parser count 2", analysis["framework_roots_from_parser_metadata"])
	}
	if analysis["user_overrides_applied"] != false {
		t.Fatalf("no exclusions must report overrides false, got %v", analysis["user_overrides_applied"])
	}
	// The envelope always carries the static disclaimer notes.
	baseNotes, ok := analysis["notes"].([]string)
	if !ok || len(baseNotes) != 7 {
		t.Fatalf("empty analysis notes = %d entries, want 7 static disclaimers", len(baseNotes))
	}
	gin := []map[string]any{{"language": "go", "metadata": map[string]any{"framework": "gin"}}}
	withNote := BuildDeadCodeAnalysisForLanguage(gin, []string{"x"}, stats, "go")
	notes, ok := withNote["notes"].([]string)
	if !ok || len(notes) != 8 || !strings.Contains(notes[7], "no root model") {
		t.Fatalf("gin analysis notes = %d entries, want 7 disclaimers plus root-model note", len(notes))
	}
	if withNote["user_overrides_applied"] != true {
		t.Fatal("exclusions must report overrides applied")
	}
	if withNote["reflection_modeled"] != false {
		t.Fatalf("go reflection modeled = %v, want false", withNote["reflection_modeled"])
	}
	java := BuildDeadCodeAnalysisForLanguage(nil, nil, DeadCodePolicyStats{}, "java")
	if java["reflection_modeled"] != true {
		t.Fatal("java reflection must be modeled")
	}
	if deadCodeReflectionModeled("") {
		t.Fatal("empty language reflection modeled = true, want false")
	}
	if got := normalizeDeadCodeLanguage("C#"); got != "c_sharp" {
		t.Fatalf("normalizeDeadCodeLanguage(C#) = %q, want c_sharp", got)
	}
	if got := normalizeDeadCodeLanguage(" Go "); got != "go" {
		t.Fatalf("normalizeDeadCodeLanguage( Go ) = %q, want go", got)
	}
}

func TestCov6DeadCodeDowngradedAndIdentity(t *testing.T) {
	t.Parallel()
	var nilRoots DeadCodeDowngradedRoots
	if nilRoots.IsDowngraded("e1", "ruby.rails_controller_action") {
		t.Fatal("nil downgrade map must keep the root")
	}
	roots := DeadCodeDowngradedRoots{"e1": {"ruby.rails_controller_action": {}}}
	if !roots.IsDowngraded("e1", "ruby.rails_controller_action") {
		t.Fatal("recorded downgrade must report true")
	}
	if roots.IsDowngraded("e1", "ruby.other_kind") || roots.IsDowngraded("e2", "ruby.rails_controller_action") {
		t.Fatal("unrecorded downgrade must report false")
	}
	entity := &querycontract.EntityContent{Language: "go", RelativePath: "a.go"}
	if got := deadCodeEntityLanguage(map[string]any{"language": "python"}, entity); got != "go" {
		t.Fatalf("entity language = %q, want go (entity wins)", got)
	}
	if got := deadCodeEntityLanguage(map[string]any{"language": "python"}, nil); got != "python" {
		t.Fatalf("result language = %q, want python (fallback)", got)
	}
	if got := deadCodeEntityPath(nil, entity); got != "a.go" {
		t.Fatalf("entity path = %q, want a.go", got)
	}
	if got := deadCodeEntityPath(map[string]any{"file_path": "b.go"}, nil); got != "b.go" {
		t.Fatalf("result path = %q, want b.go", got)
	}
	if got := primaryEntityLabel(map[string]any{"labels": []any{"Function", "Other"}}); got != "Function" {
		t.Fatalf("primary label = %q, want Function", got)
	}
	if got := primaryEntityLabel(map[string]any{}); got != "" {
		t.Fatalf("missing labels = %q, want empty", got)
	}
}

func TestCov6DeadCodeExclusionPaths(t *testing.T) {
	t.Parallel()
	if !deadCodeIsJavaTestSourceSetPath("src/test/java/Foo.java") {
		t.Fatal("src/test source set must be a test path")
	}
	if !deadCodeIsJavaTestSourceSetPath("src/testfixtures/java/Foo.java") {
		t.Fatal("testfixtures source set must be a test path")
	}
	if deadCodeIsJavaTestSourceSetPath("src/main/java/Foo.java") {
		t.Fatal("src/main must not be a test path")
	}
	if deadCodeIsJavaTestSourceSetPath("Foo.java") {
		t.Fatal("bare file must not be a test path")
	}
	if got := stripGoComments("code // trailing"); strings.TrimSpace(got) != "code" {
		t.Fatalf("line comment strip = %q, want code", got)
	}
	if got := stripGoComments("a /* mid */ b"); !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Fatalf("block comment strip = %q, want a and b", got)
	}
	if got := stripGoComments("a /* unterminated"); !strings.HasPrefix(got, "a") {
		t.Fatalf("unterminated block strip = %q, want leading code kept", got)
	}
	if got := stripGoComments(""); got != "" {
		t.Fatalf("empty strip = %q, want empty", got)
	}
}

func TestCov6BuildSearchGraphEntitiesQuery(t *testing.T) {
	t.Parallel()
	if cypher, params := BuildSearchGraphEntitiesQuery("", "run", "", 10, false, repositoryAccessFilter{}); cypher != "" || params != nil {
		t.Fatal("empty repo must return no query")
	}
	cypher, params := BuildSearchGraphEntitiesQuery("repo-1", "run", "go", 10, true, repositoryAccessFilter{})
	if !strings.Contains(cypher, "MATCH (r:Repository") || !strings.Contains(cypher, "e.name = $query") {
		t.Fatalf("exact query = %q, want repo match with equality", cypher)
	}
	if params["repo_id"] != "repo-1" || params["language"] != "go" || params["query"] != "run" {
		t.Fatalf("exact params = %v, want repo/query/language", params)
	}
	contains, _ := BuildSearchGraphEntitiesQuery("repo-1", "run", "", 10, false, repositoryAccessFilter{})
	if !strings.Contains(contains, "CONTAINS $query") {
		t.Fatalf("fuzzy query = %q, want CONTAINS", contains)
	}
}

func TestCov6GoCallbackAndLambdaRemainders(t *testing.T) {
	t.Parallel()
	// Reconcile-shaped source reaches the ctrl request/result tail.
	policy := NewDeadCodeGoPolicyContext(
		map[string]any{"labels": []any{"Function"}, "name": "Reconcile"},
		&querycontract.EntityContent{
			Language:    "go",
			SourceCache: "func (r *R) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) { return ctrl.Result{}, nil }",
		},
	)
	if !deadCodeIsGoFrameworkCallbackRoot(map[string]any{"labels": []any{"Function"}, "name": "Reconcile"}, policy) {
		t.Fatal("controller-runtime Reconcile must be a framework callback root")
	}
	// Entity-carried lambda identity resolves through entity metadata.
	if !DeadCodeIsPythonAnonymousLambda(
		map[string]any{"name": "other"},
		&querycontract.EntityContent{EntityName: "lambda@5", Language: "python", Metadata: map[string]any{"semantic_kind": "lambda"}},
	) {
		t.Fatal("entity lambda identity with lambda kind must be a root")
	}
	if DeadCodeIsPythonAnonymousLambda(map[string]any{"name": "plain"}, cov6RootEntity("python")) {
		t.Fatal("plain name must not be an anonymous lambda")
	}
	// JavaScript express routes and TS public exports admit framework roots.
	express := map[string]any{"labels": []any{"Function"}, "metadata": map[string]any{"dead_code_root_kinds": []any{"javascript.express_route_registration"}}}
	if !DeadCodeIsJavaScriptFrameworkRoot(express, cov6RootEntity("javascript"), &DeadCodePolicyStats{}) {
		t.Fatal("express route registration must be a framework root")
	}
	exported := map[string]any{"metadata": map[string]any{"dead_code_root_kinds": []any{"typescript.public_api_export"}}}
	if !DeadCodeIsJavaScriptFrameworkRoot(exported, cov6RootEntity("typescript"), &DeadCodePolicyStats{}) {
		t.Fatal("typescript public api export must be a framework root")
	}
}

func TestCov6RerankScalarHelpers(t *testing.T) {
	t.Parallel()
	values := map[string]any{"i": 3, "f": float64(2.5), "s": "x"}
	if mapInt(values, "i") != 3 || mapInt(values, "f") != 2 || mapInt(values, "missing") != 0 || mapInt(values, "s") != 0 {
		t.Fatalf("mapInt mismatch: %v", values)
	}
	if got := resultRepoID(map[string]any{"repo_id": "r1"}, "fallback"); got != "r1" {
		t.Fatalf("resultRepoID = %q, want r1", got)
	}
	if got := resultRepoID(map[string]any{}, "fallback"); got != "fallback" {
		t.Fatalf("resultRepoID fallback = %q, want fallback", got)
	}
}
