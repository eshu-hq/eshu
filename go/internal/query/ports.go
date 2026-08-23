// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

type (
	GraphQuery                     = querycontract.GraphQuery
	ContentStore                   = querycontract.ContentStore
	RepositoryContentCoverage      = querycontract.RepositoryContentCoverage
	RepositoryLanguageCount        = querycontract.RepositoryLanguageCount
	RepositoryEntityTypeCount      = querycontract.RepositoryEntityTypeCount
	RepositoryLanguageAggregate    = querycontract.RepositoryLanguageAggregate
	RepositoryLanguageRepository   = querycontract.RepositoryLanguageRepository
	RepositoryLanguageInventoryRow = querycontract.RepositoryLanguageInventoryRow
	RepositoryCatalogEntry         = querycontract.RepositoryCatalogEntry
)
