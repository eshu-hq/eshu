// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// package cigates (not cigates_test) so this test can call the unexported
// checkVerifyScriptWorkflowMatch directly, the same internal-test-package
// pattern scriptworkflow_soundsubset_test.go already uses in this directory.

// TestCheckVerifyScriptWorkflowMatch_WorkflowsDirUnreadable pins #5939 review
// (P2, Copilot): checkVerifyScriptWorkflowMatch used to silently discard the
// error scriptWorkflowSoundSubset returns on an unreadable .github/workflows
// with a bare `return nil`, so a broken workflows directory read as "no
// drift" -- a false green in exactly the situation a hard failure is wanted.
// This makes the directory genuinely unreadable (not merely absent, which
// legitimately has nothing to check) and asserts the check now fails loudly
// and names the read failure, instead of passing silently.
func TestCheckVerifyScriptWorkflowMatch_WorkflowsDirUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: directory permission bits do not block root reads")
	}

	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// os.ReadDir needs read+execute on the directory itself; stripping all
	// bits makes the directory unreadable without touching its contents.
	if err := os.Chmod(wfDir, 0o000); err != nil {
		t.Fatal(err)
	}
	// Restore permissions before t.TempDir's own cleanup, which needs to walk
	// and remove this directory.
	t.Cleanup(func() { _ = os.Chmod(wfDir, 0o755) })

	reg := &Registry{}
	errs := checkVerifyScriptWorkflowMatch(root, reg)
	if len(errs) != 1 {
		t.Fatalf("want exactly one drift error for an unreadable workflows dir, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "reading workflow-script sound subset") {
		t.Errorf("drift error does not name the read failure: %v", errs[0])
	}
}
