// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewAttestCaptureAndVerifyUnchangedInputs(t *testing.T) {
	t.Parallel()
	fixture := newReviewAttestFixture(t)

	fixture.run(t, "capture")
	output := fixture.run(t, "verify")
	if !strings.Contains(output, "PASS: review attestation matches exact inputs") {
		t.Fatalf("verify output = %q", output)
	}
	info, err := os.Stat(fixture.receipt)
	if err != nil {
		t.Fatalf("stat receipt: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode = %o, want 600", info.Mode().Perm())
	}
}

func TestReviewAttestRejectsDirtyWorktree(t *testing.T) {
	t.Parallel()
	fixture := newReviewAttestFixture(t)
	fixture.run(t, "capture")
	writeTestFile(t, filepath.Join(fixture.repo, "product.txt"), "dirty\n")

	output, err := fixture.command("verify").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "working tree is not clean") {
		t.Fatalf("verify error = %v, output = %s", err, output)
	}
}

func TestReviewAttestRejectsChangedClaims(t *testing.T) {
	t.Parallel()
	fixture := newReviewAttestFixture(t)
	fixture.run(t, "capture")
	writeTestFile(t, fixture.claims, "changed title\x00changed body\n")

	output, err := fixture.command("verify").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "claims_sha256 changed") {
		t.Fatalf("verify error = %v, output = %s", err, output)
	}
}

func TestReviewAttestRejectsNewHeadCommit(t *testing.T) {
	t.Parallel()
	fixture := newReviewAttestFixture(t)
	fixture.run(t, "capture")
	writeTestFile(t, filepath.Join(fixture.repo, "product.txt"), "second\n")
	fixture.git(t, "add", "product.txt")
	fixture.git(t, "commit", "-m", "second change")

	output, err := fixture.command("verify").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "head_commit changed") {
		t.Fatalf("verify error = %v, output = %s", err, output)
	}
}

type reviewAttestFixture struct {
	repo    string
	claims  string
	packet  string
	verdict string
	receipt string
	binary  string
}

func newReviewAttestFixture(t *testing.T) reviewAttestFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	fixture := reviewAttestFixture{
		repo:    repo,
		claims:  filepath.Join(root, "claims.bin"),
		packet:  filepath.Join(root, "packet.md"),
		verdict: filepath.Join(root, "verdict.md"),
		receipt: filepath.Join(root, "receipt.json"),
		binary:  buildBinary(t),
	}
	fixture.git(t, "init", "-b", "main")
	fixture.git(t, "config", "user.name", "review-test")
	fixture.git(t, "config", "user.email", "review-test@example.invalid")
	writeTestFile(t, filepath.Join(repo, "product.txt"), "base\n")
	fixture.git(t, "add", "product.txt")
	fixture.git(t, "commit", "-m", "base")
	fixture.git(t, "checkout", "-b", "feature")
	writeTestFile(t, filepath.Join(repo, "product.txt"), "feature\n")
	fixture.git(t, "add", "product.txt")
	fixture.git(t, "commit", "-m", "feature")
	writeTestFile(t, fixture.claims, "title\x00body\n")
	writeTestFile(t, fixture.packet, "review packet\n")
	writeTestFile(t, fixture.verdict, "P0=0 P1=0 P2-blocking=0\n")
	return fixture
}

func (f reviewAttestFixture) command(action string) *exec.Cmd {
	return exec.Command(
		f.binary,
		"review-attest", action,
		"--repo-root", f.repo,
		"--base", "main",
		"--claims-file", f.claims,
		"--review-packet", f.packet,
		"--verdict", f.verdict,
		"--receipt", f.receipt,
	)
}

func (f reviewAttestFixture) run(t *testing.T, action string) string {
	t.Helper()
	output, err := f.command(action).CombinedOutput()
	if err != nil {
		t.Fatalf("review-attest %s: %v\n%s", action, err, output)
	}
	return string(output)
}

func (f reviewAttestFixture) git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.repo}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
