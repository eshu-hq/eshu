package main

import (
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/askwiring"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// mountAskAndNarration wires the Ask Eshu endpoint and the governed narration
// posture onto mux. It must be called after the query router and all other
// routes are mounted so the in-process mux is fully assembled before the MCP
// runner is wired.
//
// The MCP server uses its own in-process mux as the MCPRunner handler so that
// the engine's tool calls dispatch through the MCP server's own routes — the
// same way cmd/api wires the engine through the API mux.
//
// Construction order is intentional: the ask handler is built first so that
// AdapterReady() reflects whether provider.NewAdapter actually succeeded.
// The narration posture is then built from that readiness signal and injected
// into both the engine (via SetPosture) and the status handler
// (statusHandler.NarrationPosture). This ensures GET
// /api/v0/status/answer-narration and POST /api/v0/ask report consistent
// availability on the MCP server — matching cmd/api semantics exactly.
//
// SHARED-TOKEN-ONLY: POST /api/v0/ask is not in the scoped-HTTP-route allow-
// list (scopedHTTPRouteSupportsTenantFilter), so the MCP "ask" tool only works
// for callers authenticated with a shared API key or in auth-disabled local
// mode. Scoped-bearer-token callers receive 403. Scoped-token support for Ask
// (including inner-call route gating to prevent cross-tenant data leakage) is
// tracked in issue #3300 / PR #3310 and must not be added here.
func mountAskAndNarration(
	getenv func(string) string,
	mux *http.ServeMux,
	apiKey string,
	statusHandler *query.StatusHandler,
	logger *slog.Logger,
) {
	// Build the handler first. AdapterReady() is true only when every
	// construction step succeeded: profile found, provider.NewAdapter built,
	// engine built. A nil Asker (any failure) keeps the handler default-off.
	ask := askwiring.BuildAskHandler(getenv, mux, apiKey, logger)

	// Build the governed narration posture from real adapter readiness, not
	// merely profile presence.
	posture := askwiring.BuildNarrationPosture(getenv, ask.AdapterReady())

	// Inject the posture into the engine so narration is governed at call time.
	ask.SetPosture(posture)

	// Inject the posture into the status handler so GET
	// /api/v0/status/answer-narration reflects the same gate state.
	if statusHandler != nil {
		statusHandler.NarrationPosture = posture
	}

	ask.Handler.Mount(mux)
}
