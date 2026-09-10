// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// CodeHandler is the code-handler family type. The implementation moved to
// internal/query/codequery for #6060 (lane A); this alias keeps the
// APIRouter wiring, the cmd/api and cmd/mcp-server constructors, and every
// staying caller compiling unchanged.
type CodeHandler = codequery.CodeHandler
