// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import (
	"fmt"
	"os/exec"
	"strings"
)

// GitDiffNameStatus runs `git diff --name-status` in repoPath and returns the
// changed files it names.
//
// Rename and copy detection are requested explicitly (--find-renames,
// --find-copies, --find-copies-harder) because the pre-change impact API lines
// up evidence across a move only when it gets both endpoints; without them git
// reports a rename as an unrelated add and delete and the old path's evidence
// is lost.
//
// Ref handling matches git's own positional form: both refs given compares
// them, one ref given compares the working tree against it, and neither given
// diffs the working tree against the index. The trailing "--" ends option
// parsing so a ref that looks like a flag cannot be read as one.
func GitDiffNameStatus(repoPath, baseRef, headRef string) ([]FileChange, error) {
	args := []string{"-C", repoPath, "diff", "--name-status", "--find-renames", "--find-copies", "--find-copies-harder"}
	switch {
	case baseRef != "" && headRef != "":
		args = append(args, baseRef, headRef)
	case baseRef != "":
		args = append(args, baseRef)
	case headRef != "":
		args = append(args, headRef)
	}
	args = append(args, "--")
	out, err := exec.Command("git", args...).Output() // #nosec G204 -- fixed binary "git"; args are program-constructed from flag values (refs and "--"), not arbitrary user strings
	if err != nil {
		return nil, fmt.Errorf("derive git diff: %w", err)
	}
	return ParseNameStatusDiff(string(out)), nil
}

// ParseNameStatusDiff turns `git diff --name-status` output into FileChange
// rows. Lines with fewer than two tab-separated fields are skipped, which
// covers the trailing empty line every git run ends with.
//
// For a rename or a copy git prints three fields -- status, source, target --
// and this reads them in that order: field 1 becomes OldPath and field 2
// becomes Path. Reading them the other way round would hand the API a plausible
// pair pointing backwards in time.
func ParseNameStatusDiff(output string) []FileChange {
	changes := []FileChange{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) < 2 {
			continue
		}
		status := NormalizeStatus(fields[0])
		change := FileChange{Path: fields[1], Status: status}
		if (status == "renamed" || status == "copied") && len(fields) >= 3 {
			change.OldPath = fields[1]
			change.Path = fields[2]
		}
		changes = append(changes, change)
	}
	return changes
}

// NormalizeStatus maps a git status letter to the word the pre-change impact
// API expects.
//
// R and C carry a similarity score (R100, C85), so they are matched by prefix
// rather than equality. Everything this does not recognize -- M, T, U, X, and
// any letter git adds later -- becomes "modified", which is the safe default:
// it asks the API to look at the file rather than to assume it appeared or
// vanished.
func NormalizeStatus(status string) string {
	status = strings.ToUpper(strings.TrimSpace(status))
	switch {
	case status == "A":
		return "added"
	case status == "D":
		return "deleted"
	case strings.HasPrefix(status, "R"):
		return "renamed"
	case strings.HasPrefix(status, "C"):
		return "copied"
	default:
		return "modified"
	}
}

// ModifiedFiles turns explicitly listed paths (`--file`) into FileChange rows.
// They are all "modified": an operator naming a path is telling the CLI to look
// at it, not what git did to it.
func ModifiedFiles(paths []string) []FileChange {
	changes := make([]FileChange, 0, len(paths))
	for _, value := range CleanValues(paths) {
		changes = append(changes, FileChange{Path: value, Status: "modified"})
	}
	return changes
}

// ChangedPaths projects the target path out of each change, dropping blanks.
// For a rename this is the new path; the old one still travels in the
// FileChange row alongside it.
func ChangedPaths(changes []FileChange) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if strings.TrimSpace(change.Path) != "" {
			paths = append(paths, change.Path)
		}
	}
	return CleanValues(paths)
}

// CleanValues trims each value and drops the ones that are empty afterwards.
// It always returns a non-nil slice, so an all-blank input serializes as []
// rather than null and the API sees "no paths" instead of a missing field.
func CleanValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}
