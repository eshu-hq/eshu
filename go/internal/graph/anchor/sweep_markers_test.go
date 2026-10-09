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
// The marker sits within markerReach lines above the literal's first line and
// excuses the nearest template below it, one marker per template. The named test
// must exist in a _test.go file in the marker's own directory, so the owning
// package's own tests run the proof, and it is the proof that the labels the
// writer can use are anchor labels. The sweep fails on a dynamic-label writer
// with no marker of its own, on a marker that names no test in its directory,
// and on a marker with no template of its own under it, so the annotation moves
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

// definedTests returns, for each Test function in the _test.go files under
// root, the set of directories (relative to root) that define it.
func definedTests(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	out := make(map[string]map[string]bool)
	walkGoFiles(t, root, true, func(rel, path string) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range testFuncLine.FindAllStringSubmatch(string(raw), -1) {
			if out[m[1]] == nil {
				out[m[1]] = make(map[string]bool)
			}
			out[m[1]][filepath.Dir(rel)] = true
		}
	})
	return out
}

// markerProblems checks the dynamic-label sites against the markers and the
// defined tests. Each marker excuses the nearest template below it and nothing
// else: a marker claims at most one site, a site is paired with the nearest
// marker above it that claims it, and a marker whose nearest template is out of
// reach, or already paired with a nearer marker, is stale. The named proof must
// be a test defined in the marker's own directory. It returns one line per
// problem.
func markerProblems(sites []cypherSite, markers []marker, tests map[string]map[string]bool) []string {
	var problems []string
	claimed := make(map[int][]int) // site index -> marker indexes that claim it
	for i, m := range markers {
		nearest := -1
		for j, site := range sites {
			if site.File != m.File || site.Line <= m.Line {
				continue
			}
			if nearest < 0 || site.Line < sites[nearest].Line {
				nearest = j
			}
		}
		if nearest >= 0 && sites[nearest].Line-m.Line <= markerReach {
			claimed[nearest] = append(claimed[nearest], i)
			continue
		}
		problems = append(problems, m.File+":"+strconv.Itoa(m.Line)+": marker has no dynamic-label writer within "+strconv.Itoa(markerReach)+
			" lines below it (stale marker)")
	}
	for j, site := range sites {
		claimers := claimed[j]
		if len(claimers) == 0 {
			problems = append(problems, site.Key+": dynamic-label writer has no marker of its own within "+strconv.Itoa(markerReach)+
				" lines above it; add `// anchor-census: dynamic-label writer; label set bounded by <TestName>` with a test in the same directory that proves the labels are anchor labels")
			continue
		}
		// The nearest marker pairs with the site; any further claimer is shared.
		for _, i := range claimers[:len(claimers)-1] {
			m := markers[i]
			problems = append(problems, m.File+":"+strconv.Itoa(m.Line)+": marker shares its writer with a nearer marker (stale marker)")
		}
	}
	for _, m := range markers {
		dirs, defined := tests[m.Test]
		switch {
		case !defined:
			problems = append(problems, m.File+":"+strconv.Itoa(m.Line)+": marker names "+m.Test+", which no _test.go file defines")
		case !dirs[filepath.Dir(m.File)]:
			problems = append(problems, m.File+":"+strconv.Itoa(m.Line)+": marker names "+m.Test+", which is not defined in the marker's own directory")
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

// TestOneMarkerExcusesOnlyTheNearestWriter is the seeded violation for marker
// reuse: a second template placed under an existing marker has no marker of its
// own and is reported, so a new writer cannot hide behind a neighbour's proof.
func TestOneMarkerExcusesOnlyTheNearestWriter(t *testing.T) {
	second := "const plantedSecond = `UNWIND $rows AS row MERGE (m:%s {uid: row.uid}) SET m.id = row.uid`\n"
	root := plantedTree(t, "package planted\n\n"+plantedMarker+plantedTemplate+"\n"+second, plantedTest)
	problems := problemsFor(t, root)
	if len(problems) != 1 || !strings.Contains(problems[0], "has no marker of its own") || !strings.Contains(problems[0], "planted.go:6") {
		t.Fatalf("problems = %v, want the second template reported", problems)
	}
	// Two markers over one template: the farther marker is shared and reported.
	two := plantedTree(t, "package planted\n\n"+plantedMarker+plantedMarker+plantedTemplate, plantedTest)
	if problems := problemsFor(t, two); len(problems) != 1 || !strings.Contains(problems[0], "shares its writer") {
		t.Fatalf("problems = %v, want the shared marker reported", problems)
	}
}

// TestMarkerProofMustLiveInTheMarkersDirectory: a test of the same name in
// another directory does not prove the writer.
func TestMarkerProofMustLiveInTheMarkersDirectory(t *testing.T) {
	root := plantedTree(t, "package planted\n\n"+plantedMarker+plantedTemplate, "")
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other", "proof_test.go"), []byte(plantedTest), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := problemsFor(t, root); len(problems) != 1 || !strings.Contains(problems[0], "not defined in the marker's own directory") {
		t.Fatalf("problems = %v, want the foreign proof reported", problems)
	}
}
