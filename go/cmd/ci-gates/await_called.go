// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "strings"

// calledCheckName is the single allowed Build Test caller alias for a trusted
// workflow filename and its existing API-visible job name.
func calledCheckName(workflow, job string) string {
	base, ok := strings.CutSuffix(workflow, ".yml")
	if !ok || base == "" || strings.ContainsAny(base, "/\\") || job == "" {
		return ""
	}
	return "call-" + base + " / " + job
}
