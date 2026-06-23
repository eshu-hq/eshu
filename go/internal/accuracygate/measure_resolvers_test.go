package accuracygate_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/accuracygate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/goldenaudit"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// resolverCoveredLanguages is the published cross-repo call-resolver coverage
// set from issue #3487's matrix (go/internal/reducer/README.md). A dedicated
// resolver requires the parser to emit receiver-type or structured-import
// evidence; c, cpp, csharp, and scala are documented parser-capability gaps and
// are intentionally excluded. The gate counts this published set so a silent
// removal of a resolver drops coverage below the floor.
//
// This list is the published matrix, not a measurement, and is asserted against
// the reducer README in TestAccuracyResolverMatrixMatchesPublishedDoc so it
// cannot drift from the documented coverage.
func resolverCoveredLanguages() []string {
	return []string{
		"dart", "elixir", "go", "groovy", "haskell", "java", "javascript",
		"jsx", "kotlin", "perl", "python", "rust", "swift", "tsx", "typescript",
	}
}

// resolverCallFixture is a single-file caller->callee CALLS fixture. The callee
// is defined in the same file so same-file scope resolution binds the edge
// without needing per-language cross-repo evidence, letting the gate measure the
// real reducer call-edge path across several languages deterministically.
type resolverCallFixture struct {
	language     string
	fileName     string
	source       string
	caller       string
	callee       string
	callerEntity string
	calleeEntity string
}

func resolverCallFixtures() []resolverCallFixture {
	return []resolverCallFixture{
		{
			language: "go", fileName: "calls.go", caller: "Caller", callee: "callee",
			callerEntity: "content-entity:go:Caller", calleeEntity: "content-entity:go:callee",
			source: "package p\n\nfunc callee() int { return 1 }\n\nfunc Caller() int {\n\treturn callee()\n}\n",
		},
		{
			language: "python", fileName: "calls.py", caller: "caller", callee: "callee",
			callerEntity: "content-entity:python:caller", calleeEntity: "content-entity:python:callee",
			source: "def callee():\n    return 1\n\ndef caller():\n    return callee()\n",
		},
		{
			language: "typescript", fileName: "calls.ts", caller: "caller", callee: "callee",
			callerEntity: "content-entity:typescript:caller", calleeEntity: "content-entity:typescript:callee",
			source: "function callee(): number { return 1; }\n\nfunction caller(): number {\n  return callee();\n}\n",
		},
	}
}

// measureResolvers measures cross-repo call-resolver accuracy on two axes:
//   - precision/recall of observed CALLS edges against the golden caller->callee
//     edge, scored with the real reducer extraction and goldenaudit.ScoreAccuracy
//     (the same scorer resolutionparity's harness uses), and
//   - resolver language coverage from the published #3487 matrix.
//
// CoveredItems carries the resolver-covered language count so a dropped resolver
// fails the gate even when the sampled edges still resolve.
func measureResolvers(t *testing.T, engine *parser.Engine) accuracygate.Metric {
	t.Helper()

	var expectedEdges, observedEdges []goldenaudit.Edge
	labels := make(map[string]string)
	for _, fixture := range resolverCallFixtures() {
		expected := goldenaudit.Edge{SourceID: fixture.callerEntity, TargetID: fixture.calleeEntity, Type: "CALLS"}
		expectedEdges = append(expectedEdges, expected)

		observed := observeResolverEdges(t, engine, fixture)
		observedEdges = append(observedEdges, observed...)
		labels[fixture.language] = resolverEdgeStatus(expected, observed)
	}

	result := goldenaudit.ScoreAccuracy(
		goldenaudit.Graph{Edges: expectedEdges},
		goldenaudit.Graph{Edges: observedEdges},
	)
	metric := accuracygate.Metric{
		Precision:    result.Overall.Precision,
		Recall:       result.Overall.Recall,
		CoveredItems: len(resolverCoveredLanguages()),
		Labels:       labels,
	}
	for _, language := range resolverCoveredLanguages() {
		labels["covered:"+language] = "resolver"
	}
	return metric
}

// observeResolverEdges parses one fixture, assigns stable UIDs to the caller and
// callee functions, runs the real reducer call-row extraction, and returns the
// observed CALLS edges.
func observeResolverEdges(t *testing.T, engine *parser.Engine, fixture resolverCallFixture) []goldenaudit.Edge {
	t.Helper()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, fixture.fileName)
	writeFixtureFile(t, filePath, fixture.source)

	repoID := "accuracygate-resolver-" + fixture.language
	paths := []string{filePath}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, paths)
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v", err)
	}
	envelopes := []facts.Envelope{{
		FactKind: "repository",
		Payload:  map[string]any{"repo_id": repoID, "imports_map": importsMap},
	}}

	parsed, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v", err)
	}
	assignResolverUIDs(parsed, map[string]string{
		fixture.caller: fixture.callerEntity,
		fixture.callee: fixture.calleeEntity,
	})
	envelopes = append(envelopes, facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repoID,
			"relative_path":    fixture.fileName,
			"parsed_file_data": parsed,
		},
	})

	_, rows := reducer.ExtractCodeCallRows(envelopes)
	return edgesFromRows(rows)
}

// assignResolverUIDs stamps stable content-entity UIDs onto the caller and
// callee function items so the reducer emits resolvable caller/callee entity ids.
func assignResolverUIDs(parsed map[string]any, uidByName map[string]string) {
	functions, ok := parsed["functions"].([]map[string]any)
	if !ok {
		return
	}
	for _, function := range functions {
		name, _ := function["name"].(string)
		if uid, ok := uidByName[name]; ok {
			function["uid"] = uid
		}
	}
}

// edgesFromRows converts reducer call rows into golden-audit CALLS edges,
// dropping rows that did not resolve to both a caller and callee entity.
func edgesFromRows(rows []map[string]any) []goldenaudit.Edge {
	edges := make([]goldenaudit.Edge, 0, len(rows))
	for _, row := range rows {
		source, _ := row["caller_entity_id"].(string)
		target, _ := row["callee_entity_id"].(string)
		relType, _ := row["relationship_type"].(string)
		relType = strings.TrimSpace(relType)
		if relType == "" {
			relType = "CALLS"
		}
		if source == "" || target == "" {
			continue
		}
		edges = append(edges, goldenaudit.Edge{SourceID: source, TargetID: target, Type: relType})
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].Key() < edges[j].Key() })
	return edges
}

// resolverEdgeStatus renders a short per-language detail for the published
// report: whether the golden edge was observed.
func resolverEdgeStatus(expected goldenaudit.Edge, observed []goldenaudit.Edge) string {
	for _, edge := range observed {
		if edge.Key() == expected.Key() {
			return "resolved:" + expected.Key()
		}
	}
	return "unresolved:" + expected.Key()
}

func writeFixtureFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir error = %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(body, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture %q error = %v", path, err)
	}
}
