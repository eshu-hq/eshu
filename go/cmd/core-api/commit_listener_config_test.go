// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

// TestCommitListenerMissingCompanionsOrderIsDeterministic is the F4 RED
// test: the missing-variable error must list companions in a fixed order.
// The companions are collected by ranging over a map, so the order is
// random per call. Fifty iterations make a nondeterministic order fail.
func TestCommitListenerMissingCompanionsOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	env := map[string]string{"ESHU_COMMIT_LISTEN": "127.0.0.1:8089"}
	const want = "commit listener ESHU_COMMIT_LISTEN requires " +
		"ESHU_COMMIT_CLAIM_PLANE_URL, ESHU_COMMIT_INSTANCE_ID, " +
		"ESHU_COMMIT_MANIFEST_PATH, ESHU_COMMIT_SCOPE_KIND, " +
		"ESHU_COMMIT_SOURCE_SYSTEM to be set"
	for i := 0; i < 50; i++ {
		_, err := commitListenerConfigFromEnv(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("commitListenerConfigFromEnv() error = nil, want fail-closed")
		}
		if got := err.Error(); got != want {
			t.Fatalf("iteration %d: missing-companions error = %q, want %q", i, got, want)
		}
	}
}
