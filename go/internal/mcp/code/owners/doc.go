// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeownerstools defines pure route selection for the MCP CODEOWNERS
// ownership family.
//
// Route decides whether this package owns a tool and maps decoded arguments to
// a dependency-neutral internal request without executing it. The parent mcp
// package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded read
// behind the path, including effective_owner precedence between a service
// manifest and the CODEOWNERS file. This package runs no query and must keep
// the tool name, request path, and query keys stable.
//
// The one coercion this family does not share with the rest of the dispatcher
// is after_order_index, the numeric leg of the three-part keyset cursor the
// listing pages with. It is formatted only when the caller sent the key, and
// stays the empty string otherwise: the handler admits the cursor only when
// after_order_index, after_pattern, and after_ref all arrive, so defaulting an
// absent leg to "0" would hand it a half-supplied cursor on a first page.
package codeownerstools
