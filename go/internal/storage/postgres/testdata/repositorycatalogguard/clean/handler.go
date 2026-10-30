// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package clean is a planted repository-catalog consumer for the #9567
// guard's GREEN case. It names only identity keys the catalog keeps. It is
// testdata and is never built into a binary.
package clean

// Reads returns two identity keys the catalog projection keeps.
func Reads() (string, string) {
	return "graph_id", "remote_url"
}
