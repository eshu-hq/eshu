// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func assertQueryTestStringSliceEqual(t *testing.T, got any, want []string) {
	t.Helper()

	gotSlice, ok := got.([]any)
	if !ok {
		t.Fatalf("string slice type = %T, want []any", got)
	}
	if len(gotSlice) != len(want) {
		t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
	}
	for i, wantValue := range want {
		if gotValue, ok := gotSlice[i].(string); !ok || gotValue != wantValue {
			t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
		}
	}
}

// buildDeadCodeGraphCypher renders the dead-code candidate scan for the default
// Function label with an unscoped access filter, so the older shape tests can
// assert the Cypher skeleton without building a grant. It lives in a test file
// on purpose: an all-scopes filter is not something production code may hand
// the builder, because the scan's authorization seam IS the access argument
// that buildDeadCodeGraphCypherForLabel takes. Production callers reach the
// builder through scanDeadCodeCandidates, which derives access from the
// request. The GraphBackend argument is unused -- both backends render the same
// Cypher -- and stays only so the backend-matrix tests read naturally.
func buildDeadCodeGraphCypher(hasRepoID bool, _ GraphBackend) string {
	return buildDeadCodeGraphCypherForLabel(hasRepoID, "Function", "", repositoryAccessFilter{AllScopes: true})
}
