// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import "sync/atomic"

// goHTTPFrameworkSemanticsInvocationCount counts calls into
// goHTTPFrameworkSemantics. Test-only: production code never reads it. It
// exists so a regression test can assert this count stays at zero for a Go
// file whose imports contain none of net/http or a
// goRouteFrameworkConstructors framework package, proving the import gate at
// the language.go call site (issue #5219) actually skips the parent-lookup
// build and full-tree walk instead of merely happening to return no output.
// An atomic keeps this safe to read from a test while collector workers may
// still be parsing other files concurrently.
var goHTTPFrameworkSemanticsInvocationCount atomic.Int64

// ResetGoHTTPFrameworkSemanticsInvocationCountForTest zeroes
// goHTTPFrameworkSemanticsInvocationCount and returns its value beforehand.
// Test-only.
func ResetGoHTTPFrameworkSemanticsInvocationCountForTest() int64 {
	return goHTTPFrameworkSemanticsInvocationCount.Swap(0)
}

// GoHTTPFrameworkSemanticsInvocationCountForTest reads
// goHTTPFrameworkSemanticsInvocationCount without resetting it. Test-only.
func GoHTTPFrameworkSemanticsInvocationCountForTest() int64 {
	return goHTTPFrameworkSemanticsInvocationCount.Load()
}

// goFileImportsRouteFramework reports whether importAliases contains at least
// one alias for net/http or for a goRouteFrameworkConstructors framework
// import path (gin, echo, chi, fiber). goHTTPFrameworkSemantics can only ever
// produce output when one of these imports is present: net/http registration
// detection is gated on a net/http alias (goHTTPRegistrationBaseKnown), and
// every third-party route receiver is gated on a
// goRouteFrameworkConstructors import (goRouteFrameworkConstructor). Without
// any of them the walk is provably a 0/0 no-op, so language.go uses this to
// skip calling goHTTPFrameworkSemantics entirely (issue #5219) rather than
// paying for the parent-lookup build and full-tree walk on every file. The
// framework-path set is derived from goRouteFrameworkConstructors, the single
// source of truth also used by goRouteFrameworkConstructor, so the two lists
// cannot drift apart.
func goFileImportsRouteFramework(importAliases map[string][]string) bool {
	if len(goAliasesForImportPath(importAliases, "net/http")) > 0 {
		return true
	}
	for _, spec := range goRouteFrameworkConstructors {
		if len(goAliasesForImportPath(importAliases, spec.importPath)) > 0 {
			return true
		}
	}
	return false
}
