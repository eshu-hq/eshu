// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitsubmodule

import (
	"context"
	"os/exec"
	"strings"
)

// gitlinkTreeMode is the git tree-entry mode reserved for a gitlink: a
// commit SHA recorded directly in the parent tree, rather than a blob or a
// nested tree. Only an entry with this exact mode is a pinned submodule
// commit (https://git-scm.com/docs/gitmodules, "git-config" mode 160000).
const gitlinkTreeMode = "160000"

// gitSubmoduleGitlinkSHA resolves the pinned commit SHA for the gitlink tree
// entry at submodulePath in repoPath's selected commit. An empty commitSHA
// preserves the legacy HEAD fallback (issue #5420 Phase 2b).
//
// It reads `git ls-tree <commit> -- <submodulePath>` rather than `git rev-parse
// <commit>:<submodulePath>` so the tree entry's mode can be checked: only a
// mode 160000 (gitlinkTreeMode) entry is a pinned submodule commit. A regular
// directory or file that happens to share the declared ".gitmodules" path
// resolves to a different mode and must not be reported as a pin.
//
// This is a purely local tree read on an already-cloned repository — no
// network access, no credentials — so it shells out directly the same way
// gitCommitSHA does (see git_snapshot_materialization.go), rather than
// through gitRun/gitCommandEnv, which exist to thread RepoSyncConfig/token
// auth material into commands that talk to a remote (fetch/clone).
//
// It returns nil (never guesses) when:
//   - the git invocation fails, including an invalid selected commit, an
//     unborn HEAD fallback, or repoPath not being a git repository at all;
//   - ls-tree returns no entry for submodulePath — a ".gitmodules" entry
//     declared but never `git submodule add`ed (or whose path no longer
//     exists) has no gitlink to resolve; or
//   - the resolved entry's mode is not exactly gitlinkTreeMode.
//
// A gitlink is read from the committed TREE, not the working directory, so
// an uninitialized submodule (an empty checkout, but the gitlink IS
// committed) still resolves correctly — SnapshotRepository never needs the
// submodule's working-tree contents for this fact.
func GitSubmoduleGitlinkSHA(ctx context.Context, repoPath, commitSHA, submodulePath string) *string {
	treeish := strings.TrimSpace(commitSHA)
	if treeish == "" {
		treeish = "HEAD"
	}
	command := exec.CommandContext(ctx, "git", "-C", repoPath, "ls-tree", treeish, "--", submodulePath) // #nosec G204 -- runs git with internally selected commit and resolved local repository path
	output, err := command.Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		if sha, ok := parseGitlinkTreeLine(line); ok {
			return &sha
		}
	}
	return nil
}

// parseGitlinkTreeLine parses one line of `git ls-tree` output
// ("<mode> <type> <sha>\t<path>") and reports the entry's SHA when its mode
// is exactly gitlinkTreeMode. Any other shape — including a mode/type that
// marks a regular blob or nested tree — is reported as not-a-gitlink so the
// caller never guesses a pinned commit from a non-gitlink entry.
func parseGitlinkTreeLine(line string) (sha string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	tabIdx := strings.IndexByte(line, '\t')
	if tabIdx < 0 {
		return "", false
	}
	fields := strings.Fields(line[:tabIdx])
	if len(fields) != 3 {
		return "", false
	}
	mode, objType, entrySHA := fields[0], fields[1], fields[2]
	if mode != gitlinkTreeMode || objType != "commit" || entrySHA == "" {
		return "", false
	}
	return entrySHA, true
}
