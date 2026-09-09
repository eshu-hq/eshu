// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

// StringSliceContains reports whether values contains want.
//
// It lives here rather than in either package's _test.go files because the
// #6060 code-family split left both root internal/query and
// internal/query/codequery asserting against it, and a helper declared in one
// package's test files is unreachable from the other. Duplicating it was the
// alternative and it would drift. Same rationale as CodeGrantScopedAuthContext
// (codegrantauthcontext.go).
func StringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// EqualStringSlices reports whether got and want hold the same strings in the
// same order. Shared across the root/codequery split for the reason given on
// StringSliceContains.
func EqualStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
