// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "github.com/eshu-hq/eshu/go/internal/cigates"

// calledCheckName is the single allowed Build Test caller alias for a trusted
// workflow filename and its existing API-visible job name.
func calledCheckName(workflow, job string) string {
	base, ok := cigates.PRWorkflowStem(workflow)
	if !ok || job == "" {
		return ""
	}
	return "call-" + base + " / " + job
}
