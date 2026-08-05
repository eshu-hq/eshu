// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// managedCopyMatchesCommit verifies every copied regular file against an
// immutable commit tree. Missing commit paths are allowed because the managed
// copier intentionally filters files; extra copied paths and submodule content
// fail closed because they cannot be attributed to a blob in the outer tree.
func managedCopyMatchesCommit(
	ctx context.Context,
	sourcePath string,
	targetPath string,
	commitSHA string,
) bool {
	gitDirOutput, err := exec.CommandContext(
		ctx, "git", "-C", sourcePath, "rev-parse", "--absolute-git-dir",
	).Output() // #nosec G204 -- fixed Git query over an internally resolved repository path
	if err != nil {
		return false
	}
	gitDir := strings.TrimSpace(string(gitDirOutput))
	if gitDir == "" {
		return false
	}

	indexFile, err := os.CreateTemp("", "eshu-managed-copy-index-*")
	if err != nil {
		return false
	}
	indexPath := indexFile.Name()
	if err := indexFile.Close(); err != nil {
		_ = os.Remove(indexPath)
		return false
	}
	if err := os.Remove(indexPath); err != nil {
		return false
	}
	defer func() { _ = os.Remove(indexPath) }()

	command := func(args ...string) *exec.Cmd {
		baseArgs := []string{
			"--git-dir", gitDir,
			"--work-tree", targetPath,
			"-c", "core.fileMode=false",
		}
		cmd := exec.CommandContext(ctx, "git", append(baseArgs, args...)...) // #nosec G204 -- fixed Git operations over internally resolved paths and commit identity
		cmd.Env = managedCopyGitEnvironment(indexPath)
		return cmd
	}

	if err := command("read-tree", strings.TrimSpace(commitSHA)).Run(); err != nil {
		return false
	}
	trackedOutput, err := command("ls-files", "-z").Output()
	if err != nil {
		return false
	}
	targetFiles, err := managedCopyRegularFiles(targetPath)
	if err != nil {
		return false
	}
	missing := make([][]byte, 0)
	for _, trackedPath := range bytes.Split(trackedOutput, []byte{0}) {
		if len(trackedPath) == 0 {
			continue
		}
		if _, ok := targetFiles[string(trackedPath)]; !ok {
			missing = append(missing, trackedPath)
		}
	}
	if len(missing) > 0 {
		input := append(bytes.Join(missing, []byte{0}), 0)
		remove := command("update-index", "--force-remove", "-z", "--stdin")
		remove.Stdin = bytes.NewReader(input)
		if err := remove.Run(); err != nil {
			return false
		}
	}
	extraOutput, err := command("ls-files", "--others", "-z", "--").Output()
	if err != nil || len(bytes.Trim(extraOutput, "\x00")) > 0 {
		return false
	}
	if err := command("update-index", "--refresh").Run(); err != nil {
		return false
	}
	return command("diff-files", "--quiet", "--").Run() == nil
}

func managedCopyGitEnvironment(indexPath string) []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "GIT_INDEX_FILE=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "GIT_INDEX_FILE="+indexPath)
}

func managedCopyRegularFiles(root string) (map[string]struct{}, error) {
	files := make(map[string]struct{})
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return os.ErrInvalid
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relativePath)] = struct{}{}
		return nil
	})
	return files, err
}
