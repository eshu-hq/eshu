// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Tests of the name-free read model; see name_free_harness_test.go for the
// theory.

package artifacts

import (
	"math/rand"
	"path"
	"sort"
	"strings"
	"testing"
)

// TestNameFreeRowsEqualBuildWithName is the proof. Pass: 0 differing rows, 0
// order-only differences, and an order that is total on the corpus.
func TestNameFreeRowsEqualBuildWithName(t *testing.T) {
	cfg := nameFreeApply{scoped: nameFreeScopedKinds(t)}
	agg := nameFreeRunCorpus(t, cfg, 400)

	groups := make([]string, 0, len(agg.byGroup))
	for g := range agg.byGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	for _, g := range groups {
		v := agg.byGroup[g]
		t.Logf("group %-14s pairs=%-6d rows=%-7d pathsChangedByName=%-5d rowsCollapsedByName=%d", g, v[0], v[1], v[2], v[3])
	}
	t.Logf("pairs=%d nonEmptyPairs=%d rows=%d differingRows=%d orderOnlyDiffs=%d "+
		"pathsChangedByName=%d rawRowsCollapsedByName=%d sentinelBuildMismatch=%d",
		agg.pairs, agg.nonEmptyPairs, agg.rows, agg.diffRows, agg.orderOnly,
		agg.stripped, agg.collapsed, agg.sentinelBroken)
	t.Logf("SORT TIES: threeKeyTies=%d (adjacent rows equal on path, source_repo, relative_path: the pairs the "+
		"evidence_kind key now orders) fourKeyTies=%d (adjacent rows equal on all four keys)",
		agg.threeKeyTies, agg.fourKeyTies)
	t.Logf("REORDERED by the fourth key, against the pre-PR3a loader replayed on the same rows: builds=%d rows=%d",
		agg.reorderedPairs, agg.reorderedRows)

	kinds := make([]string, 0, len(agg.kindRows))
	for k := range agg.kindRows {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		t.Logf("kind %-36s scoped=%-5v rows=%-7d unscopedPathChangedByName=%d", k, cfg.scoped[k], agg.kindRows[k], agg.kindPathChanged[k])
	}
	t.Logf("SCOPED   rows=%d differingRows=%d pathsChangedByName=%d rawRowsCollapsedByName=%d",
		agg.scopedRows, agg.scopedDiff, agg.scopedStripped, agg.scopedCollapsed)
	t.Logf("UNSCOPED rows=%d differingRows=%d rowsWhosePathTheNameChanged=%d",
		agg.unscopedRows, agg.unscopedDiff, agg.unscopedPathChangedByName)
	if agg.unscopedRows == 0 || agg.scopedRows == 0 {
		t.Fatalf("corpus holds scoped=%d unscoped=%d rows; both classes are required", agg.scopedRows, agg.unscopedRows)
	}
	if agg.unscopedPathChangedByName != 0 || agg.unscopedDiff != 0 || agg.scopedDiff != 0 {
		t.Fatalf("name scope violated: scopedDiff=%d unscopedDiff=%d unscopedPathChanged=%d", agg.scopedDiff, agg.unscopedDiff, agg.unscopedPathChangedByName)
	}

	// Inert guards: the corpus must make the name matter.
	if agg.pairs < 5000 || agg.nonEmptyPairs < 2000 {
		t.Fatalf("corpus too small: pairs=%d nonEmpty=%d", agg.pairs, agg.nonEmptyPairs)
	}
	if agg.stripped == 0 || agg.collapsed == 0 {
		t.Fatalf("name never changed a path (%d) or collapsed rows (%d); the proof is inert", agg.stripped, agg.collapsed)
	}
	if agg.fourKeyTies != 0 {
		t.Errorf("%d sort-key ties under path, source_repo, relative_path and evidence_kind; the order is not total", agg.fourKeyTies)
	}
	if agg.sentinelBroken != 0 {
		t.Errorf("%d cases where a sentinel-name build differs from the empty-name build beyond source_repo", agg.sentinelBroken)
	}
	if agg.diffRows != 0 || agg.orderOnly != 0 {
		for _, f := range agg.failures[:min(len(agg.failures), 20)] {
			t.Log(f)
		}
		t.Fatalf("FAIL: %d differing rows, %d order-only differences", agg.diffRows, agg.orderOnly)
	}
}

