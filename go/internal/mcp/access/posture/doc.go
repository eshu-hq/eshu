// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package secretsiamtools defines pure route selection for the MCP
// secrets/IAM posture family.
//
// Route decides which of the five secrets/IAM tools this package owns and maps
// decoded arguments to a dependency-neutral internal request without executing
// it. The parent mcp package owns tool registration and its order, global
// route fanout, the private adapter, HTTP dispatch, authorization, timeouts,
// response budgets, envelopes, summaries, and telemetry. The query package
// owns the bounded, scope-anchored reads behind these paths. This package runs
// no query and must keep the tool names, request paths, and query keys stable.
//
// The four listings and the summary are deliberately asymmetric. Each listing
// pages, so it carries limit — defaulting to 50 — alongside its own cursor and
// filter keys. count_secrets_iam_posture aggregates a whole scope, so it
// carries scope_id and nothing else: there is no page to size and nothing to
// seek past, and a limit forwarded here would be inert (the handler never
// reads one), so the key would only advertise a bound the endpoint does not
// honor.
package secretsiamtools
