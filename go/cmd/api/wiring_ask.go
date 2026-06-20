package main

import (
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/askwiring"
)

// askHandlerResult bundles the HTTP handler with a setter for the governed
// narration posture. The setter is a no-op when the engine was not built
// (adapter or engine construction failed).
//
// It is a thin alias of [askwiring.HandlerResult] that keeps the cmd/api
// call-site API unchanged while delegating construction to the shared package.
type askHandlerResult = askwiring.HandlerResult

// buildAskHandler constructs an AskHandler for POST /api/v0/ask.
//
// Returns an askHandlerResult with a nil-Asker handler (default-off → 503
// unavailable) and a no-op SetPosture in any of the following cases:
//   - ESHU_ASK_ENABLED is not "true"
//   - No agent_reasoning provider profile is configured
//   - The provider adapter cannot be constructed (logged at WARN)
//   - The engine cannot be constructed (logged at WARN)
//
// When all conditions are met and the engine is built, returns a fully-wired
// handler whose SetPosture injects the governed posture into the engine.
// apiHandler must be the fully-mounted API mux; the engine's MCPRunner
// dispatches tool calls in-process through it.
func buildAskHandler(
	getenv func(string) string,
	apiHandler http.Handler,
	apiKey string,
	logger *slog.Logger,
) askHandlerResult {
	return askwiring.BuildAskHandler(getenv, apiHandler, apiKey, logger)
}