// TestNameFreeHarnessCatchesWrongApplyName is the seeded RED leg: three
// plausible read-time shortcuts must each be caught by the same corpus.
func TestNameFreeHarnessCatchesWrongApplyName(t *testing.T) {
	scoped := nameFreeScopedKinds(t)
	for label, cfg := range map[string]nameFreeApply{
		"strip every kind (uniform)": {scoped: scoped, uniform: true},
		"no dedup on the named key":  {scoped: scoped, noDedup: true},
		"keep the raw order":         {scoped: scoped, noResort: true},
	} {
		agg := nameFreeRunCorpus(t, cfg, 100)
		t.Logf("mutant %q: differingRows=%d (scoped=%d unscoped=%d) orderOnlyDiffs=%d",
			label, agg.diffRows, agg.scopedDiff, agg.unscopedDiff, agg.orderOnly)
		if agg.diffRows == 0 && agg.orderOnly == 0 {
			t.Errorf("mutant %q survived: the corpus does not exercise that part of the name contract", label)
		}
	}
}

// legacyCleanRepositoryRelativePath is a frozen copy of the clean step as it
// stood in query/repositoryartifacts before #7126 PR 3a
// (CleanRepositoryRelativePath). It shares no code with the production
// CleanRelativePath, so a change to that function cannot move its own oracle.
func legacyCleanRepositoryRelativePath(relativePath string) string {
	relativePath = path.Clean(strings.TrimSpace(relativePath))
	switch relativePath {
	case "", ".", "/":
		return ""
	default:
		return strings.TrimPrefix(relativePath, "./")
	}
}

// legacyNormalizeRepoScopedPath is a frozen copy of normalizeRepoScopedPath
// before #7126 PR 3a split it into a clean step and a strip step, clean step
// included. It is the oracle of TestStripStepEqualsTheLegacyNormalize.
func legacyNormalizeRepoScopedPath(pathValue, repoName string) string {
	cleanedPath := legacyCleanRepositoryRelativePath(pathValue)
	trimmedRepoName := strings.TrimSpace(repoName)
	if cleanedPath == "" || trimmedRepoName == "" {
		return cleanedPath
	}

	parts := strings.Split(cleanedPath, "/")
	for index := range parts {
		prefix := path.Join(parts[:index+1]...)
		if path.Base(prefix) != trimmedRepoName {
			continue
		}
		suffix := path.Join(parts[index+1:]...)
		if suffix != "" && suffix != "." {
			return suffix
		}
	}
	return cleanedPath
}

// TestStripStepEqualsTheLegacyNormalize proves the split is byte-identical:
// the strip of the cleaned path equals the pre-split function on the original
// path, for random paths and names. It also logs how often cleaning the stored
// path a second time would differ, which is why a stored path goes through the
// strip step only.
func TestStripStepEqualsTheLegacyNormalize(t *testing.T) {
	rng := rand.New(rand.NewSource(7126))
	parts := []string{"svc", "svcs", "a", "..", ".", "", "env", "sv", "x y", "ü", "svc ", " svc"}
	names := []string{"", "svc", " svc ", "..", ".", "env", "a", "x y"}
	recleanDiffers, recleanDiffersTwiceCleaned := 0, 0
	const trials = 200000
	for i := 0; i < trials; i++ {
		n := 1 + rng.Intn(6)
		segs := make([]string, n)
		for j := range segs {
			segs[j] = parts[rng.Intn(len(parts))]
		}
		original := strings.Join(segs, "/")
		if rng.Intn(5) == 0 {
			original = "./" + original
		}
		name := names[rng.Intn(len(names))]
		stored := CleanRelativePath(original)
		if legacy := legacyCleanRepositoryRelativePath(original); stored != legacy {
			t.Fatalf("original %q: CleanRelativePath=%q, legacy clean=%q", original, stored, legacy)
		}
		want := legacyNormalizeRepoScopedPath(original, name)
		if got := StripRepositoryName(stored, name); got != want {
			t.Fatalf("original %q stored %q name %q: strip=%q, legacy normalize(original)=%q", original, stored, name, got, want)
		}
		if normalizeRepoScopedPath(stored, name) != want {
			recleanDiffers++
		}
		// The compose and var-file extractors clean once before
		// appendConfigArtifactRow cleans again, so the stored path there is a
		// twice-cleaned string.
		twice := CleanRelativePath(stored)
		live := legacyNormalizeRepoScopedPath(stored, name)
		if got := StripRepositoryName(twice, name); got != live {
			t.Fatalf("twice-cleaned %q name %q: strip=%q, live=%q", twice, name, got, live)
		}
		if normalizeRepoScopedPath(twice, name) != live {
			recleanDiffersTwiceCleaned++
		}
	}
	t.Logf("re-cleaning the stored path differs from the live result in %d of %d random inputs; "+
		"%d when the extractor already cleaned twice", recleanDiffers, trials, recleanDiffersTwiceCleaned)
}

