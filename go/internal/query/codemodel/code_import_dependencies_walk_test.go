// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// walkEdge builds one Python import edge from file `from` to the module named
// by file `to`, the shape enumerateImportCycles collapses into a file digraph.
func walkEdge(from, to string) importCycleEdge {
	return importCycleEdge{
		repoID:       "repo-1",
		repoName:     "repo",
		sourcePath:   "/repo/" + from + ".py",
		sourceFile:   from + ".py",
		sourceModule: from,
		language:     "python",
		targetModule: to,
		lineNumber:   1,
	}
}

// layeredDAG returns a layered digraph with `width` files per layer and every
// file importing every file of the next layer, so the number of distinct paths
// grows as width^layers while the graph has no cycle at all. Last-layer files
// import an external module so they still exist as nodes.
func layeredDAG(layers, width int) []importCycleEdge {
	var edges []importCycleEdge
	name := func(layer, index int) string { return fmt.Sprintf("l%02d_%d", layer, index) }
	for layer := 0; layer < layers; layer++ {
		for from := 0; from < width; from++ {
			if layer == layers-1 {
				edges = append(edges, walkEdge(name(layer, from), "external_module"))
				continue
			}
			for to := 0; to < width; to++ {
				edges = append(edges, walkEdge(name(layer, from), name(layer+1, to)))
			}
		}
	}
	return edges
}

// completeDigraph returns every ordered pair of n files, a single dense strongly
// connected component whose simple-cycle count grows factorially with n.
func completeDigraph(n int) []importCycleEdge {
	var edges []importCycleEdge
	for from := 0; from < n; from++ {
		for to := 0; to < n; to++ {
			if from != to {
				edges = append(edges, walkEdge(fmt.Sprintf("n%02d", from), fmt.Sprintf("n%02d", to)))
			}
		}
	}
	return edges
}

// TestEnumerateImportCyclesDoesNoWalkOnAnAcyclicGraph pins the root-cause fix for
// the walk bound. importCycleEnumerationCap counts closed cycles only, so a dense
// DAG, which closes none, never trips it while a plain depth-first walk from every
// file still visits every path. Every simple cycle lies inside one strongly
// connected component, so a graph with no component larger than one node needs no
// walk at all.
func TestEnumerateImportCyclesDoesNoWalkOnAnAcyclicGraph(t *testing.T) {
	t.Parallel()

	// width^layers paths: 4^12 is about 16.7 million, far past any budget.
	cycles, enumeration := enumerateImportCycles(layeredDAG(12, 4), 8)

	if len(cycles) != 0 {
		t.Fatalf("len(cycles) = %d, want 0 for an acyclic graph", len(cycles))
	}
	if enumeration.StepsExamined != 0 {
		t.Fatalf("StepsExamined = %d, want 0: an acyclic graph has no strongly connected component to walk", enumeration.StepsExamined)
	}
	if enumeration.Truncated || enumeration.StopReason != CycleStopNone {
		t.Fatalf("enumeration = %+v, want an untruncated run with stop reason %q", enumeration, CycleStopNone)
	}
}

// TestEnumerateImportCyclesStopsAtTheStepBudget proves the budget stops a dense
// component's walk deterministically and says why. The same input under the same
// budget must produce the same stop, since a request must not answer differently
// on a slower host.
func TestEnumerateImportCyclesStopsAtTheStepBudget(t *testing.T) {
	t.Parallel()

	// K6 has 409 simple cycles, under the 1,000-cycle cap, so only the step
	// budget can stop it early.
	edges := completeDigraph(6)
	const budget = 200

	cycles, enumeration := enumerateImportCyclesWithinBudget(edges, 6, budget)
	if !enumeration.Truncated || enumeration.StopReason != CycleStopStepBudget {
		t.Fatalf("enumeration = %+v, want Truncated with stop reason %q", enumeration, CycleStopStepBudget)
	}
	if enumeration.StepsExamined != budget {
		t.Fatalf("StepsExamined = %d, want exactly the budget %d", enumeration.StepsExamined, budget)
	}
	if enumeration.StepBudget != budget {
		t.Fatalf("StepBudget = %d, want %d", enumeration.StepBudget, budget)
	}

	again, second := enumerateImportCyclesWithinBudget(edges, 6, budget)
	if len(again) != len(cycles) || second != enumeration {
		t.Fatalf("second run = %d cycles %+v, first = %d cycles %+v: the budget stop must be deterministic", len(again), second, len(cycles), enumeration)
	}

	// A budget larger than the work completes with the full answer.
	full, done := enumerateImportCyclesWithinBudget(edges, 6, 10_000_000)
	if done.Truncated || done.StopReason != CycleStopNone {
		t.Fatalf("large budget enumeration = %+v, want an untruncated run", done)
	}
	if len(full) <= len(cycles) {
		t.Fatalf("large budget found %d cycles, want more than the %d found under the small budget", len(full), len(cycles))
	}
}

