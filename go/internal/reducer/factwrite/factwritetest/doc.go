// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factwritetest provides a fake [factwrite.Execer] ([FakeExecer])
// and a decoder ([DecodeBatchedFactCalls]) for the batched-insert calls
// [factwrite.BatchInsertFacts] issues against it, so a reducer family's
// writer test can assert on the rows a batched insert actually recorded
// without a live database.
//
// It is a regular (non-_test.go) package, not a _test.go file, because Go
// forbids importing another package's _test.go files: any family package
// under go/internal/reducer whose writer test wants this fixture must import
// it from here (issue #6061). Import it only from a _test.go file — it is
// test support, not a production dependency.
package factwritetest
