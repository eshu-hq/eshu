// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// maxDirFiles is the per-directory cap on non-test .go files (issue #6054).
const maxDirFiles = 40

// goRoot is the module directory the gate measures.
const goRoot = "go"

// snapshot is the directory census of one git tree under go/.
type snapshot struct {
	// qualifying maps a go/-relative directory to the basenames of the
	// non-test .go files directly inside it. Skipped directories are absent.
	qualifying map[string]map[string]struct{}
	// goDirs holds every directory that directly contains any .go file, test
	// files included. A subpackage for the naming rule is such a directory.
	goDirs map[string]struct{}
	// allDirs holds every directory that contains any tracked path, with all
	// its ancestors. A trailer must name one of these.
	allDirs map[string]struct{}
	// subpackages maps a directory to its immediate child directories that
	// directly contain a .go file.
	subpackages map[string][]string
}

// loadSnapshot reads the census of tree (a commit or tree id) from git.
func loadSnapshot(g gitRepo, tree string) (snapshot, error) {
	out, err := g.output("ls-tree", "-r", "-z", "--name-only", "--full-tree", tree, "--", goRoot)
	if err != nil {
		return snapshot{}, fmt.Errorf("list %s in %s: %w", goRoot, short(tree), err)
	}
	return parseSnapshot(strings.Split(out, "\x00")), nil
}

// parseSnapshot builds a snapshot from repository-relative file paths.
func parseSnapshot(paths []string) snapshot {
	s := snapshot{
		qualifying:  map[string]map[string]struct{}{},
		goDirs:      map[string]struct{}{},
		allDirs:     map[string]struct{}{},
		subpackages: map[string][]string{},
	}
	for _, p := range paths {
		rel, ok := strings.CutPrefix(p, goRoot+"/")
		if !ok {
			continue
		}
		dir, base := path.Split(rel)
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" {
			dir = "."
		}
		addAncestors(s.allDirs, dir)
		if !strings.HasSuffix(base, ".go") {
			continue
		}
		s.goDirs[dir] = struct{}{}
		if strings.HasSuffix(base, "_test.go") || skipDir(dir) {
			continue
		}
		if s.qualifying[dir] == nil {
			s.qualifying[dir] = map[string]struct{}{}
		}
		s.qualifying[dir][base] = struct{}{}
	}
	for d := range s.goDirs {
		if d == "." {
			continue
		}
		parent, name := path.Split(d)
		parent = strings.TrimSuffix(parent, "/")
		if parent == "" {
			parent = "."
		}
		if skipName(name) {
			continue
		}
		s.subpackages[parent] = append(s.subpackages[parent], name)
	}
	for _, names := range s.subpackages {
		sort.Strings(names)
	}
	return s
}

// addAncestors records dir and each of its parents (except ".").
func addAncestors(set map[string]struct{}, dir string) {
	if dir == "." {
		set["."] = struct{}{}
		return
	}
	for i := 0; i <= len(dir); i++ {
		if i == len(dir) || dir[i] == '/' {
			set[dir[:i]] = struct{}{}
		}
	}
}

// skipName reports whether one path segment is outside the gate: vendor,
// testdata, generated, or hidden.
func skipName(seg string) bool {
	switch seg {
	case "vendor", "testdata", "generated":
		return true
	}
	return strings.HasPrefix(seg, ".") && seg != "."
}

// skipDir reports whether any segment of a go/-relative directory is skipped.
func skipDir(dir string) bool {
	for seg := range strings.SplitSeq(dir, "/") {
		if skipName(seg) {
			return true
		}
	}
	return false
}

// count returns the non-test .go file count of dir.
func (s snapshot) count(dir string) int { return len(s.qualifying[dir]) }
