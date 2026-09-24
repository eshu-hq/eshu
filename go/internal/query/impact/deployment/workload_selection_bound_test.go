// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestResolveWorkloadSelectorScopedNameReadBoundsGrantedRows pins the #6801
// review F-R5-1 fix: a scoped caller's name read carries the grant predicate
// on its WHERE line and binds the grant params, so the candidate bound counts
// granted rows only. An unscoped caller's read stays grant-free.
func TestResolveWorkloadSelectorScopedNameReadBoundsGrantedRows(t *testing.T) {
	t.Parallel()

	var scopedCypher, unscopedCypher string
	var scopedParams map[string]any
	capture := func(dst *string, params *map[string]any) querytestutil.FakeGraphReader {
		return querytestutil.FakeGraphReader{RunFn: func(_ context.Context, cypher string, p map[string]any) ([]map[string]any, error) {
			querytestutil.AssertCypherHasNoBrokenAndOr(t, cypher)
			if strings.Contains(cypher, "w.name = $service_name") {
				*dst = cypher
				if params != nil {
					*params = p
				}
			}
			return nil, nil
		}}
	}

	if _, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), capture(&scopedCypher, &scopedParams), "api", nil, nil); err != nil {
		t.Fatalf("scoped ResolveWorkloadSelector() error = %v", err)
	}
	if !strings.Contains(scopedCypher, "WHERE w.name = $service_name AND (w.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped name read = %q, want the grant predicate on the WHERE line", scopedCypher)
	}
	if _, ok := scopedParams["allowed_repository_ids"]; !ok {
		t.Fatalf("scoped name read params = %v, want allowed_repository_ids bound", scopedParams)
	}
	if _, ok := scopedParams["scope_grant_0"]; !ok {
		t.Fatalf("scoped name read params = %v, want scope_grant_0 bound for the DEFINES term", scopedParams)
	}

	if _, err := ResolveWorkloadSelector(context.Background(), capture(&unscopedCypher, nil), "api", nil, nil); err != nil {
		t.Fatalf("unscoped ResolveWorkloadSelector() error = %v", err)
	}
	if strings.Contains(unscopedCypher, "allowed_repository_ids") {
		t.Fatalf("unscoped name read = %q, want no grant predicate", unscopedCypher)
	}
}
