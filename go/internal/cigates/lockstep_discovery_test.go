// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"io/fs"
	"path/filepath"
	"runtime"
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
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".venv", "vendor":
				return filepath.SkipDir
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
	var goFiles []string
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
				results[i] = dsScanOneFile(root, goFiles[i], baseIndex)
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
