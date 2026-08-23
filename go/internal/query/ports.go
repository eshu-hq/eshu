// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// GraphQuery is the concurrent-safe read-only graph traversal surface.
type GraphQuery = querycontract.GraphQuery

// ContentStore is the relational content-query surface used by read handlers.
type ContentStore = querycontract.ContentStore

// RepositoryContentCoverage summarizes content-store coverage for one repository.
type RepositoryContentCoverage = querycontract.RepositoryContentCoverage

// RepositoryLanguageCount captures one language bucket in repository coverage.
type RepositoryLanguageCount = querycontract.RepositoryLanguageCount

// RepositoryEntityTypeCount captures one entity-type coverage bucket.
type RepositoryEntityTypeCount = querycontract.RepositoryEntityTypeCount

// RepositoryLanguageAggregate captures corpus-level language coverage counts.
type RepositoryLanguageAggregate = querycontract.RepositoryLanguageAggregate

// RepositoryLanguageRepository captures one repository matched by language.
type RepositoryLanguageRepository = querycontract.RepositoryLanguageRepository

// RepositoryLanguageInventoryRow captures one language bucket across repositories.
type RepositoryLanguageInventoryRow = querycontract.RepositoryLanguageInventoryRow

// RepositoryCatalogEntry is the relational repository catalog row.
type RepositoryCatalogEntry = querycontract.RepositoryCatalogEntry
