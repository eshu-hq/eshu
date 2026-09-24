// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the environment-compare alias must live in package query so the APIRouter field and the cmd/api and cmd/mcp-server wiring compile unchanged.

import "github.com/eshu-hq/eshu/go/internal/query/compare"

// compare_alias.go is the root alias shim for the environment-compare family,
// which moved to internal/query/compare (#6642). cmd/api and cmd/mcp-server
// build the handler through package query, and the APIRouter field names it,
// so this stays until the #6642 alias sweep.

// CompareHandler serves POST /api/v0/compare/environments. See
// compare.Handler.
type CompareHandler = compare.Handler
