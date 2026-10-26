// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimedeclarationtools defines pure route selection and the tool
// definition for the MCP runtime declaration family (#8817).
//
// Route decides whether this package owns a tool and maps decoded arguments to
// a dependency-neutral internal request without executing it. The parent mcp
// package owns the root registration splice and client-visible order, global
// route fanout, the private adapter, HTTP dispatch, authorization, timeouts,
// response budgets, envelopes, summaries, and telemetry. The query package
// owns the bounded graph read behind the path
// (internal/query/runtime/declaration). This package runs no query and must
// keep the tool name and request path stable.
//
// The request body forwards the caller's arguments unselected, so the strict
// HTTP decoder rejects a misspelled or mistyped filter instead of the adapter
// dropping it.
package runtimedeclarationtools
