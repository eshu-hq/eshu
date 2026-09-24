// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package content holds the shared content-read test doubles for
// internal/query and its handler-family subpackages.
//
// It is the content half of querytestutil, nested under it for #6642. The
// parent's contract applies unchanged: every helper lives in an ordinary
// non-test file so other packages' tests can import it, nothing here is
// production behavior, and no non-test file under internal/query may import
// this package. internal/queryplan's callsite inventory enforces that last rule
// for this leaf the same way it does for the parent.
//
// The store doubles live here:
//
//   - FakePortContentStore answers querycontract.ContentStore plus the narrow
//     optional ports package query type-asserts a store against. Its entity
//     reads filter before they limit, matching the production SQL.
//     FakeDeadCodeContentStore, ResolvingEntityContentStore and
//     PatternConsumerSearchContentStore narrow or override it for one handler
//     family each. SortEntityContentByLocation and FilterLanguageRepos are the
//     ordering and grant predicates it shares with those doubles.
//
// The fake database/sql driver (OpenReaderTestDB, ReaderQueryResult, the
// Reader*Columns helpers, ReaderQueryContainsInOrder and ReaderCheckArgs)
// peeled out to internal/testutil/contentreader for #6818 move 5; see its
// doc.go for that contract.
package content
