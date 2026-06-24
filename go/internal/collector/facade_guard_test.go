// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestNoMockOnlyGoogleWorkspaceCollectorPackage(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir("googleworkspace")
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read googleworkspace collector facade directory: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			t.Fatalf("mock-only Google Workspace collector facade source %q exists without a live implementation", entry.Name())
		}
	}
}
