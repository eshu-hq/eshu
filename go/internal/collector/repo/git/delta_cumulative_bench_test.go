// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// benchGit runs git for a benchmark fixture and returns trimmed stdout. Auto
// maintenance and auto gc are off: since Git 2.47 a fetch starts a detached
// `git maintenance run --auto` that keeps writing objects after the command
// returns, which races the TempDir cleanup and the next run's fixture.
func benchGit(b *testing.B, dir string, args ...string) string {
	b.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- benchmark fixture, controlled args
	cmd.Env = append(append(os.Environ(), scratchGitEnv...),
		"GIT_AUTHOR_NAME=eshu", "GIT_AUTHOR_EMAIL=eshu@example.com", "GIT_COMMITTER_NAME=eshu", "GIT_COMMITTER_EMAIL=eshu@example.com")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		b.Fatalf("git %v: %v\nstderr: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out))
}

// newChainBenchFixture builds a 500-file repository with commits F (full),
// c1 (the active delta: 20 files changed, 5 added, 5 deleted), and C (the
// remote head: 20 more files changed), cloned at c1 with full history.
func newChainBenchFixture(b *testing.B) (checkout, full, active string) {
	b.Helper()
	root := b.TempDir()
	src, remote := filepath.Join(root, "src"), filepath.Join(root, "remote.git")
	checkout = filepath.Join(root, "checkout")
	benchGit(b, root, "init", "-q", "-b", "main", src)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o600); err != nil {
			b.Fatalf("write %s: %v", name, err)
		}
	}
	commit := func(message string) string {
		benchGit(b, src, "add", "-A")
		benchGit(b, src, "commit", "-qm", message)
		return benchGit(b, src, "rev-parse", "HEAD")
	}
	for i := range 500 {
		write(fmt.Sprintf("f%03d.go", i), fmt.Sprintf("package p\n\nfunc f%03d() {}\n", i))
	}
	full = commit("F")
	for i := range 20 {
		write(fmt.Sprintf("f%03d.go", i), fmt.Sprintf("package p\n\nfunc f%03dc1() {}\n", i))
	}
	for i := range 5 {
		write(fmt.Sprintf("n%03d.go", i), "package p\n")
		benchGit(b, src, "rm", "-q", fmt.Sprintf("f%03d.go", 499-i))
	}
	active = commit("c1")
	benchGit(b, root, "clone", "-q", "--bare", src, remote)
	benchGit(b, root, "clone", "-q", "file://"+remote, checkout)
	// updateRepository runs its own git with the process environment, so the
	// checkout and remote also carry the setting in their repository config.
	for _, repo := range []string{remote, checkout} {
		benchGit(b, repo, "config", "maintenance.auto", "false")
		benchGit(b, repo, "config", "gc.auto", "0")
	}
	for i := 20; i < 40; i++ {
		write(fmt.Sprintf("f%03d.go", i), fmt.Sprintf("package p\n\nfunc f%03dC() {}\n", i))
	}
	commit("C")
	benchGit(b, remote, "fetch", "-q", src, "main:main")
	return checkout, full, active
}

// BenchmarkUpdateRepositoryChainedDelta measures one steady-state delta sync
// of a chained scope (#8780 no-regression): fetch, the baseline decision, the
// diff work, and the checkout. A cumulative delta runs two diffs and one
// ls-tree where the active-commit diff ran one diff.
func BenchmarkUpdateRepositoryChainedDelta(b *testing.B) {
	checkout, full, active := newChainBenchFixture(b)
	config := RepoSyncConfig{CloneDepth: 50, GitAuthMethod: "none"}
	baseline := chainBenchBaseline(full, active)
	b.ResetTimer()
	for range b.N {
		updated, delta, _, err := updateRepository(context.Background(), config, checkout, "", discardLogger(), baselineTestEvent(),
			baseline, nil, "")
		if err != nil || !updated || delta.IsEmpty() {
			b.Fatalf("updateRepository() updated=%v err=%v delta=%+v, want a delta", updated, err, delta)
		}
	}
}
