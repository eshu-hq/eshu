// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querytestutil holds test helpers shared by internal/query and its
// handler-family subpackages.
//
// The helpers live in ordinary (non-_test.go) files on purpose. A symbol
// declared in a _test.go file is not part of the importable package, so no
// export, alias, or forwarder can reach it from another package's tests. That
// constraint is what forces this package to exist: as internal/query splits
// into handler-family subpackages (#6060), each family's tests need the same
// helpers, and they cannot reach root's _test.go declarations at all.
//
// This package is intended for tests only. No production file imports it today,
// and while that holds the linker drops it from production binaries — a
// consequence of the invariant, not a guarantee independent of it. A production
// import would pull testing into a shipped binary and should be treated as a
// defect rather than documented as behavior.
package querytestutil
