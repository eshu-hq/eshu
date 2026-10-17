// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRepo runs git against one repository. Every command uses `-C dir`, tree
// object ids, and top-anchored pathspecs, so the working directory and a hook's
// exported GIT_DIR cannot change which tree is read.
type gitRepo struct {
	dir string
	// env holds extra KEY=VALUE entries appended to the process environment.
	env []string
}

// output runs git and returns stdout. A non-zero exit returns a *gitError that
// keeps the exit status and stderr so callers can tell "conflict" (1) from
// "broken" (anything else).
func (g gitRepo) output(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", g.dir}, args...)...) // #nosec G204 -- args are built by this tool from refs and fixed verbs, never shell-interpreted
	if len(g.env) > 0 {
		cmd.Env = append(os.Environ(), g.env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return stdout.String(), &gitError{args: args, code: code, stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return stdout.String(), nil
}

// line runs git and returns the first stdout line, trimmed.
func (g gitRepo) line(args ...string) (string, error) {
	out, err := g.output(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]), nil
}

// resolveCommit returns the commit id ref points at, or an error if ref does
// not resolve to a commit in this checkout.
func (g gitRepo) resolveCommit(ref string) (string, error) {
	return g.line("rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

// gitError is a failed git invocation.
type gitError struct {
	args   []string
	code   int
	stderr string
	err    error
}

func (e *gitError) Error() string {
	msg := fmt.Sprintf("git %s: %v", strings.Join(e.args, " "), e.err)
	if e.stderr != "" {
		msg += ": " + e.stderr
	}
	return msg
}

func (e *gitError) Unwrap() error { return e.err }

// exitCode returns the git exit status carried by err, or -1.
func exitCode(err error) int {
	var ge *gitError
	if errors.As(err, &ge) {
		return ge.code
	}
	return -1
}

// short abbreviates a commit or tree id for operator output.
func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// indexTree returns the tree id of the staged index without taking
// index.lock. `git commit` holds that lock while it runs pre-commit hooks, and
// a plain `git write-tree` needs it whenever the index's cache-tree is stale,
// as it is right after `git add`. The tree is written from a private copy of
// the index instead; the objects land in the shared object store.
func (g gitRepo) indexTree() (string, error) {
	indexPath, err := g.line("rev-parse", "--git-path", "index")
	if err != nil {
		return "", fmt.Errorf("locate the index: %w", err)
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(g.dir, indexPath)
	}
	copyFile, err := os.CreateTemp("", "dirgate-index-*")
	if err != nil {
		return "", fmt.Errorf("copy the index: %w", err)
	}
	defer func() { _ = os.Remove(copyFile.Name()) }()
	if err := copyInto(copyFile, indexPath); err != nil {
		return "", err
	}
	private := gitRepo{dir: g.dir, env: append(append([]string(nil), g.env...), "GIT_INDEX_FILE="+copyFile.Name())}
	tree, err := private.line("write-tree")
	if err != nil {
		return "", fmt.Errorf("write the index tree: %w", err)
	}
	return tree, nil
}

// copyInto copies the file at src into dst and closes dst.
func copyInto(dst *os.File, src string) error {
	in, err := os.Open(src) // #nosec G304 -- src comes from `git rev-parse --git-path index`, not user input
	if err != nil {
		_ = dst.Close()
		return fmt.Errorf("open the index: %w", err)
	}
	defer func() { _ = in.Close() }()
	if _, err := io.Copy(dst, in); err != nil {
		_ = dst.Close()
		return fmt.Errorf("copy the index: %w", err)
	}
	return dst.Close()
}
