// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B3 root alias shim for #6060: type aliases for the moved repository family must live in package query so the APIRouter wiring and cmd constructors compile unchanged.

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// RepositoryHandler is the repository-handler family type. The
// implementation moved to internal/query/repository for #6060 (lane B B3);
// this alias keeps the APIRouter wiring, the cmd/api and cmd/mcp-server
// constructors, and every staying caller compiling unchanged.
type RepositoryHandler = repository.Handler

// CatalogWorkloadIdentityEntry is a repository read-model workload handle
// for the console catalog. Alias onto querycontract through the moved
// repository family; see RepositoryHandler.
type CatalogWorkloadIdentityEntry = repository.CatalogWorkloadIdentityEntry

// RepositoryRef is one source-backed repository branch or ref head. Alias
// onto querycontract; the read-model package and the staying OpenAPI
// components share it without importing each other.
type RepositoryRef = querycontract.RepositoryRef
