// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
)

func TestRemoteE2ERuntimeStateVerifierIncludesSecurityAlertCollector(t *testing.T) {
	t.Parallel()

	script := readRepositoryFile(t, "../../..", "scripts/verify_remote_e2e_runtime_state.sh")
	if !strings.Contains(script, "collector-security-alerts") {
		t.Fatal("verify_remote_e2e_runtime_state.sh must require collector-security-alerts by default")
	}
}
