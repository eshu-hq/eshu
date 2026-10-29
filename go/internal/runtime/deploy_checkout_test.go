// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// expectedDeployCheckoutCommit pins the eshu-hq/eshu-deploy commit the
// repointed contract tests assert against (#8871 in-tree removal). The pin
// advances only with a reviewed deploy cutover; tests fail on any other
// commit instead of asserting against drifted assets.
const expectedDeployCheckoutCommit = "6b6164d4fd95b8959284c5af0ba9a233fe032134"

// deployCheckoutRoot resolves the eshu-deploy checkout every repointed
// contract test reads deploy assets from. It fails closed: an absent
// ESHU_DEPLOY_CHECKOUT, a missing directory, or a commit mismatch fails the
// calling test instead of silently reading the wrong tree.
func deployCheckoutRoot(t *testing.T) string {
	t.Helper()

	dir := os.Getenv("ESHU_DEPLOY_CHECKOUT")
	if dir == "" {
		t.Fatal("ESHU_DEPLOY_CHECKOUT is not set; point it at an eshu-hq/eshu-deploy checkout at " + expectedDeployCheckoutCommit)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("ESHU_DEPLOY_CHECKOUT=%q is not a directory: %v", dir, err)
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ESHU_DEPLOY_CHECKOUT=%q is not a git checkout: %v", dir, err)
	}
	if got := strings.TrimSpace(string(out)); got != expectedDeployCheckoutCommit {
		t.Fatalf("ESHU_DEPLOY_CHECKOUT=%q is at %s, want %s", dir, got, expectedDeployCheckoutCommit)
	}
	return dir
}

// deployComposePath joins a compose-relative path under the pinned checkout.
func deployComposePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(deployCheckoutRoot(t), "compose", name)
}

// deployGitOpsPath joins a gitops-relative path under the pinned checkout.
func deployGitOpsPath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(deployCheckoutRoot(t), "gitops", name)
}

// TestDeployCheckoutRootFailsClosedWithoutEnv proves the deploy-checkout
// helper refuses to guess: without ESHU_DEPLOY_CHECKOUT the calling test
// fails instead of reading the (deleted) in-tree deploy surface.
func TestDeployCheckoutRootFailsClosedWithoutEnv(t *testing.T) {
	t.Parallel()

	out := runDeployCheckoutProbe(t, nil)
	if !strings.Contains(out, "ESHU_DEPLOY_CHECKOUT") {
		t.Fatalf("probe output does not name the required env var:\n%s", out)
	}
}

// TestDeployCheckoutRootRejectsWrongCommit proves the pin is enforced: a
// checkout at any other commit fails instead of asserting against drifted
// deploy assets.
func TestDeployCheckoutRootRejectsWrongCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initGitRepo(t, dir)
	out := runDeployCheckoutProbe(t, map[string]string{"ESHU_DEPLOY_CHECKOUT": dir})
	if !strings.Contains(out, "ESHU_DEPLOY_CHECKOUT") {
		t.Fatalf("probe output does not name the required env var:\n%s", out)
	}
}

// runDeployCheckoutProbe runs the helper probe in a subprocess and returns
// its combined output. The probe must fail (nonzero exit): absent env and
// wrong-pin checkouts are both fatal.
func runDeployCheckoutProbe(t *testing.T, env map[string]string) string {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run", "TestDeployCheckoutHelperProbe$", "-test.v")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GO_WANT_HELPER_PROCESS=1"}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helper probe succeeded, want failure\n%s", out)
	}
	return string(out)
}

// TestDeployCheckoutHelperProbe is the subprocess entry point: it calls the
// helper so the parent can assert the fail-closed behavior end to end.
func TestDeployCheckoutHelperProbe(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		t.Skip("helper probe entry point")
	}
	deployCheckoutRoot(t)
}

// initGitRepo creates a minimal git checkout whose HEAD cannot match the
// pinned deploy commit.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "probe.txt"), []byte("probe"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"add", "-A"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "probe"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
