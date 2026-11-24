// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"strings"
	"testing"
)

// TestSingleRepositoryQueryKeepsRotationPredicates pins the single-repository
// read against the rotation page contract: the same candidate predicates, the
// same go.mod and Go-evidence filters, the same active-generation commit join,
// and a total-candidates count over the same candidate set the page windows.
// A drift here would plan a repository the rotation would never visit, or
// fence with a wrong slot count.
func TestSingleRepositoryQueryKeepsRotationPredicates(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"source_system = 'git'",
		"scope_kind = 'repository'",
		"active_generation_id IS NOT NULL",
		"source_key = $1",
		"relative_path = 'go.mod'",
		"relative_path LIKE '%/go.mod'",
		"vulnerability.go_module_evidence",
		"vulnerability.go_call_reachability",
		"COUNT(*)",
	} {
		if !strings.Contains(singleRepositoryQuery, want) {
			t.Errorf("singleRepositoryQuery is missing %q", want)
		}
	}
	if strings.Contains(singleRepositoryQuery, "$2") {
		t.Error("singleRepositoryQuery takes only the repository id; want no $2 page parameter")
	}
}