// TestEnumerateImportCyclesReportsTheCycleCapAsItsStopReason keeps the existing
// cap distinguishable from the new budget.
func TestEnumerateImportCyclesReportsTheCycleCapAsItsStopReason(t *testing.T) {
	t.Parallel()

	var edges []importCycleEdge
	for index := 0; index < importCycleEnumerationCap+1; index++ {
		left := fmt.Sprintf("k%04d", index)
		edges = append(edges, walkEdge(left, left+"z"), walkEdge(left+"z", left))
	}
	_, enumeration := enumerateImportCycles(edges, 5)
	if !enumeration.Truncated || enumeration.StopReason != CycleStopCycleCap {
		t.Fatalf("enumeration = %+v, want Truncated with stop reason %q", enumeration, CycleStopCycleCap)
	}
}

// bruteForceCycleKeys is an independent oracle: it lists every simple directed
// cycle of length 2..maxLength over `n` nodes by trying every ordered selection
// of distinct nodes, keeping the rotation that starts at the smallest node. It
// shares no code or idea with the depth-first walk or the component filter.
func bruteForceCycleKeys(n int, adjacent [][]bool, maxLength int) map[string]bool {
	keys := map[string]bool{}
	var extend func(path []int)
	extend = func(path []int) {
		last := path[len(path)-1]
		if len(path) >= 2 && adjacent[last][path[0]] {
			smallest := true
			for _, node := range path {
				if node < path[0] {
					smallest = false
				}
			}
			if smallest {
				parts := make([]string, 0, len(path))
				for _, node := range path {
					parts = append(parts, fmt.Sprintf("n%02d.py", node))
				}
				keys["repo-1\x00"+strings.Join(parts, "\x00")] = true
			}
		}
		if len(path) == maxLength {
			return
		}
		for next := 0; next < n; next++ {
			used := false
			for _, node := range path {
				if node == next {
					used = true
				}
			}
			if !used && adjacent[last][next] {
				extend(append(append([]int(nil), path...), next))
			}
		}
	}
	for start := 0; start < n; start++ {
		extend([]int{start})
	}
	return keys
}

