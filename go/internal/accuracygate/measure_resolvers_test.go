// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accuracygate_test

import (
	"fmt"
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
// set from issue #3487's matrix (go/internal/reducer/README.md). It names the
// languages the coverage measurement MUST exercise; c, cpp, csharp, and scala are
// documented parser-capability gaps and are intentionally excluded. The set is
// asserted against the reducer README in TestAccuracyResolverMatrixMatchesPublishedDoc
// so it cannot drift from the documented coverage.
//
// This list is the documented scope, not the measurement. The gate's covered
// count comes from measureResolvers running each language's resolver over a
// per-language fixture and counting only languages whose resolver actually
// produces the expected CALLS edge — so removing a resolver drops the measured
// count below the floor even though this list is unchanged.
func resolverCoveredLanguages() []string {
	languages := make([]string, 0, len(resolverCoverageFixtures()))
	for _, fixture := range resolverCoverageFixtures() {
		languages = append(languages, fixture.language)
	}
	return languages
}

// resolverCallFixture is a single-file caller->callee CALLS fixture parsed by the
// real engine. The callee is defined in the same file so same-file scope
// resolution binds the edge without per-language cross-repo evidence, letting the
// gate measure the real reducer call-edge path across several languages
// deterministically for the precision/recall axis.
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
//   - resolver language coverage MEASURED per language: each documented resolver
//     language is exercised with a fixture that drives its resolver, and a
//     language is counted covered only when its resolver actually produces the
//     expected CALLS edge.
//
// CoveredItems carries the measured covered-language count, not a documented
// constant, so removing any one resolver drops the count below the floor and
// fails the gate even when the sampled precision/recall edges still resolve.
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

	covered := measureResolverCoverage(t)
	for language, status := range covered {
		labels["covered:"+language] = status
	}

	return accuracygate.Metric{
		Precision:    result.Overall.Precision,
		Recall:       result.Overall.Recall,
		CoveredItems: countCoveredResolvers(covered),
		Labels:       labels,
	}
}

// measureResolverCoverage runs every documented resolver language's fixture
// through the real reducer call-row extraction and returns, per language, whether
// its resolver produced the expected CALLS edge. The returned status is "resolver"
// for a covered language and "uncovered" otherwise, so the published report shows
// exactly which resolver, if any, stopped firing.
func measureResolverCoverage(t *testing.T) map[string]string {
	t.Helper()

	statuses := make(map[string]string)
	for _, fixture := range resolverCoverageFixtures() {
		_, rows := reducer.ExtractCodeCallRows(fixture.envelopes)
		if resolverFixtureEdgeObserved(rows, fixture) {
			statuses[fixture.language] = "resolver"
			continue
		}
		statuses[fixture.language] = "uncovered"
	}
	return statuses
}

// measureResolverCoverageWithBrokenLanguage runs the coverage measurement but
// strips the named language's resolver firing signal (the inferred receiver type
// and imports the resolver keys on) from its caller envelope, so that resolver
// can no longer bind. It models removing the resolver: the language drops to
// "uncovered" and the measured count falls. Other languages are unchanged.
func measureResolverCoverageWithBrokenLanguage(t *testing.T, language string) map[string]string {
	t.Helper()

	statuses := make(map[string]string)
	for _, fixture := range resolverCoverageFixtures() {
		envelopes := fixture.envelopes
		if fixture.language == language {
			envelopes = stripResolverSignal(envelopes)
		}
		_, rows := reducer.ExtractCodeCallRows(envelopes)
		if resolverFixtureEdgeObserved(rows, fixture) {
			statuses[fixture.language] = "resolver"
			continue
		}
		statuses[fixture.language] = "uncovered"
	}
	return statuses
}

// stripResolverSignal removes the inferred receiver type, imports, and repository
// imports_map that language-specific resolvers bind on, leaving a bare call name
// that only the shared repo-unique-name fallback could match. It returns copies so
// the shared fixture envelopes are not mutated.
func stripResolverSignal(envelopes []facts.Envelope) []facts.Envelope {
	stripped := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		payload := make(map[string]any, len(envelope.Payload))
		for key, value := range envelope.Payload {
			payload[key] = value
		}
		delete(payload, "imports_map")
		if parsed, ok := payload["parsed_file_data"].(map[string]any); ok {
			next := make(map[string]any, len(parsed))
			for key, value := range parsed {
				next[key] = value
			}
			delete(next, "imports")
			if calls, ok := next["function_calls"].([]any); ok {
				next["function_calls"] = callsWithoutReceiverType(calls)
			}
			payload["parsed_file_data"] = next
		}
		stripped = append(stripped, facts.Envelope{FactKind: envelope.FactKind, Payload: payload})
	}
	return stripped
}

// callsWithoutReceiverType returns copies of the call items with the inferred
// receiver type removed so receiver-typed resolvers cannot bind.
func callsWithoutReceiverType(calls []any) []any {
	out := make([]any, 0, len(calls))
	for _, raw := range calls {
		call, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		next := make(map[string]any, len(call))
		for key, value := range call {
			next[key] = value
		}
		delete(next, "inferred_obj_type")
		out = append(out, next)
	}
	return out
}

// countCoveredResolvers counts the languages whose resolver produced the expected
// edge. This measured count is the gate's resolver CoveredItems.
func countCoveredResolvers(statuses map[string]string) int {
	covered := 0
	for _, status := range statuses {
		if status == "resolver" {
			covered++
		}
	}
	return covered
}

// resolverFixtureEdgeObserved reports whether the reducer call rows contain the
// fixture's expected caller->callee CALLS edge, optionally requiring a specific
// resolution method so a same-name fallback match does not count as resolver
// coverage.
func resolverFixtureEdgeObserved(rows []map[string]any, fixture resolverCoverageFixture) bool {
	for _, row := range rows {
		caller, _ := row["caller_entity_id"].(string)
		callee, _ := row["callee_entity_id"].(string)
		if caller != fixture.callerUID || callee != fixture.calleeUID {
			continue
		}
		if fixture.resolutionMethod == "" {
			return true
		}
		// resolution_method is stored as codeprovenance.Method (a string type),
		// so compare through fmt.Sprint rather than a string type assertion.
		return fmt.Sprint(row["resolution_method"]) == fixture.resolutionMethod
	}
	return false
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
