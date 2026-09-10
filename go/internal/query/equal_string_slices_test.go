// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// equalStringSlices reports whether got and want hold the same strings in the
// same order. It is duplicated in package codequery
// (code_dead_code_contract_test.go): the #6060 CodeHandler move split its
// callers across both packages, and a _test.go symbol is not importable
// across a package boundary, so each side keeps its own copy rather than one
// package reaching into the other's test files.