// TestNameTouchPoints pins exactly what the name changes on one file set.
func TestNameTouchPoints(t *testing.T) {
	files := nameFreeNameBearingFiles("svc")
	raw := nameFreeFlatten(BuildConfigPaths("", files))
	named := nameFreeFlatten(BuildConfigPaths("svc", files))
	for _, r := range raw {
		t.Logf("raw   %-42s kind=%-34s rel=%s", r.Path, r.Kind, r.Rel)
	}
	for _, r := range named {
		t.Logf("named %-42s kind=%-34s rel=%s src=%s", r.Path, r.Kind, r.Rel, r.Source)
	}
	has := func(rows []nameFreeRow, p, kind string) bool {
		for _, r := range rows {
			if r.Path == p && r.Kind == kind {
				return true
			}
		}
		return false
	}
	count := func(rows []nameFreeRow, p, kind string) int {
		n := 0
		for _, r := range rows {
			if r.Path == p && r.Kind == kind {
				n++
			}
		}
		return n
	}
	// Name stripped from a tfvars path; "svcs" (name is a prefix of the
	// component) untouched.
	if !has(raw, "svc/env/a.tfvars", "terraform_var_file") || !has(named, "env/a.tfvars", "terraform_var_file") {
		t.Error("tfvars path under the name directory was not stripped under the name")
	}
	if !has(named, "svcs/c.tfvars", "terraform_var_file") {
		t.Error("a component that only starts with the name must stay")
	}
	// Two raw compose references collapse to one named row; raw keeps both.
	if count(raw, "svc/conf/.env", "docker_compose_env_file") != 1 || count(raw, "conf/.env", "docker_compose_env_file") != 1 {
		t.Error("raw build must hold both distinct compose paths")
	}
	if count(named, "conf/.env", "docker_compose_env_file") != 1 || count(named, "svc/conf/.env", "docker_compose_env_file") != 0 {
		t.Error("named build must hold one collapsed compose row")
	}
	// Ansible rows are not name-scoped.
	if !has(raw, "svc/group_vars/all.yml", "ansible_vars") || !has(named, "svc/group_vars/all.yml", "ansible_vars") {
		t.Error("ansible path must be identical raw and named")
	}
	if len(raw) <= len(named) {
		t.Errorf("raw rows %d, named rows %d: expected the name to collapse rows", len(raw), len(named))
	}
}

// TestUnscopedRowsKeepNameComponent pins the case the first ruling got
// wrong: a kustomize row and an Ansible row whose path holds the repository
// name as a component keep that path, while HCL and compose rows lose it.
func TestUnscopedRowsKeepNameComponent(t *testing.T) {
	files := []File{
		fileRow("overlays/kustomization.yaml", kustomizationFor("res.yaml")),
		fileRow("overlays/res.yaml", policyResource("configd-app")),
		fileRow("configd/roles/web/tasks/main.yml", "- debug: {}"),
		fileRow("configd/env/a.tfvars", "x = 1"),
		fileRow("docker-compose.yml", "services:\n  app:\n    env_file:\n      - configd/conf/.env\n      - conf/.env\n"),
	}
	// The policy path is /configd/configd-app/*, so "configd" is a component.
	for _, name := range []string{"configd", "roles", "overlays", "role", "configd-app", ""} {
		rows := nameFreeFlatten(BuildConfigPaths(name, files))
		raw := nameFreeFlatten(BuildConfigPaths("", files))
		for _, r := range rows {
			switch {
			case nameFreeScopedKinds(t)[r.Kind]:
				continue
			default:
				found := false
				for _, rr := range raw {
					if rr.Path == r.Path && rr.Kind == r.Kind && rr.Rel == r.Rel {
						found = true
					}
				}
				if !found {
					t.Errorf("name %q changed unscoped row %+v", name, r)
				}
			}
		}
	}
	rows := nameFreeFlatten(BuildConfigPaths("configd", files))
	want := map[string]bool{
		"/configd/configd-app/*":           false, // kustomize, unchanged
		"configd/roles/web/tasks/main.yml": false, // ansible, unchanged
		"env/a.tfvars":                     false, // hcl, stripped
		"conf/.env":                        false, // compose, stripped and collapsed
	}
	for _, r := range rows {
		if _, ok := want[r.Path]; ok {
			want[r.Path] = true
		}
		if r.Path == "configd/env/a.tfvars" || r.Path == "configd/conf/.env" {
			t.Errorf("scoped row %+v kept the name component", r)
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("name=configd: expected row path %q missing from %+v", p, rows)
		}
	}
	n := 0
	for _, r := range rows {
		if r.Path == "conf/.env" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("collapsed compose row count = %d, want 1", n)
	}
}
