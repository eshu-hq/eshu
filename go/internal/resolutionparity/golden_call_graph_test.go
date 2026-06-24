package resolutionparity

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/goldenaudit"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestGoldenCallGraphCorrectnessHarness(t *testing.T) {
	t.Parallel()

	fixtures := append(
		sourceCallGraphFixtures,
		dartImportBindingCallGraphFixture(),
		importBindingCallGraphFixture(),
		elixirImportBindingCallGraphFixture(),
		groovyClassQualifiedCallGraphFixture(),
		haskellImportBindingCallGraphFixture(),
		javaImportBindingCallGraphFixture(),
		typeScriptImportBindingCallGraphFixture(),
	)
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.language, func(t *testing.T) {
			t.Parallel()
			if reason, ok := sourceCallGraphFixtureGaps[fixture.language]; ok {
				t.Skipf("source-derived correctness golden gap: %s", reason)
			}

			expected := goldenCallGraphForFixture(fixture)
			observed, methods := observeSourceCallGraph(t, fixture)
			result := goldenaudit.ScoreAccuracy(expected, observed)
			if ok, msg := result.MeetsThreshold(1.0, 1.0); !ok {
				t.Fatalf("call graph accuracy failed: %s\n%s", result.Summary(), msg)
			}
			assertGoldenCallGraphMethods(t, expected.Edges, fixture.method, methods)
		})
	}
}

func TestGoldenCallGraphSCIPTierFixture(t *testing.T) {
	t.Parallel()

	expected := goldenaudit.Graph{
		Edges: []goldenaudit.Edge{
			{SourceID: "content-entity:scip-caller", TargetID: "content-entity:scip-callee", Type: "CALLS"},
		},
	}
	_, rows := reducer.ExtractCodeCallRows([]facts.Envelope{
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-scip-golden",
			"relative_path": "caller.py",
			"parsed_file_data": map[string]any{
				"path": "/repo/scip/caller.py",
				"functions": []any{
					map[string]any{"name": "caller", "line_number": 1, "end_line": 4, "uid": "content-entity:scip-caller"},
				},
				"function_calls_scip": []any{
					map[string]any{
						"caller_file": "/repo/scip/caller.py",
						"caller_line": 1,
						"callee_file": "/repo/scip/callee.py",
						"callee_line": 1,
						"ref_line":    2,
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-scip-golden",
			"relative_path": "callee.py",
			"parsed_file_data": map[string]any{
				"path": "/repo/scip/callee.py",
				"functions": []any{
					map[string]any{"name": "callee", "line_number": 1, "end_line": 3, "uid": "content-entity:scip-callee"},
				},
			},
		}},
	})
	observed, methods := graphFromCodeCallRows(rows)
	result := goldenaudit.ScoreAccuracy(expected, observed)
	if ok, msg := result.MeetsThreshold(1.0, 1.0); !ok {
		t.Fatalf("SCIP call graph accuracy failed: %s\n%s", result.Summary(), msg)
	}
	assertGoldenCallGraphMethods(t, expected.Edges, codeprovenance.MethodSCIP, methods)
}

func TestGoldenCallGraphRejectsWrongTargetWithSameTier(t *testing.T) {
	t.Parallel()

	expected := goldenaudit.Graph{
		Edges: []goldenaudit.Edge{
			{SourceID: "content-entity:caller", TargetID: "content-entity:callee", Type: "CALLS"},
		},
	}
	observed := goldenaudit.Graph{
		Edges: []goldenaudit.Edge{
			{SourceID: "content-entity:caller", TargetID: "content-entity:wrong-callee", Type: "CALLS"},
		},
	}
	methods := map[string]codeprovenance.Method{
		observed.Edges[0].Key(): codeprovenance.MethodSameFile,
	}

	result := goldenaudit.ScoreAccuracy(expected, observed)
	if ok, _ := result.MeetsThreshold(1.0, 1.0); ok {
		t.Fatalf("wrong-target edge passed accuracy gate with same resolution tier: %s", result.Summary())
	}
	if got := methods[observed.Edges[0].Key()]; got != codeprovenance.MethodSameFile {
		t.Fatalf("wrong-target method = %q, want %q", got, codeprovenance.MethodSameFile)
	}
}

func TestGoldenCallGraphFixturesCoverRegisteredSourceLanguages(t *testing.T) {
	t.Parallel()

	covered := map[string]struct{}{}
	for _, fixture := range sourceCallGraphFixtures {
		covered[fixture.language] = struct{}{}
	}
	var missing []string
	for _, language := range callGraphSourceLanguages() {
		_, hasFixture := covered[language]
		_, hasGap := sourceCallGraphFixtureGaps[language]
		if !hasFixture && !hasGap {
			missing = append(missing, language)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("missing source-derived call-graph golden fixture or explicit gap for parser languages: %v", missing)
	}
}

func TestGoldenCallGraphFixtureGapsDoNotShadowFixtures(t *testing.T) {
	t.Parallel()

	covered := map[string]struct{}{}
	for _, fixture := range sourceCallGraphFixtures {
		covered[fixture.language] = struct{}{}
	}

	var shadowed []string
	for language := range sourceCallGraphFixtureGaps {
		if _, ok := covered[language]; ok {
			shadowed = append(shadowed, language)
		}
	}
	sort.Strings(shadowed)
	if len(shadowed) > 0 {
		t.Fatalf("source-derived call-graph fixture gaps shadow active fixtures: %v", shadowed)
	}
}

func observeSourceCallGraph(t *testing.T, fixture goldenCallGraphFixture) (goldenaudit.Graph, map[string]codeprovenance.Method) {
	t.Helper()

	repoRoot := t.TempDir()
	var paths []string
	for relativePath, source := range fixture.files {
		path := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir fixture dir error = %v", err)
		}
		if err := os.WriteFile(path, []byte(strings.TrimLeft(source, "\n")), 0o644); err != nil {
			t.Fatalf("write fixture file %q error = %v", relativePath, err)
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, paths)
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v", err)
	}

	repoID := "resolutionparity-golden-" + fixture.language
	envelopes := []facts.Envelope{{
		FactKind: "repository",
		Payload: map[string]any{
			"repo_id":     repoID,
			"imports_map": importsMap,
		},
	}}
	uidByName := map[string]string{
		fixture.caller: "content-entity:" + fixture.language + ":" + fixture.caller,
		fixture.callee: "content-entity:" + fixture.language + ":" + fixture.callee,
	}
	for _, path := range paths {
		parsed, err := engine.ParsePath(repoRoot, path, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v", path, err)
		}
		relativePath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("Rel(%q) error = %v", path, err)
		}
		assignGoldenCallGraphUIDs(parsed, uidByName, fixture.uidByPath, filepath.ToSlash(relativePath))
		envelopes = append(envelopes, facts.Envelope{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          repoID,
				"relative_path":    filepath.ToSlash(relativePath),
				"parsed_file_data": parsed,
			},
		})
	}

	_, rows := reducer.ExtractCodeCallRows(envelopes)
	return graphFromCodeCallRows(rows)
}

