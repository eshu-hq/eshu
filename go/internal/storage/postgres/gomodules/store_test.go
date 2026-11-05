// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
)

// TestPageQueryScopePrefixMatchesTheScannerContract pins the SQL literal against
// the scope id the planner mints. If they drifted, has_go_evidence would never
// match, every lost go.mod would strand its evidence, and no test of planning
// would notice.
func TestPageQueryScopePrefixMatchesTheScannerContract(t *testing.T) {
	t.Parallel()

	const repositoryID = "repository:r_1a2b3c4d"
	scopeID, err := scannercomponent.GoReachabilityScopeID(repositoryID)
	if err != nil {
		t.Fatalf("GoReachabilityScopeID() error = %v", err)
	}
	prefix := strings.TrimSuffix(scopeID, repositoryID)
	if want := "'" + prefix + "' || candidate.repository_id"; !strings.Contains(pageQuery, want) {
		t.Fatalf("pageQuery does not build the scope id as %q", want)
	}
}