// TestEnumerateImportCyclesMatchesABruteForceOracle proves the component filter
// changes no answer: on many small seeded graphs the cycle set equals an
// independent brute-force listing, including graphs whose cycles sit in several
// components joined by one-way bridges.
func TestEnumerateImportCyclesMatchesABruteForceOracle(t *testing.T) {
	t.Parallel()

	for seed := int64(1); seed <= 60; seed++ {
		rng := rand.New(rand.NewSource(seed))
		n := 3 + rng.Intn(5)
		adjacent := make([][]bool, n)
		for i := range adjacent {
			adjacent[i] = make([]bool, n)
		}
		var edges []importCycleEdge
		for from := 0; from < n; from++ {
			// Every node appears as a file, even one with no edge.
			edges = append(edges, walkEdge(fmt.Sprintf("n%02d", from), "external_module"))
			for to := 0; to < n; to++ {
				if from != to && rng.Intn(100) < 35 {
					adjacent[from][to] = true
					edges = append(edges, walkEdge(fmt.Sprintf("n%02d", from), fmt.Sprintf("n%02d", to)))
				}
			}
		}
		maxLength := 2 + rng.Intn(n-1)

		want := bruteForceCycleKeys(n, adjacent, maxLength)
		cycles, enumeration := enumerateImportCycles(edges, maxLength)
		if enumeration.Truncated {
			t.Fatalf("seed %d: enumeration truncated (%+v) on a tiny graph", seed, enumeration)
		}
		got := map[string]bool{}
		for _, cycle := range cycles {
			got[importCycleKey(cycle)] = true
		}
		if len(got) != len(want) {
			t.Fatalf("seed %d (n=%d maxLength=%d): got %d cycles, oracle %d", seed, n, maxLength, len(got), len(want))
		}
		var missing []string
		for key := range want {
			if !got[key] {
				missing = append(missing, strings.ReplaceAll(key, "\x00", ">"))
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Fatalf("seed %d: cycles missing from the walk: %v", seed, missing)
		}
	}
}

// randomImportGraph returns n files with `edgeCount` distinct directed imports
// chosen uniformly, the worst case for a corpus whose every import resolves to a
// file in the repository.
func randomImportGraph(seed int64, n, edgeCount int) []importCycleEdge {
	rng := rand.New(rand.NewSource(seed))
	seen := map[[2]int]bool{}
	var edges []importCycleEdge
	for len(seen) < edgeCount {
		from, to := rng.Intn(n), rng.Intn(n)
		if from == to || seen[[2]int{from, to}] {
			continue
		}
		seen[[2]int{from, to}] = true
		edges = append(edges, walkEdge(fmt.Sprintf("f%04d", from), fmt.Sprintf("f%04d", to)))
	}
	for from := 0; from < n; from++ {
		edges = append(edges, walkEdge(fmt.Sprintf("f%04d", from), "external_module"))
	}
	return edges
}

// TestImportCycleStepBudgetCoversMeasuredCorpusShapes checks the budget against
// a MODEL of the largest measured Python corpus. That corpus has 4,522 IMPORTS
// edges over 626 files, but only 172 of those imports resolved to a file in the
// repository, and only resolved imports become hops. The test does not walk the
// real edge list, which it does not have: it places 172 edges uniformly at
// random over 626 files, many times, and requires the budget to be at least ten
// times the worst walk at the maximum cycle length. A uniform placement is far
// sparser in cycles than clustered real graphs, so this establishes no margin
// over a real corpus; see the clustered-graph test for a denser model. The denser shapes below are stress cases, not measurements: a corpus in
// which every import resolved, and the 25,000-row scan limit. They are allowed to
// stop at the cycle cap or the budget, and the test only requires that they stop
// within the budget and say why.
func TestImportCycleStepBudgetCoversMeasuredCorpusShapes(t *testing.T) {
	t.Parallel()

	worst := 0
	for seed := int64(1); seed <= 200; seed++ {
		_, enumeration := enumerateImportCycles(randomImportGraph(seed, 626, 172), importCycleMaxMaxLength)
		worst = max(worst, enumeration.StepsExamined)
		if enumeration.Truncated {
			t.Fatalf("seed %d: the measured corpus shape was truncated (%+v); it must complete", seed, enumeration)
		}
	}
	t.Logf("measured shape (626 files, 172 resolved edges), worst of 200 placements: %d steps; budget %d", worst, importCycleEnumerationStepBudget)
	if worst*10 > importCycleEnumerationStepBudget {
		t.Fatalf("the measured-corpus walk needed %d steps; the budget %d is not at least 10x that", worst, importCycleEnumerationStepBudget)
	}

	for _, stress := range []struct {
		name         string
		files, edges int
	}{
		{name: "every import resolved", files: 626, edges: 4522},
		{name: "scan limit", files: 626, edges: 25000},
	} {
		_, enumeration := enumerateImportCycles(randomImportGraph(7, stress.files, stress.edges), importCycleMaxMaxLength)
		t.Logf("stress %q: steps=%d stop=%s", stress.name, enumeration.StepsExamined, enumeration.StopReason)
		if enumeration.StepsExamined > importCycleEnumerationStepBudget {
			t.Fatalf("stress %q examined %d steps, over the budget %d", stress.name, enumeration.StepsExamined, importCycleEnumerationStepBudget)
		}
		if enumeration.Truncated && enumeration.StopReason == CycleStopNone {
			t.Fatalf("stress %q truncated without a stop reason: %+v", stress.name, enumeration)
		}
	}
}

// BenchmarkEnumerateImportCyclesDenseComponent measures the cost per examined
// hop on a dense single component, so the step budget can be chosen to bound wall
// time. It reports ns/step; the figure is only meaningful on a quiet host.
func BenchmarkEnumerateImportCyclesDenseComponent(b *testing.B) {
	edges := completeDigraph(9)
	b.ResetTimer()
	var steps int
	for i := 0; i < b.N; i++ {
		_, enumeration := enumerateImportCyclesWithinBudget(edges, importCycleMaxMaxLength, importCycleEnumerationStepBudget)
		steps = enumeration.StepsExamined
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(steps), "ns/step")
	b.ReportMetric(float64(steps), "steps/op")
}

// TestCycleCoverageReportsTheBudgetTheWalkRanUnder proves the response reports
// the step budget carried by the enumeration, not the package constant. The two
// are equal today; a caller that injects a budget must see its own value, or the
// coverage would name a bound the walk never ran under.
func TestCycleCoverageReportsTheBudgetTheWalkRanUnder(t *testing.T) {
	t.Parallel()

	req := ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
	response := ImportDependencyResponseWithCycleEnumeration(req, nil, CycleEnumeration{
		Truncated:  true,
		StopReason: CycleStopStepBudget,
		StepBudget: 123,
	})
	coverage, _ := response["coverage"].(map[string]any)
	if got := coverage["cycle_enumeration_step_budget"]; got != 123 {
		t.Fatalf("coverage.cycle_enumeration_step_budget = %#v, want the enumeration's own budget 123", got)
	}

	empty := ImportDependencyResponse(req, nil)
	emptyCoverage, _ := empty["coverage"].(map[string]any)
	if got := emptyCoverage["cycle_enumeration_step_budget"]; got != importCycleEnumerationStepBudget {
		t.Fatalf("empty-page coverage step budget = %#v, want the package budget %d", got, importCycleEnumerationStepBudget)
	}
}

// clusteredImportGraph models real import structure more closely than a uniform
// random graph: files sit in packages, every file imports a few files of its own
// package, and a small share of imports reach a file of another package. Import
// graphs are dense inside a package and sparse across, which is what makes real
// strongly connected components, so this shape stresses the walk where a uniform
// placement barely creates any. It is a model: no real corpus edge list is walked.
func clusteredImportGraph(seed int64, files, packageSize, importsPerFile, crossPercent int) []importCycleEdge {
	rng := rand.New(rand.NewSource(seed))
	seen := map[[2]int]bool{}
	var edges []importCycleEdge
	add := func(from, to int) {
		if from == to || seen[[2]int{from, to}] {
			return
		}
		seen[[2]int{from, to}] = true
		edges = append(edges, walkEdge(fmt.Sprintf("f%04d", from), fmt.Sprintf("f%04d", to)))
	}
	for from := 0; from < files; from++ {
		base := from - from%packageSize
		size := min(packageSize, files-base)
		for i := 0; i < importsPerFile; i++ {
			add(from, base+rng.Intn(size))
		}
		if rng.Intn(100) < crossPercent {
			add(from, rng.Intn(files))
		}
	}
	for from := 0; from < files; from++ {
		edges = append(edges, walkEdge(fmt.Sprintf("f%04d", from), "external_module"))
	}
	return edges
}

// TestClusteredImportGraphWalkStaysWithinTheBudgetOrSaysWhy walks modelled
// package-clustered graphs at the maximum cycle length. It does not claim a
// margin over any real corpus; it records how the walk behaves when components
// are dense, and requires that every stop is within the budget and named.
func TestClusteredImportGraphWalkStaysWithinTheBudgetOrSaysWhy(t *testing.T) {
	t.Parallel()

	for _, shape := range []struct {
		name                                      string
		files, packageSize, perFile, crossPercent int
	}{
		{name: "626 files, packages of 16, 2 in-package imports each", files: 626, packageSize: 16, perFile: 2, crossPercent: 5},
		{name: "626 files, packages of 16, 4 in-package imports each", files: 626, packageSize: 16, perFile: 4, crossPercent: 5},
		{name: "626 files, packages of 40, 5 in-package imports each", files: 626, packageSize: 40, perFile: 5, crossPercent: 5},
	} {
		_, enumeration := enumerateImportCycles(clusteredImportGraph(11, shape.files, shape.packageSize, shape.perFile, shape.crossPercent), importCycleMaxMaxLength)
		t.Logf("clustered model %q: steps=%d stop=%s", shape.name, enumeration.StepsExamined, enumeration.StopReason)
		if enumeration.StepsExamined > importCycleEnumerationStepBudget {
			t.Fatalf("%q examined %d steps, over the budget %d", shape.name, enumeration.StepsExamined, importCycleEnumerationStepBudget)
		}
		if enumeration.Truncated && enumeration.StopReason == CycleStopNone {
			t.Fatalf("%q truncated without a stop reason: %+v", shape.name, enumeration)
		}
	}
}
