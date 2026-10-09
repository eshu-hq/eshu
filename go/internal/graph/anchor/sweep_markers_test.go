// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A Cypher template whose node label is chosen at run time cannot be decided by
// the static sweep. Each one carries a marker comment beside it, in the file
// that owns the template:
//
//	// anchor-census: dynamic-label writer; label set bounded by TestSomething
//
// The marker sits within markerReach lines above the literal's first line. The
// named test must exist in a _test.go file under the scanned root, and it is the
// proof that the labels the writer can use are anchor labels. The sweep fails on
// a dynamic-label writer with no marker, on a marker that names no existing test,
// and on a marker with no dynamic-label writer under it, so the annotation moves
// with the code it excuses and nothing is listed by hand elsewhere.
const markerReach = 10

var (
	markerLine   = regexp.MustCompile(`^\s*//\s*anchor-census:\s*dynamic-label writer;\s*label set bounded by\s+(Test\w+)\s*$`)
	testFuncLine = regexp.MustCompile(`(?m)^func (Test\w+)\(`)
)

// marker is one annotation found in a source file.
type marker struct {
	File string
	Line int
	Test string
}

// scanMarkers returns the markers in the non-test Go files under root.
func scanMarkers(t *testing.T, root string) []marker {
	t.Helper()
	var out []marker
	walkGoFiles(t, root, false, func(rel, path string) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if m := markerLine.FindStringSubmatch(line); m != nil {
				out = append(out, marker{File: rel, Line: i + 1, Test: m[1]})
			}
		}
	})
	return out
}

// definedTests returns the names of the Test functions in the _test.go files
// under root.
func definedTests(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := make(map[string]bool)
	walkGoFiles(t, root, true, func(_, path string) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range testFuncLine.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = true
		}
	})
	return out
}

// markerProblems checks the dynamic-label sites against the markers and the
// defined tests. It returns one line per problem.
func markerProblems(sites []cypherSite, markers []marker, tests map[string]bool) []string {
	var problems []string
	paired := make(map[int]bool)
	for _, site := range sites {
		match := -1
		for i, m := range markers {
			if m.File == site.File && m.Line < site.Line && site.Line-m.Line <= markerReach {
				match = i
			}
		}
		if match < 0 {
			problems = append(problems, site.Key+": dynamic-label writer has no marker within "+strconv.Itoa(markerReach)+
				" lines above it; add `// anchor-census: dynamic-label writer; label set bounded by <TestName>` with a test that proves the labels are anchor labels")
			continue
		}
		paired[match] = true
	}
	for i, m := range markers {
		if !tests[m.Test] {
			problems = append(problems, m.File+":"+strconv.Itoa(m.Line)+": marker names "+m.Test+", which no _test.go file defines")
		}
		if !paired[i] {
			problems = append(problems, m.File+":"+strconv.Itoa(m.Line)+": marker has no dynamic-label writer within "+strconv.Itoa(markerReach)+
				" lines below it (stale marker)")
		}
	}
	sort.Strings(problems)
	return problems
}

func TestEveryDynamicLabelWriterIsMarkedWithAProof(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	result := sweepSource(t, root, Labels())
	if len(result.Dynamic) < 10 {
		t.Fatalf("static sweep found %d dynamic-label writers; the sweep is not reading go/", len(result.Dynamic))
	}
	problems := markerProblems(result.Dynamic, scanMarkers(t, root), definedTests(t, root))
	if len(problems) > 0 {
		t.Fatalf("%d dynamic-label writer problem(s):\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// plantedTree writes a scratch module root with a Go source file and, when
// testSource is not empty, a test file.
func plantedTree(t *testing.T, source, testSource string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "planted.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if testSource != "" {
		if err := os.WriteFile(filepath.Join(root, "planted_test.go"), []byte(testSource), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const (
	plantedTemplate = "const plantedTemplate = `UNWIND $rows AS row MERGE (n:%s {uid: row.uid}) SET n += row.props`\n"
	plantedMarker   = "// anchor-census: dynamic-label writer; label set bounded by TestPlantedLabelsAreAnchorLabels\n"
	plantedTest     = "package planted\n\nimport \"testing\"\n\nfunc TestPlantedLabelsAreAnchorLabels(t *testing.T) {}\n"
)

func problemsFor(t *testing.T, root string) []string {
	t.Helper()
	result := sweepSource(t, root, Labels())
	return markerProblems(result.Dynamic, scanMarkers(t, root), definedTests(t, root))
}

// TestDynamicLabelWriterWithoutAMarkerFails is the seeded violation: a new
// template writer with no marker is reported, and the same writer with a marker
// naming an existing test is clean.
func TestDynamicLabelWriterWithoutAMarkerFails(t *testing.T) {
	bare := plantedTree(t, "package planted\n\n"+plantedTemplate, plantedTest)
	if problems := problemsFor(t, bare); len(problems) != 1 || !strings.Contains(problems[0], "has no marker") {
		t.Fatalf("problems = %v, want one missing-marker report", problems)
	}
	marked := plantedTree(t, "package planted\n\n"+plantedMarker+plantedTemplate, plantedTest)
	if problems := problemsFor(t, marked); len(problems) != 0 {
		t.Fatalf("a marked writer reported: %v", problems)
	}
}

func TestMarkerNamingNoExistingTestFails(t *testing.T) {
	root := plantedTree(t, "package planted\n\n"+plantedMarker+plantedTemplate, "package planted\n")
	if problems := problemsFor(t, root); len(problems) != 1 || !strings.Contains(problems[0], "which no _test.go file defines") {
		t.Fatalf("problems = %v, want the unknown proof reported", problems)
	}
}

func TestMarkerBesideNoDynamicWriterFails(t *testing.T) {
	root := plantedTree(t, "package planted\n\n"+plantedMarker+"const unrelated = `MATCH (n:Function) RETURN n`\n", plantedTest)
	if problems := problemsFor(t, root); len(problems) != 1 || !strings.Contains(problems[0], "stale marker") {
		t.Fatalf("problems = %v, want a stale marker", problems)
	}
	far := plantedTree(t, "package planted\n\n"+plantedMarker+strings.Repeat("// filler\n", markerReach)+plantedTemplate, plantedTest)
	if problems := problemsFor(t, far); len(problems) != 2 {
		t.Fatalf("problems = %v, want a missing marker and a stale marker for a marker out of reach", problems)
	}
}
