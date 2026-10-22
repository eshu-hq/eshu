// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval

import "os/exec"

// goListDeps lists the non-test dependencies of this package. The harness
// itself must not reach the mcp root; the registry-backed test lives in the
// external eval_test package, which is allowed to.
func goListDeps() ([]byte, error) {
	return exec.Command("go", "list", "-deps", ".").CombinedOutput()
}
