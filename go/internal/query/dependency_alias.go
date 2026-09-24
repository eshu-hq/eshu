// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the DependenciesHandler alias for the moved dependency family must live in package query so the APIRouter field and cmd/api's wiring compile unchanged.

import (
	"github.com/eshu-hq/eshu/go/internal/query/dependency"
)

// dependency_alias.go is the root alias shim for the dependency inventory
// family, which moved to internal/query/dependency (#6642). cmd/api builds the
// handler as query.DependenciesHandler and the APIRouter field names it, so the
// alias stays until the #6642 alias sweep repoints those callers.

// DependenciesHandler exposes the graph-backed package dependency inventory
// (GET /api/v0/dependencies). See dependency.Handler.
type DependenciesHandler = dependency.Handler