func assignGoldenCallGraphUIDs(
	parsed map[string]any,
	uidByName map[string]string,
	uidByPath map[string]string,
	relativePath string,
) {
	for _, bucket := range entityBuckets {
		assignGoldenCallGraphBucketUIDs(parsed[bucket], uidByName, uidByPath, relativePath)
	}
}

func assignGoldenCallGraphBucketUIDs(bucket any, uidByName map[string]string, uidByPath map[string]string, relativePath string) {
	switch typed := bucket.(type) {
	case []map[string]any:
		for _, item := range typed {
			assignGoldenCallGraphItemUID(item, uidByName, uidByPath, relativePath)
		}
	case []any:
		for _, item := range typed {
			if asMap, ok := item.(map[string]any); ok {
				assignGoldenCallGraphItemUID(asMap, uidByName, uidByPath, relativePath)
			}
		}
	}
}

func assignGoldenCallGraphItemUID(item map[string]any, uidByName map[string]string, uidByPath map[string]string, relativePath string) {
	name, _ := item["name"].(string)
	if uid, ok := uidByPath[relativePath+":"+name]; ok {
		item["uid"] = uid
		return
	}
	if uid, ok := uidByName[name]; ok {
		item["uid"] = uid
	}
}

func goldenCallGraphForFixture(fixture goldenCallGraphFixture) goldenaudit.Graph {
	return goldenaudit.Graph{
		Edges: []goldenaudit.Edge{
			{
				SourceID: "content-entity:" + fixture.language + ":" + fixture.caller,
				TargetID: "content-entity:" + fixture.language + ":" + fixture.callee,
				Type:     "CALLS",
			},
		},
	}
}

func graphFromCodeCallRows(rows []map[string]any) (goldenaudit.Graph, map[string]codeprovenance.Method) {
	graph := goldenaudit.Graph{Edges: make([]goldenaudit.Edge, 0, len(rows))}
	methods := make(map[string]codeprovenance.Method, len(rows))
	for _, row := range rows {
		edge := goldenaudit.Edge{
			SourceID: stringField(row, "caller_entity_id"),
			TargetID: stringField(row, "callee_entity_id"),
			Type:     strings.TrimSpace(stringField(row, "relationship_type")),
		}
		if edge.Type == "" {
			edge.Type = "CALLS"
		}
		if edge.SourceID == "" || edge.TargetID == "" {
			continue
		}
		graph.Edges = append(graph.Edges, edge)
		methods[edge.Key()] = codeprovenance.Method(stringField(row, "resolution_method"))
	}
	sort.Slice(graph.Edges, func(i, j int) bool {
		return graph.Edges[i].Key() < graph.Edges[j].Key()
	})
	return graph, methods
}

func assertGoldenCallGraphMethods(
	t *testing.T,
	edges []goldenaudit.Edge,
	want codeprovenance.Method,
	methods map[string]codeprovenance.Method,
) {
	t.Helper()

	for _, edge := range edges {
		got := methods[edge.Key()]
		if got != want {
			t.Fatalf("edge %s resolution_method = %q, want %q", edge.Key(), got, want)
		}
	}
}

func callGraphSourceLanguages() []string {
	registry := parser.DefaultRegistry()
	sourceLanguages := map[string]struct{}{}
	for _, definition := range registry.Definitions() {
		if !callGraphParserKey(definition.ParserKey) {
			continue
		}
		sourceLanguages[definition.Language] = struct{}{}
	}
	languages := make([]string, 0, len(sourceLanguages))
	for language := range sourceLanguages {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	return languages
}

func callGraphParserKey(parserKey string) bool {
	switch parserKey {
	case "c", "c_sharp", "cpp", "dart", "elixir", "go", "groovy", "haskell",
		"java", "javascript", "kotlin", "perl", "php", "python", "ruby",
		"rust", "scala", "swift", "tsx", "typescript":
		return true
	default:
		return false
	}
}

func stringField(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return value
}
