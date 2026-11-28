// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type dsR7Item struct {
	fileRel string
	lineIdx int
	token   string
	why     string
}

type dsFileScanResult struct {
	path         string
	skipped      bool
	err          error
	read         []string
	probe        []string
	r7           []dsR7Item
	defs         []dsConstDef
	joins        []dsJoinSite
	flowCands    []dsFlowCand
	chainDirUses map[string]bool
	defined      map[string]bool
	bare         []string
	wordLines    map[string]int
}

// dsScan walks the go tree once and classifies every docs-set path literal
// the tests name. It returns the read set, the probe set, and fail-closed
// demands for read sites no rule could resolve.
func dsScan(t *testing.T, root, goRoot string) (map[string]bool, map[string]bool, []string) {
	t.Helper()
	read, probe, unresolved, err := dsScanErr(root, goRoot)
	if err != nil {
		t.Fatal(err)
	}
	return read, probe, unresolved
}

// dsGitUniverse builds the doc basename index and the Go file list from a
// single `git ls-files --cached` snapshot when root lives inside a git work
// tree, reporting gitRoot=false for scratch roots (such as t.TempDir
// fixtures) so the caller falls back to the filesystem walk. Tracked-only is
// the correctness property behind the lockstep guards: the manifest and the
// CI path classification only ever see committed files, so untracked scratch
// copies, ignored kit materializations, and nested checkout roots must not
// move any verdict (#11634).
//
// The snapshot keeps doc-set suffixes for the basename index, skipping
// entries missing from disk so a deleted-but-unstaged file cannot
// phantom-poison basename uniqueness, and Go files under goRoot, skipping
// entries missing from disk for the same reason. When root is a git work tree
// but the snapshot fails or lists no Go files, dsGitUniverse returns an
// error: silently scanning an empty or unreadable tree would disable every
// guard this package exists to enforce.
// dsGitUniverse takes skipGo to keep the scanner's own allowlist data file
// (lockstep_paths_test.go, which names every entry by construction) out of
// the file list, mirroring the filesystem walk it replaces. It also returns
// the tracked set: every index entry root-relative, the universe every
// existence check below must consult instead of the bare disk. The set mirrors the index exactly (no
// existence filter): the index is what CI checks out, so index membership is
// the committable-input truth even for entries missing from this disk.
func dsGitUniverse(root, goRoot string, skipGo func(string) bool) (baseIndex map[string][]string, goFiles []string, tracked map[string]bool, gitRoot bool, err error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("resolve scan root %q: %w", root, err)
	}
	absGoRoot, err := filepath.Abs(goRoot)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("resolve go root %q: %w", goRoot, err)
	}
	run := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", absRoot}, args...)...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	inside := true
	if out, err := run("rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		inside = false
	}
	if _, statErr := os.Lstat(filepath.Join(absRoot, ".git")); statErr != nil && !inside {
		return nil, nil, nil, false, nil
	}
	if !inside {
		return nil, nil, nil, false, fmt.Errorf("scan root %q has a .git entry but is not inside a git work tree", root)
	}
	// Without --full-name, ls-files prints paths relative to the -C
	// directory, so joining with root stays correct even when root is a
	// subdirectory of the work tree or reached through a symlink.
	raw, err := run("ls-files", "--cached", "-z")
	if err != nil {
		return nil, nil, nil, false, err
	}
	baseIndex = map[string][]string{}
	tracked = map[string]bool{}
	for _, f := range strings.Split(raw, "\x00") {
		if f == "" {
			continue
		}
		full := filepath.Join(absRoot, filepath.FromSlash(f))
		rel := filepath.ToSlash(f)
		tracked[rel] = true
		switch {
		case strings.HasSuffix(f, ".md") || strings.HasSuffix(f, ".json") || strings.HasSuffix(f, ".mdc"):
			if st, err := os.Stat(full); err != nil || st.IsDir() {
				continue
			}
			baseIndex[filepath.Base(f)] = append(baseIndex[filepath.Base(f)], rel)
		case strings.HasSuffix(f, ".go"):
			goRel, err := filepath.Rel(absGoRoot, full)
			if err != nil || goRel == ".." || strings.HasPrefix(goRel, ".."+string(filepath.Separator)) {
				continue
			}
			cand := filepath.Join(goRoot, goRel)
			if skipGo != nil && skipGo(cand) {
				continue
			}
			if st, err := os.Stat(full); err != nil || st.IsDir() {
				continue
			}
			goFiles = append(goFiles, cand)
		}
	}
	if len(goFiles) == 0 {
		return nil, nil, nil, false, fmt.Errorf("tracked snapshot for %q lists no Go files under %q", root, goRoot)
	}
	sort.Strings(goFiles)
	return baseIndex, goFiles, tracked, true, nil
}

