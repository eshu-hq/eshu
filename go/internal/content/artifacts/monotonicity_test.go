// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// plantedStopAtFloorPasses is the seeded violation of #7951: a selection that
// ignores the allowance, stops at the 50th candidate as the pre-#7951 cap did,
// and reports only what it selected as the candidate pool, so no count or
// reason discloses the dropped candidates. The monotonicity differential must
// fail on it.
var plantedStopAtFloorPasses = loadPasses{
	hydrate: func(ctx context.Context, reader Reader, repoID string, files []File, _ int, stats *ReadStats) ([]File, error) {
		out, err := hydrateConfigFiles(ctx, reader, repoID, files, HydrationFloor, stats)
		stats.CandidateCount = stats.CandidatesHydrated
		return out, err
	},
	build: BuildConfigPaths,
}

// wideTree extends a generated tree to about candidates config-artifact
// candidates. The extra files mix every class the extractors meet: Terraform
// with module sources, Terragrunt, plain YAML and JSON, var files, compose,
// and kustomizations that reference files on either side of the 50th
// candidate.
func wideTree(rng *rand.Rand, candidates int) []File {
	files := randomTree(rng)
	seen := make(map[string]bool, candidates)
	for _, file := range files {
		seen[file.RelativePath] = true
	}
	for i := 0; len(files) < candidates; i++ {
		dir := fmt.Sprintf("w%04d/", i)
		var file File
		switch rng.Intn(7) {
		case 0, 1:
			file = fileRow(dir+"main.tf", fmt.Sprintf("module \"w%d\" {\n  source = \"../w%04d\"\n}\n", i, rng.Intn(i+1)))
		case 2:
			file = fileRow(dir+"terragrunt.hcl", "include {\n  path = find_in_parent_folders()\n}\n")
		case 3:
			file = fileRow(dir+"values.yaml", poisonContents[rng.Intn(len(poisonContents))])
		case 4:
			file = fileRow(dir+"env.tfvars", "region = \"x\"\n")
		case 5:
			file = fileRow(dir+"docker-compose.yml", "services:\n  app:\n    env_file:\n      - ./app.env\n")
		default:
			target := fmt.Sprintf("../w%04d/values.yaml", rng.Intn(i+1))
			file = fileRow(dir+"kustomization.yaml", kustomizationFor(target, "res.yaml"))
		}
		if seen[file.RelativePath] {
			continue
		}
		seen[file.RelativePath] = true
		files = append(files, file)
	}
	return files
}

// relatedRowSet loads files as one related repository (list read from the
// store) at allowance and returns the encoded rows as a set, the row count,
// and the stats.
func relatedRowSet(t testing.TB, files []File, passes loadPasses, allowance int) (map[string]bool, []string, ReadStats) {
	t.Helper()
	store := newHydrationStore(files...)
	rows, stats, err := loadConfigPaths(context.Background(), store,
		Repository{ID: hydrationTestRepoID, Name: "related"}, nil, false, allowance, passes)
	if err != nil {
		t.Fatalf("load at allowance %d: %v", allowance, err)
	}
	set := make(map[string]bool, len(rows))
	ordered := make([]string, 0, len(rows))
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal row: %v", err)
		}
		set[string(encoded)] = true
		ordered = append(ordered, string(encoded))
	}
	return set, ordered, stats
}

