// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifarepodependencyproof

package main

// loadIfaRepoDependencyProofWorkers excludes the proof-only worker knob from
// normal and production builds. It deliberately does not call getenv.
func loadIfaRepoDependencyProofWorkers(_ func(string) string) int {
	return 1
}
