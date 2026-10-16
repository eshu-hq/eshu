// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The two baselines record what already existed when the gate's absolute
// invariant was introduced. Both are write-once.
const (
	baselineCapFile    = "go/cmd/dirgate/baseline-cap.tsv"
	baselineNamingFile = "go/cmd/dirgate/baseline-naming.tsv"
	baselineCapName    = "baseline-cap.tsv"
	baselineNamingName = "baseline-naming.tsv"
)

// blob is one file read from a git tree.
type blob struct {
	exists  bool
	content string
}

// baselines holds the parsed baselines plus the raw files, so the write-once
// rule can compare bytes.
type baselines struct {
	// cap maps a directory to its non-test .go count when the baseline was cut.
	cap map[string]int
	// naming maps a directory to the colliding files that were already there.
	naming map[string]map[string]struct{}
	// rawCap and rawNaming are the files as read.
	rawCap, rawNaming blob
	// problems lists malformed rows; each is a RED finding.
	problems []string
}

// readBlob reads path from tree. A missing path is not an error.
func readBlob(g gitRepo, tree, path string) (blob, error) {
	listed, err := g.output("ls-tree", "--name-only", "--full-tree", tree, "--", path)
	if err != nil {
		return blob{}, fmt.Errorf("look for %s in %s: %w", path, short(tree), err)
	}
	if strings.TrimSpace(listed) == "" {
		return blob{}, nil
	}
	content, err := g.output("cat-file", "blob", tree+":"+path)
	if err != nil {
		return blob{}, fmt.Errorf("read %s in %s: %w", path, short(tree), err)
	}
	return blob{exists: true, content: content}, nil
}

// loadBaselines reads and parses both baselines from tree, or from dir on the
// local filesystem when dir is set (replay and debugging only).
func loadBaselines(g gitRepo, tree, dir string) (baselines, error) {
	var capBlob, namingBlob blob
	var err error
	if dir != "" {
		capBlob, err = readLocalBlob(filepath.Join(dir, baselineCapName))
		if err == nil {
			namingBlob, err = readLocalBlob(filepath.Join(dir, baselineNamingName))
		}
	} else {
		capBlob, err = readBlob(g, tree, baselineCapFile)
		if err == nil {
			namingBlob, err = readBlob(g, tree, baselineNamingFile)
		}
	}
	if err != nil {
		return baselines{}, err
	}
	b := baselines{cap: map[string]int{}, naming: map[string]map[string]struct{}{}, rawCap: capBlob, rawNaming: namingBlob}
	for _, row := range tsvRows(capBlob.content) {
		fields := strings.Split(row, "\t")
		n, convErr := 0, error(nil)
		if len(fields) == 2 {
			n, convErr = strconv.Atoi(fields[1])
		}
		if len(fields) != 2 || convErr != nil || n < 1 {
			b.problems = append(b.problems, fmt.Sprintf("%s: malformed row %q (want dir<TAB>count)", baselineCapFile, row))
			continue
		}
		b.cap[fields[0]] = n
	}
	for _, row := range tsvRows(namingBlob.content) {
		fields := strings.Split(row, "\t")
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			b.problems = append(b.problems, fmt.Sprintf("%s: malformed row %q (want dir<TAB>file)", baselineNamingFile, row))
			continue
		}
		if b.naming[fields[0]] == nil {
			b.naming[fields[0]] = map[string]struct{}{}
		}
		b.naming[fields[0]][fields[1]] = struct{}{}
	}
	return b, nil
}

func readLocalBlob(path string) (blob, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path comes from the -baseline-dir flag, an operator replay option
	if os.IsNotExist(err) {
		return blob{}, nil
	}
	if err != nil {
		return blob{}, fmt.Errorf("read %s: %w", path, err)
	}
	return blob{exists: true, content: string(data)}, nil
}

// tsvRows returns the data rows of a TSV: no blank lines, no # comments.
func tsvRows(content string) []string {
	var rows []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// writeOnceFindings makes both baselines write-once. A file that exists at base
// must be byte-identical at head: modified, emptied, or deleted is RED, and no
// trailer waives it. A file absent at base may be created freely (bootstrap).
func writeOnceFindings(base, head baselines) []finding {
	var out []finding
	for _, f := range []struct {
		path       string
		base, head blob
	}{
		{baselineCapFile, base.rawCap, head.rawCap},
		{baselineNamingFile, base.rawNaming, head.rawNaming},
	} {
		if !f.base.exists || (f.head.exists && f.head.content == f.base.content) {
			continue
		}
		what := "modified"
		if !f.head.exists {
			what = "deleted"
		}
		out = append(out, finding{sev: sevRed, kind: kindBaseline, file: f.path, msg: fmt.Sprintf(
			"RED %s was %s. The baselines are write-once: they exist at the base, so they must stay byte-identical. "+
				"A dead row is harmless because the growth rule still guards every merge, and no trailer waives this.", f.path, what)})
	}
	for _, p := range head.problems {
		out = append(out, finding{sev: sevRed, kind: kindBaseline, msg: "RED " + p})
	}
	return out
}

const baselineHeader = "# dirgate baseline (go/cmd/dirgate). Generated by `dirgate baseline` from a tree; do not edit.\n" +
	"# Write-once: once this file exists at the base of a change, the gate fails any diff to it.\n" +
	"# Rows are never removed. A row that no longer matches the tree is harmless: the delta rule still guards every merge.\n"

// generateBaselines renders both baseline files from a tree's census,
// deterministically: rows sorted bytewise, no timestamps, no commit ids.
func generateBaselines(s snapshot) (capTSV, namingTSV string) {
	var capB, namingB strings.Builder
	capB.WriteString(baselineHeader)
	capB.WriteString("# Columns (tab-separated): dir<TAB>count. dir is relative to go/ and held more than 40 non-test .go files.\n")
	dirs := make([]string, 0, len(s.qualifying))
	for d := range s.qualifying {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if n := s.count(d); n > maxDirFiles {
			fmt.Fprintf(&capB, "%s\t%d\n", d, n)
		}
	}
	namingB.WriteString(baselineHeader)
	namingB.WriteString("# Columns (tab-separated): dir<TAB>file. file collided with a sibling subpackage of dir.\n")
	for _, c := range collisionsOf(s) {
		fmt.Fprintf(&namingB, "%s\t%s\n", c.dir, c.file)
	}
	return capB.String(), namingB.String()
}