// monotonicityViolations is the #7951 differential for one tree: the rows at
// the whole window are a superset of the rows at the floor, byte-identical
// when the window holds no more than HydrationFloor candidates, equal to the
// read-everything oracle at the whole window, and the stats count every
// candidate of the window. passes is the pass under test.
func monotonicityViolations(t testing.TB, name string, files []File, passes loadPasses) []string {
	t.Helper()
	var violations []string
	floorSet, floorRows, _ := relatedRowSet(t, files, passes, HydrationFloor)
	wholeSet, wholeRows, wholeStats := relatedRowSet(t, files, passes, AllowanceWholeWindow)
	_, oracleRows, _ := relatedRowSet(t, files, fullConfigArtifactPasses, AllowanceWholeWindow)

	for row := range floorSet {
		if !wholeSet[row] {
			violations = append(violations, fmt.Sprintf("%s: row at the floor missing at the whole window: %s", name, row))
			break
		}
	}
	if fmt.Sprint(wholeRows) != fmt.Sprint(oracleRows) {
		violations = append(violations, fmt.Sprintf("%s: whole-window rows (%d) differ from the read-everything oracle (%d)", name, len(wholeRows), len(oracleRows)))
	}
	// The pool is counted on the list, which carries no content.
	_, pool := SelectHydrationIndexes(newHydrationStore(files...).listed(t), isConfigArtifactCandidate, fileContent, AllowanceWholeWindow)
	if pool <= HydrationFloor && fmt.Sprint(floorRows) != fmt.Sprint(wholeRows) {
		violations = append(violations, fmt.Sprintf("%s: %d candidates, yet floor and whole-window rows differ", name, pool))
	}
	if wholeStats.CandidateCount != pool || wholeStats.CandidatesHydrated != pool {
		violations = append(violations, fmt.Sprintf("%s: whole window counted %d candidates, hydrated %d, want %d of %d",
			name, wholeStats.CandidateCount, wholeStats.CandidatesHydrated, pool, pool))
	}
	return violations
}

// monotonicityCorpus is the PR1 generated trees (seeds 1..400) plus wide trees
// of 60 to 3,700 candidates.
func monotonicityCorpus() map[string][]File {
	corpus := make(map[string][]File, 420)
	for seed := int64(1); seed <= 400; seed++ {
		corpus[fmt.Sprintf("seed %d", seed)] = randomTree(rand.New(rand.NewSource(seed)))
	}
	for i, size := range []int{60, 75, 120, 250, 400, 800, 1200, 2000, 3000, 3700} {
		for variant := int64(0); variant < 2; variant++ {
			seed := int64(7951+10*i) + variant
			corpus[fmt.Sprintf("wide %d seed %d", size, seed)] = wideTree(rand.New(rand.NewSource(seed)), size)
		}
	}
	return corpus
}

// TestLiftedCapIsMonotoneOverGeneratedTrees is ruling check 4: over the PR1
// generated trees and wide trees of 60-3,700 candidates, lifting the cap only
// adds rows.
func TestLiftedCapIsMonotoneOverGeneratedTrees(t *testing.T) {
	t.Parallel()

	corpus := monotonicityCorpus()
	names := make([]string, 0, len(corpus))
	for name := range corpus {
		names = append(names, name)
	}
	sort.Strings(names)
	grew := 0
	for _, name := range names {
		if violations := monotonicityViolations(t, name, corpus[name], productionLoadPasses); len(violations) > 0 {
			t.Fatalf("%d violations, first: %s", len(violations), violations[0])
		}
		floorSet, _, _ := relatedRowSet(t, corpus[name], productionLoadPasses, HydrationFloor)
		wholeSet, _, _ := relatedRowSet(t, corpus[name], productionLoadPasses, AllowanceWholeWindow)
		if len(wholeSet) > len(floorSet) {
			grew++
		}
	}
	if grew < 10 {
		t.Fatalf("only %d trees gained rows past the floor; the corpus does not exercise the lifted cap", grew)
	}
}

// TestLiftedCapMonotonicitySeededPair is the seeded RED/GREEN pair of check 4:
// the differential passes on the production pass and fails on the planted
// stop-at-50 selection.
func TestLiftedCapMonotonicitySeededPair(t *testing.T) {
	t.Parallel()

	files := wideTree(rand.New(rand.NewSource(7951)), 400)
	if violations := monotonicityViolations(t, "green", files, productionLoadPasses); len(violations) != 0 {
		t.Fatalf("GREEN leg: %v", violations)
	}
	if violations := monotonicityViolations(t, "red", files, plantedStopAtFloorPasses); len(violations) == 0 {
		t.Fatal("RED leg: the planted stop-at-50 selection passed the monotonicity differential")
	}
}
