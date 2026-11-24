// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestPlanGoReachabilityScopeRefusesMissingFlags proves the one-shot validates
// its required flags before opening any connection: a missing flag usage-errors
// without touching the database or the config.
func TestPlanGoReachabilityScopeRefusesMissingFlags(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runPlanGoReachabilityScope(context.Background(), []string{"--instance-id", "only-this"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--request-id") {
		t.Fatalf("stderr = %q, want the required-flags usage", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no outcome document on a usage error", stdout.String())
	}
}
