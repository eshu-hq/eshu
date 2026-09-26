// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"regexp"
)

// mergeQueueBranchPattern matches the branch GitHub creates for a merge queue
// entry: gh-readonly-queue/<base branch>/pr-<number>-<base sha>. The trailing
// SHA is the commit the group was built on (the group commit's parent).
var mergeQueueBranchPattern = regexp.MustCompile(`^gh-readonly-queue/(.+)/pr-[0-9]+-([0-9a-f]{40})$`)

// mergeGroupBaseSHA returns the fixed base SHA of a merge group from the queue
// branch of its workflow run (workflow_run.head_branch). Unlike the base
// branch name it never moves: once the queue fast-forwards baseRef to the
// group head, comparing against baseRef is empty, while the base SHA still
// yields the diff the queue evaluated (#7281).
//
// The branch must target baseRef; a queue branch for another base is a
// mismatched input, not a diff to evaluate. Every failure is fail-closed and
// distinct from an empty diff, so a run that cannot name its base never passes
// vacuously.
func mergeGroupBaseSHA(mergeGroupBranch, baseRef string) (string, error) {
	match := mergeQueueBranchPattern.FindStringSubmatch(mergeGroupBranch)
	if match == nil {
		return "", fmt.Errorf("cannot determine the merge group base SHA: %q is not a merge queue branch (want gh-readonly-queue/<base>/pr-<n>-<sha>)", mergeGroupBranch)
	}
	if match[1] != baseRef {
		return "", fmt.Errorf("cannot determine the merge group base SHA: queue branch %q targets base %q, not %q", mergeGroupBranch, match[1], baseRef)
	}
	return match[2], nil
}
