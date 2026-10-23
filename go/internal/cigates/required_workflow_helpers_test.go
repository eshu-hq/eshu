// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublisherProbeCodeIsNotARealAwaitExitCode keeps the unclassified probe
// unclassified. If it ever collided with a real await exit code, the `*)` arm
// would stop being exercised and the arm an operator meets when something
// unanticipated happens would go unchecked.
func TestPublisherProbeCodeIsNotARealAwaitExitCode(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		awaitExitPassedCode,
		awaitExitGateFailedCode,
		awaitExitStillRunningCode,
		awaitExitBrokenCode,
		awaitExitGateCancelledCode,
		awaitExitReusedCode,
		awaitExitSmokePassedCode,
	} {
		if publisherUnclassifiedProbeCode == code {
			t.Fatalf("publisherUnclassifiedProbeCode %d collides with a real await exit code",
				publisherUnclassifiedProbeCode)
		}
	}
}

// repositoryRoot returns the working tree root, so a test can read a
// committed file without depending on where `go test` was invoked from.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// writeWorkflowFile writes a workflow body under root and returns its path.
func writeWorkflowFile(t *testing.T, root, body string) string {
	t.Helper()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "required-gates.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// repositoryRequiredGatesWorkflow returns the committed required-gates
// workflow path.
func repositoryRequiredGatesWorkflow(t *testing.T) string {
	t.Helper()
	return filepath.Join(repositoryRoot(t), ".github", "workflows", "required-gates.yml")
}
