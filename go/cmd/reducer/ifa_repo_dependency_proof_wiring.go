// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package main

const ifaRepoDependencyProofWorkersEnv = "ESHU_IFA_REPO_DEPENDENCY_PROOF_WORKERS"

// loadIfaRepoDependencyProofWorkers reads the proof-only worker count. The
// build tag keeps this knob unreachable from normal and production binaries.
func loadIfaRepoDependencyProofWorkers(getenv func(string) string) int {
	return loadPositiveIntOrDefault(getenv, ifaRepoDependencyProofWorkersEnv, 1)
}
