// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Name-free proof of the #7126 config-artifact read model, run against the
// production strip step and the production name-scope table.
//
// Theory (arbiter ruling G1): the repository NAME enters the output of
// BuildConfigPaths at exactly four places, so rows can be stored name-free and
// the graph name applied at read time:
//
//	path          the strip step (StripRepositoryName), only for rows of a
//	              name-scoped evidence kind (NameScoped): HCL and Docker Compose
//	source_repo   the name itself, every row
//	dedup key     path|name|relative_path|kind, so two raw paths that collapse
//	              to one named path become one row
//	sort order    the final sort runs on the named path
//
// Claim under test: for every (name, files),
//
//	BuildConfigPaths(name, files) ==
//	    applyName(name, BuildConfigPaths("", files))
//
// "" is the unbound build: normalizeRepoScopedPath returns the cleaned path
// when the name is blank, so no production seam is needed. applyName runs the
// production StripRepositoryName on the cleaned stored path and never cleans
// again.

package artifacts

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

type nameFreeRow struct{ Path, Source, Rel, Kind string }

// nameFreeCase is one recorded file set of testdata/name_free_corpus.jsonl.
type nameFreeCase struct {
	Name  string `json:"name"`
	Files []struct {
		RelativePath string `json:"relative_path"`
		Content      string `json:"content"`
	} `json:"files"`
}

// files returns the case's files in the extractors' file type.
func (c nameFreeCase) files() []File {
	files := make([]File, 0, len(c.Files))
	for _, f := range c.Files {
		files = append(files, File{RelativePath: f.RelativePath, Content: f.Content})
	}
	return files
}

func nameFreeFlatten(result map[string]any) []nameFreeRow {
	var out []nameFreeRow
	for _, row := range ConfigPathRows(result) {
		out = append(out, nameFreeRow{
			Path:   stringValue(row, "path"),
			Source: stringValue(row, "source_repo"),
			Rel:    stringValue(row, "relative_path"),
			Kind:   stringValue(row, "evidence_kind"),
		})
	}
	return out
}

// nameFreeLess is the production total order: path, source_repo,
// relative_path, evidence_kind.
func nameFreeLess(rows []nameFreeRow) func(i, j int) bool {
	return func(i, j int) bool {
		if rows[i].Path != rows[j].Path {
			return rows[i].Path < rows[j].Path
		}
		if rows[i].Source != rows[j].Source {
			return rows[i].Source < rows[j].Source
		}
		if rows[i].Rel != rows[j].Rel {
			return rows[i].Rel < rows[j].Rel
		}
		return rows[i].Kind < rows[j].Kind
	}
}

// nameFreeScopedKinds is the production name-scope table as a set of the
// name-scoped kinds. The guard in name_scope_test.go proves the table matches
// the call sites of the extractors.
func nameFreeScopedKinds(t *testing.T) map[string]bool {
	t.Helper()
	scoped := map[string]bool{}
	for _, kind := range EvidenceKinds() {
		if isScoped, _ := NameScoped(kind); isScoped {
			scoped[kind] = true
		}
	}
	if len(scoped) != 10 {
		t.Fatalf("name-scope table holds %d scoped kinds %v, want the 7 HCL and 3 compose kinds", len(scoped), scoped)
	}
	return scoped
}

// nameFreeApply configures the read-time name step the proof applies, with
// switches that turn it into a known-wrong shortcut for the seeded RED leg.
type nameFreeApply struct {
	scoped   map[string]bool
	uniform  bool // mutant: strip every kind, not only the scoped ones
	noDedup  bool // mutant: skip the dedup on the named key
	noResort bool // mutant: keep the raw order
}

// nameFreeApplyName is the read-time function the stored design needs. It
// returns the rows, the count of rows whose path the name changed, and the
// count of raw rows that collapsed into another row under the name.
func nameFreeApplyName(name string, raw []nameFreeRow, cfg nameFreeApply) (rows []nameFreeRow, stripped, collapsed int) {
	seen := map[string]struct{}{}
	for _, r := range raw {
		p := r.Path
		if cfg.uniform || cfg.scoped[r.Kind] {
			p = StripRepositoryName(p, name)
		}
		if p == "" {
			continue
		}
		if p != r.Path {
			stripped++
		}
		if !cfg.noDedup {
			key := strings.Join([]string{p, name, r.Rel, r.Kind}, "|")
			if _, ok := seen[key]; ok {
				collapsed++
				continue
			}
			seen[key] = struct{}{}
		}
		rows = append(rows, nameFreeRow{Path: p, Source: name, Rel: r.Rel, Kind: r.Kind})
	}
	if !cfg.noResort {
		sort.SliceStable(rows, nameFreeLess(rows))
	}
	return rows, stripped, collapsed
}

// nameFreeDiff returns the symmetric multiset difference size and whether the
// order matches.
func nameFreeDiff(want, got []nameFreeRow) (diff int, sameOrder bool) {
	count := map[nameFreeRow]int{}
	for _, r := range want {
		count[r]++
	}
	for _, r := range got {
		count[r]--
	}
	for _, n := range count {
		if n < 0 {
			n = -n
		}
		diff += n
	}
	sameOrder = len(want) == len(got)
	for i := 0; sameOrder && i < len(want); i++ {
		sameOrder = want[i] == got[i]
	}
	return diff, sameOrder
}

