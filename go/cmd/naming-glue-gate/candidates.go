// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// minCandidateNameLen is the shortest basename this gate will send to the
// model. It is a length floor, not a word list: below it, a two-real-word
// glue is implausible ("aws", "gcp") and every char of quota is better spent
// on names long enough to actually be ambiguous.
const minCandidateNameLen = 8

// Candidate is one newly introduced directory this gate asks the model to
// classify.
type Candidate struct {
	Path string // repo-relative, e.g. "go/internal/reducer/workloadinstance"
	Name string // basename, e.g. "workloadinstance"
}

// newDirectoriesFromLists returns the directories present in cur but not in
// old, filtered to the shape naming.md rule 3 actually targets: a new,
// lowercase, unseparated, sufficiently long basename. It is the pure core
// behind GitNewDirectories, independent of git so it is unit-testable
// without a repository.
//
// Filtered out, mirroring scripts/verify-filename-stutter.sh's own
// exemptions plus this gate's narrower scope:
//   - any path under a testdata/ or fixtures/ root (fixture corpora mirror
//     third-party conventions this repo does not control);
//   - dot-directories (tool-owned, not ours to name);
//   - a basename containing '_' or '-' (already word-separated, so it is
//     not the no-boundary glue shape rule 3 forbids);
//   - a basename containing an uppercase letter (not the lowercase glue
//     shape either -- most often a fixture mirroring an external name);
//   - a basename shorter than minCandidateNameLen.
func newDirectoriesFromLists(old, cur []string) []Candidate {
	existing := make(map[string]bool, len(old))
	for _, d := range old {
		existing[d] = true
	}

	var out []Candidate
	for _, d := range cur {
		if existing[d] {
			continue
		}
		if isUnderFixtureRoot(d) {
			continue
		}
		name := basename(d)
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !isGlueCandidateShape(name) {
			continue
		}
		out = append(out, Candidate{Path: d, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// isUnderFixtureRoot reports whether any component of path is "testdata" or
// "fixtures" -- everything at or below such a root is exempt, same rule
// verify-filename-stutter.sh already applies.
func isUnderFixtureRoot(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == "testdata" || part == "fixtures" {
			return true
		}
	}
	return false
}

// isGlueCandidateShape reports whether name has the shape a glued compound
// takes in this repo: lowercase letters only (no separator, no digit, no
// uppercase), at least minCandidateNameLen long.
func isGlueCandidateShape(name string) bool {
	if len(name) < minCandidateNameLen {
		return false
	}
	for _, r := range name {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// basename returns the final path component.
func basename(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// GitRunner executes git commands against a repository root. Production
// code uses realGitRunner; tests inject a fake so this package's tests never
// touch a real .git tree.
type GitRunner interface {
	// Directories lists every directory (recursively) under root/dir as
	// it exists at ref, repo-relative paths.
	Directories(ref, dir string) ([]string, error)
}

// realGitRunner shells out to the git binary.
type realGitRunner struct {
	repoRoot string
}

// NewGitRunner returns a GitRunner backed by the git binary invoked with
// repoRoot as its working directory.
func NewGitRunner(repoRoot string) GitRunner {
	return realGitRunner{repoRoot: repoRoot}
}

func (g realGitRunner) Directories(ref, dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-tree", "-d", "-r", "--name-only", ref, "--", dir) // #nosec G204 -- ref/dir are caller-controlled CLI flags, not external input.
	cmd.Dir = g.repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-tree -d -r --name-only %s -- %s: %w (%s)", ref, dir, err, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}

// NewDirectories returns the newly introduced directories under dirs
// between baseRef and headRef, using runner to read each ref's tree.
func NewDirectories(runner GitRunner, baseRef, headRef string, dirs []string) ([]Candidate, error) {
	var old, cur []string
	for _, dir := range dirs {
		o, err := runner.Directories(baseRef, dir)
		if err != nil {
			return nil, fmt.Errorf("reading %s at %s: %w", dir, baseRef, err)
		}
		old = append(old, o...)
		c, err := runner.Directories(headRef, dir)
		if err != nil {
			return nil, fmt.Errorf("reading %s at %s: %w", dir, headRef, err)
		}
		cur = append(cur, c...)
	}
	return newDirectoriesFromLists(old, cur), nil
}
