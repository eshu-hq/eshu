// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package code groups MCP route-selection packages for source-code analysis
// domains. It is a documentation namespace and owns no runtime behavior.
//
// Registration, global route fanout, dispatch, and telemetry stay in the
// parent mcp package; query execution stays in internal/query/codequery with
// analysis behind its deadcode leaf. The child packages here own only family
// membership and dependency-neutral request selection.
package code