func dsScanErr(root, goRoot string) (map[string]bool, map[string]bool, []string, error) {
	read := map[string]bool{}
	probe := map[string]bool{}
	// dsAllowlistData reports whether path is the scanner's own allowlist
	// data file. That file names every entry by construction, and its
	// justification prose mentions read helpers; scanning it would report
	// the allowlist itself as findings, so both walks skip it.
	dsAllowlistData := func(path string) bool {
		return strings.HasSuffix(path, "lockstep_paths_test.go")
	}
	mark := func(rel, kind string) {
		if kind == "read" {
			read[rel] = true
			delete(probe, rel)
			return
		}
		if !read[rel] {
			probe[rel] = true
		}
	}
	baseIndex := map[string][]string{}
	var goFiles []string
	// tracked is nil outside a git work tree: scratch roots keep the legacy
	// disk behavior, and every existence check below treats a nil universe
	// as no constraint.
	var tracked map[string]bool
	if indexed, files, universe, gitRoot, err := dsGitUniverse(root, goRoot, dsAllowlistData); err != nil {
		return nil, nil, nil, err
	} else if gitRoot {
		baseIndex, goFiles, tracked = indexed, files, universe
	} else {
		cleanRoot := filepath.Clean(root)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", ".venv", "vendor":
					return filepath.SkipDir
				}
				// A directory carrying its own .git entry is a nested
				// checkout root (a linked worktree or submodule), not part
				// of this tree; descending into it lets another checkout's
				// files poison basename uniqueness.
				if filepath.Clean(p) != cleanRoot {
					if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".mdc") {
				rel, _ := filepath.Rel(root, p)
				rel = filepath.ToSlash(rel)
				base := filepath.Base(p)
				baseIndex[base] = append(baseIndex[base], rel)
			}
			return nil
		})
		walkErr := filepath.WalkDir(goRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || dsAllowlistData(path) {
				return nil
			}
			goFiles = append(goFiles, path)
			return nil
		})
		if walkErr != nil {
			return nil, nil, nil, walkErr
		}
	}

	results := make([]dsFileScanResult, len(goFiles))
	workers := min(max(runtime.GOMAXPROCS(0), 1), max(len(goFiles), 1))
	var nextIdx atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				i := int(nextIdx.Add(1) - 1)
				if i >= len(goFiles) {
					return
				}
				results[i] = dsScanOneFile(root, goFiles[i], baseIndex, tracked)
			}
		}()
	}
	wg.Wait()

	var defs []dsConstDef
	var joins []dsJoinSite
	var flowCands []dsFlowCand
	chainDirUses := map[string]map[string]bool{}
	definedIn := map[string]map[string]bool{}
	codeBare := map[string][]string{}
	type wordLoc struct {
		file string
		line int
	}
	wordCount := map[string]int{}
	wordFirst := map[string]wordLoc{}

	var r7pending []string
	r7seen := map[string]bool{}
	r7add := func(fileRel string, lineIdx int, token, why string) {
		if allowedUnresolvedReads[fileRel+"|"+token] != "" {
			return
		}
		key := fileRel + "|" + token
		if r7seen[key] {
			return
		}
		r7seen[key] = true
		r7pending = append(r7pending, "R7 "+fileRel+":"+strconv.Itoa(lineIdx+1)+" ("+token+"): "+why)
	}

	for i := range results {
		res := &results[i]
		if res.err != nil {
			return nil, nil, nil, res.err
		}
		if res.skipped {
			continue
		}
		for _, rel := range res.read {
			mark(rel, "read")
		}
		for _, rel := range res.probe {
			mark(rel, "probe")
		}
		for _, item := range res.r7 {
			r7add(item.fileRel, item.lineIdx, item.token, item.why)
		}
		defs = append(defs, res.defs...)
		joins = append(joins, res.joins...)
		flowCands = append(flowCands, res.flowCands...)
		if len(res.chainDirUses) > 0 {
			chainDirUses[res.path] = res.chainDirUses
		}
		definedIn[res.path] = res.defined
		codeBare[res.path] = res.bare
		for tok, line := range res.wordLines {
			if line < 0 {
				wordCount[tok] = 2
				continue
			}
			switch wordCount[tok] {
			case 0:
				wordCount[tok] = 1
				wordFirst[tok] = wordLoc{file: res.path, line: line}
			case 1:
				wordCount[tok] = 2
			}
		}
	}
	// R3: a path-named constant referenced anywhere in Go code pins its
	// file. The reference may live in a test, in production, or in another
	// constant alias; any of them couples the file's content to the build.
	// The search runs on string-free lines so a name inside a literal cannot
	// pose as a reference.
	for _, def := range defs {
		cnt := wordCount[def.name]
		if cnt >= 2 {
			mark(def.rel, "read")
			continue
		}
		if cnt == 1 {
			first := wordFirst[def.name]
			if first.file != def.file || first.line != def.lineIdx {
				mark(def.rel, "read")
			}
		}
	}
	// Flow candidates only count when the name is defined in the same file:
	// a struct field that shares an identifier with a read is not a flow.
	for _, d := range defs {
		if definedIn[d.file] == nil {
			definedIn[d.file] = map[string]bool{}
		}
		definedIn[d.file][d.name] = true
	}
	for _, j := range joins {
		if definedIn[j.file] == nil {
			definedIn[j.file] = map[string]bool{}
		}
		definedIn[j.file][j.name] = true
	}
	for _, c := range flowCands {
		// A name already used under a code-tree chain is classified
		// there; its other uses raise their own demands when needed.
		if definedIn[c.file][c.name] && !chainDirUses[c.file][c.name] {
			r7add(c.rel, c.lineIdx, c.raw, "read-flow target unresolvable; classify the caller-relative path")
		}
	}
	// A join site whose line names a constant the scanner already resolved
	// to a read needs no R7: the constant carries the target.
	docConstNames := map[string]bool{}
	for _, def := range defs {
		if read[def.rel] {
			docConstNames[def.name] = true
		}
	}
	// R7a: a path-named Join local that flows into a read on another line is
	// a test input whose target no static rule resolved. Fail closed.
	joinDefAt := map[string]map[int]string{}
	for _, o := range joins {
		if joinDefAt[o.file] == nil {
			joinDefAt[o.file] = map[int]string{}
		}
		joinDefAt[o.file][o.lineIdx] = o.name
	}
	for _, j := range joins {
		if !j.test || j.resolved || j.dirTree != "" {
			continue
		}
		if allowedUnresolvedReads[j.rel+"|"+j.name] != "" {
			continue
		}
		if lines, ok := codeBare[j.file]; ok && j.lineIdx < len(lines) {
			pinned := false
			for _, m := range dsIdent.FindAllStringSubmatch(lines[j.lineIdx], -1) {
				if m[1] != j.name && docConstNames[m[1]] {
					pinned = true
					break
				}
			}
			if pinned {
				continue
			}
		}
		for i, cl := range codeBare[j.file] {
			if i == j.lineIdx {
				continue
			}
			// A second definition of the same name is shadowing or
			// reassignment, not a read of the first value.
			if joinDefAt[j.file][i] == j.name {
				continue
			}
			if (dsReadCall.MatchString(cl) || dsIsJoin(cl)) && !dsWriteCall.MatchString(cl) && dsHasWord(cl, j.name) {
				r7add(j.rel, j.lineIdx, j.name, "join-built path flows into a read at line "+strconv.Itoa(i+1)+"; classify the caller-relative target")
				break
			}
		}
	}
	return read, probe, r7pending, nil
}
