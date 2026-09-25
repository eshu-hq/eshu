// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ask serves POST /api/v0/ask: the natural-language answer endpoint
// with an optional Server-Sent Events stream.
//
// Handler requires a built engine (Asker); without one the route answers 503
// (default-off when ESHU_ASK_ENABLED is not "true"). Callers need the
// ask_search permission feature and data classes; others get 403. Responses
// are AnswerPackets with truth classification, substance and runtime
// guardrails, publish-safety screening, and SSE token deltas validated
// before emission.
//
// Capability and Support declare the family's capability row once:
// internal/query/contract registers Support() for production and main_test.go
// registers it for this package's own tests.
//
// The package moved out of the root query package for #6642. Root keeps
// AskHandler and the answer aliases in ask_alias.go for handler wiring, the
// impact shim, and the staying auth test until the #6642 alias sweep.
// Construction for cmd/api and cmd/mcp-server lives in internal/askwiring,
// which imports this package directly.
package ask