type nameFreeAgg struct {
	pairs, nonEmptyPairs, rows int
	diffRows, orderOnly        int
	stripped, collapsed        int
	sentinelBroken             int
	// threeKeyTies counts adjacent rows of a build that tie under the three
	// keys the live loader sorted on before #7126 PR3a (path, source_repo,
	// relative_path): the pairs the fourth key (evidence_kind) now orders.
	// fourKeyTies counts adjacent rows that tie under all four keys; the
	// order is total only when it is zero.
	threeKeyTies, fourKeyTies int
	// reorderedPairs counts the (name, files) builds whose row order differs
	// from the order the live loader produced before #7126 PR3a, replayed
	// with its old build sort and its old three-key sort.Slice. reorderedRows
	// counts the positions that differ.
	reorderedPairs, reorderedRows int
	failures                      []string
	// byGroup is pairs, rows, pathsChangedByName, rowsCollapsedByName per
	// corpus group.
	byGroup map[string]*[4]int

	// Split by name scope: scoped rows are the ones built through
	// appendConfigArtifactRow, unscoped are kustomize and Ansible rows.
	scopedRows, unscopedRows        int
	scopedDiff, unscopedDiff        int
	unscopedPathChangedByName       int
	scopedStripped, scopedCollapsed int
	kindRows                        map[string]int
	kindPathChanged                 map[string]int
}

const nameFreeSentinel = "\x00name-free-unbound\x00"

func (a *nameFreeAgg) check(label, name string, files []File, cfg nameFreeApply) {
	want := nameFreeFlatten(BuildConfigPaths(name, files))
	raw := nameFreeFlatten(BuildConfigPaths("", files))
	got, stripped, collapsed := nameFreeApplyName(name, raw, cfg)
	diff, sameOrder := nameFreeDiff(want, got)

	if a.byGroup == nil {
		a.byGroup = map[string]*[4]int{}
	}
	group := label
	if i := strings.IndexAny(group, "[/-"); i > 0 {
		group = group[:i]
	}
	if a.byGroup[group] == nil {
		a.byGroup[group] = &[4]int{}
	}
	g := a.byGroup[group]
	g[0]++
	g[1] += len(want)
	g[2] += stripped
	g[3] += collapsed

	if a.kindRows == nil {
		a.kindRows = map[string]int{}
		a.kindPathChanged = map[string]int{}
	}
	split := func(rows []nameFreeRow, scoped bool) []nameFreeRow {
		var out []nameFreeRow
		for _, r := range rows {
			if cfg.scoped[r.Kind] == scoped {
				out = append(out, r)
			}
		}
		return out
	}
	for _, r := range want {
		a.kindRows[r.Kind]++
		if cfg.scoped[r.Kind] {
			a.scopedRows++
		} else {
			a.unscopedRows++
		}
	}
	dScoped, _ := nameFreeDiff(split(want, true), split(got, true))
	dUnscoped, _ := nameFreeDiff(split(want, false), split(got, false))
	a.scopedDiff += dScoped
	a.unscopedDiff += dUnscoped
	a.scopedStripped += stripped
	a.scopedCollapsed += collapsed
	// An unscoped row's path must be identical in the named and unbound builds.
	rawUnscoped := map[nameFreeRow]int{}
	for _, r := range split(raw, false) {
		r.Source = ""
		rawUnscoped[r]++
	}
	for _, r := range split(want, false) {
		r.Source = ""
		if rawUnscoped[r] == 0 {
			a.unscopedPathChangedByName++
			a.kindPathChanged[r.Kind]++
		} else {
			rawUnscoped[r]--
		}
	}

	a.pairs++
	a.rows += len(want)
	a.stripped += stripped
	a.collapsed += collapsed
	if len(want) > 0 {
		a.nonEmptyPairs++
	}
	for i := 1; i < len(want); i++ {
		prev, cur := want[i-1], want[i]
		if prev.Path == cur.Path && prev.Source == cur.Source && prev.Rel == cur.Rel {
			a.threeKeyTies++
			if prev.Kind == cur.Kind {
				a.fourKeyTies++
			}
		}
	}
	oldBuild := append([]nameFreeRow(nil), want...)
	sort.Slice(oldBuild, func(i, j int) bool {
		if oldBuild[i].Path != oldBuild[j].Path {
			return oldBuild[i].Path < oldBuild[j].Path
		}
		if oldBuild[i].Kind != oldBuild[j].Kind {
			return oldBuild[i].Kind < oldBuild[j].Kind
		}
		return oldBuild[i].Rel < oldBuild[j].Rel
	})
	oldLoad := append([]nameFreeRow(nil), oldBuild...)
	sort.Slice(oldLoad, func(i, j int) bool {
		if oldLoad[i].Path != oldLoad[j].Path {
			return oldLoad[i].Path < oldLoad[j].Path
		}
		if oldLoad[i].Source != oldLoad[j].Source {
			return oldLoad[i].Source < oldLoad[j].Source
		}
		return oldLoad[i].Rel < oldLoad[j].Rel
	})
	moved := 0
	for i := range want {
		if oldLoad[i] != want[i] {
			moved++
		}
	}
	if moved > 0 {
		a.reorderedPairs++
		a.reorderedRows += moved
	}
	// The sentinel build must equal the empty-name build apart from source_repo.
	sentinel := nameFreeFlatten(BuildConfigPaths(nameFreeSentinel, files))
	for i := range sentinel {
		sentinel[i].Source = ""
	}
	rawNoSource := append([]nameFreeRow(nil), raw...)
	for i := range rawNoSource {
		rawNoSource[i].Source = ""
	}
	if d, same := nameFreeDiff(rawNoSource, sentinel); d != 0 || !same {
		a.sentinelBroken++
	}
	if diff != 0 {
		a.diffRows += diff
		a.failures = append(a.failures, fmt.Sprintf("%s name=%q: %d differing rows", label, name, diff))
	} else if !sameOrder {
		a.orderOnly++
		a.failures = append(a.failures, fmt.Sprintf("%s name=%q: same rows, different order", label, name))
	}
}
