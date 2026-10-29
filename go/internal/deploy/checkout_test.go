// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deploy

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// expectedDeployCheckoutCommit pins the eshu-hq/eshu-deploy commit the
// repointed boundary test asserts against (#8871 in-tree removal). It must
// match the twin pin in go/internal/runtime/deploy_checkout_test.go: both
// helpers fail closed on any other commit, so a skew between the twins
// surfaces as a test failure, not a silent drift.
const expectedDeployCheckoutCommit = "6b6164d4fd95b8959284c5af0ba9a233fe032134"

// deployCheckoutForContract resolves the eshu-deploy checkout the boundary
// test reads deploy assets from. It fails closed: an absent
// ESHU_DEPLOY_CHECKOUT, a missing directory, or a commit mismatch fails the
// calling test instead of silently reading the wrong tree.
func deployCheckoutForContract(t *testing.T) string {
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

// TestDeployCheckoutForContractFailsClosedWithoutEnv proves the boundary
// test refuses to guess: without ESHU_DEPLOY_CHECKOUT the probe fails
// naming the required variable.
func TestDeployCheckoutForContractFailsClosedWithoutEnv(t *testing.T) {
	t.Parallel()

	cmd := exec.Command(os.Args[0], "-test.run", "TestDeployCheckoutContractProbe$", "-test.v")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GO_WANT_HELPER_PROCESS=1"}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helper probe succeeded, want failure\n%s", out)
	}
	if !strings.Contains(string(out), "ESHU_DEPLOY_CHECKOUT") {
		t.Fatalf("probe output does not name the required env var:\n%s", out)
	}
}

// TestDeployCheckoutContractProbe is the subprocess entry point: it calls
// the helper so the parent can assert the fail-closed behavior end to end.
func TestDeployCheckoutContractProbe(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		t.Skip("helper probe entry point")
	}
	deployCheckoutForContract(t)
}
