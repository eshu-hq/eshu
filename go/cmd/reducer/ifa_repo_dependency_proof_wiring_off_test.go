// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifarepodependencyproof

package main

import "testing"

func TestIfaRepoDependencyProofWorkersExcludedByDefault(t *testing.T) {
	t.Parallel()

	workers := loadIfaRepoDependencyProofWorkers(func(name string) string {
		t.Fatalf("normal build read proof-only environment variable %q", name)
		return ""
	})
	if workers != 1 {
		t.Fatalf("workers = %d, want 1", workers)
	}
}
