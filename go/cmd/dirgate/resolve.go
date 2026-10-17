// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path"
	"strings"
)

// Run modes.
const (
	modeBaseline  = "baseline"
	modeCI        = "ci"
	modePrePush   = "pre-push"
	modePreCommit = "pre-commit"
)

// defaultBaseRef is the local comparison branch.
const defaultBaseRef = "origin/main"

// resolution says which two trees to compare and which commits to read
// trailers from.
type resolution struct {
	mode       string
	baseCommit string // commit whose tree is the "before"
	headTree   string // tree id after: the merged result, or the index
	revRange   string // commits whose messages may carry waivers
	pending    bool   // the commit message does not exist yet (pre-commit)
	notes      []string
	only       map[string]bool // go/-relative directories to judge; nil = all
}

// resolveCI compares headRef (the merge ref in CI) with its first parent. A merge-base would
// misread a PR that branched before main moved files out of a directory as a
// shrink; HEAD^1 is exactly the tree the merge ref was built on. Precedent:
// scripts/verify-performance-evidence.sh.
func resolveCI(g gitRepo, event, headRef string) (resolution, error) {
	out, err := g.line("rev-list", "--parents", "-n", "1", headRef)
	if err != nil {
		return resolution{}, fmt.Errorf("read %s: %w", headRef, err)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return resolution{}, fmt.Errorf("%s has no parent, so there is no base to compare with", headRef)
	}
	if event == "pull_request" && len(fields) < 3 {
		return resolution{}, fmt.Errorf("%s is not a merge commit; a pull_request run must check out the merge ref (parent 1 is the base, parent 2 the PR head)", headRef)
	}
	headTree, err := g.line("rev-parse", fields[0]+"^{tree}")
	if err != nil {
		return resolution{}, err
	}
	// Every event is enforcing and reads the whole history reachable from the
	// base, so every event unshallows a shallow checkout first.
	if err := ensureFullHistory(g); err != nil {
		return resolution{}, err
	}
	return resolution{
		mode:       modeCI,
		baseCommit: fields[1],
		headTree:   headTree,
		revRange:   fields[1] + ".." + fields[0],
	}, nil
}

// resolvePrePush compares what the branch would look like merged onto the base
// ref with the base ref. A conflicting merge falls back to the merge base and
// says so.
func resolvePrePush(g gitRepo, baseRef string) (resolution, error) {
	base, err := g.resolveCommit(baseRef)
	if err != nil || base == "" {
		return resolution{}, fmt.Errorf("base ref %q does not resolve to a commit; fetch it or pass -base-ref", baseRef)
	}
	if err := ensureFullHistory(g); err != nil {
		return resolution{}, err
	}
	out, mergeErr := g.output("merge-tree", "--write-tree", base, "HEAD")
	if mergeErr == nil {
		tree := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		return resolution{mode: modePrePush, baseCommit: base, headTree: tree, revRange: base + "..HEAD"}, nil
	}
	if exitCode(mergeErr) != 1 {
		return resolution{}, fmt.Errorf("merge %s into HEAD: %w", baseRef, mergeErr)
	}
	mb, err := g.line("merge-base", base, "HEAD")
	if err != nil || mb == "" {
		return resolution{}, fmt.Errorf("no common ancestor between %s and HEAD: %v", baseRef, err)
	}
	headTree, err := g.line("rev-parse", "HEAD^{tree}")
	if err != nil {
		return resolution{}, err
	}
	return resolution{
		mode: modePrePush, baseCommit: mb, headTree: headTree, revRange: mb + "..HEAD",
		notes: []string{fmt.Sprintf(
			"merging %s into HEAD conflicts, so the merge-base fallback is in use (base %s); it cannot see growth that landed on %s, so rebase before you push",
			baseRef, short(mb), baseRef)},
	}, nil
}

// resolvePreCommit compares the index with the merge base, limited to the
// directories of the staged files. The commit message does not exist yet, so
// trailer-dependent findings are only notes here; pre-push and CI enforce them.
func resolvePreCommit(g gitRepo, baseRef string, files []string) (resolution, error) {
	base, err := g.resolveCommit(baseRef)
	if err != nil || base == "" {
		return resolution{}, fmt.Errorf("base ref %q does not resolve to a commit; fetch it or pass -base-ref", baseRef)
	}
	mb, err := g.line("merge-base", base, "HEAD")
	if err != nil || mb == "" {
		return resolution{}, fmt.Errorf("no common ancestor between %s and HEAD: %v", baseRef, err)
	}
	tree, err := g.indexTree()
	if err != nil {
		return resolution{}, fmt.Errorf("read the index: %w", err)
	}
	return resolution{
		mode: modePreCommit, baseCommit: mb, headTree: tree, revRange: mb + "..HEAD",
		pending: true, only: changedDirectories(files),
	}, nil
}

// changedDirectories maps staged paths to the go/-relative directories whose
// verdict they can change: the directory itself and its parent (a new
// subpackage can collide with a file one level up).
func changedDirectories(files []string) map[string]bool {
	only := map[string]bool{}
	for _, f := range files {
		rel, ok := strings.CutPrefix(path.Clean(f), goRoot+"/")
		if !ok || !strings.HasSuffix(rel, ".go") {
			continue
		}
		dir := path.Dir(rel)
		only[dir] = true
		if dir != "." {
			only[path.Dir(dir)] = true
		}
	}
	return only
}
