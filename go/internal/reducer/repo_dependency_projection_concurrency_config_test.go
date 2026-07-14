// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestRepoDependencyProjectionRunnerWorkerCountDefaults(t *testing.T) {
	t.Parallel()

	var cfg RepoDependencyProjectionRunnerConfig
	if got := cfg.workerCount(); got != 1 {
		t.Fatalf("workerCount() = %d, want 1", got)
	}
}
