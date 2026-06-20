package main

import (
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// mountAskAndNarration wires the Ask Eshu endpoint and the governed narration
// posture onto mux. It must be called after router.Mount so the mux is fully
// assembled before the in-process MCP runner is wired.
//
// Construction order is intentional: the ask handler is built first so that
// AdapterReady() reflects whether provider.NewAdapter actually succeeded.
// The narration posture is then built from that readiness signal and injected
// into both the engine (via SetPosture) and the status handler
// (statusHandler.NarrationPosture). This ensures GET
// /api/v0/status/answer-narration and POST /api/v0/ask report consistent
// availability — a profile with an unset credential env var causes both to
// report unavailable rather than the status endpoint falsely reporting
// Available while the ask endpoint returns 503.
func mountAskAndNarration(
	getenv func(string) string,
	mux *http.ServeMux,
	inProcessHandler http.Handler,
	apiKey string,
	statusHandler *query.StatusHandler,
	logger *slog.Logger,
) {
	// Build the handler first. AdapterReady() is true only when every
	// construction step succeeded: profile found, provider.NewAdapter built,
	// engine built. A nil Asker (any failure) makes AdapterReady() false.
	//
	// inProcessHandler — NOT the bare mux — backs the engine's in-process MCP
	// runner. It is the scoped-auth-middleware-wrapped handler, so every inner
	// tool dispatch re-runs the scoped-route gate under the caller's token; a
	// scoped caller can therefore only reach routes that are themselves scope-
	// safe (see scopedHTTPRouteSupportsTenantFilter). The ask handler itself is
	// still mounted on mux below.
	ask := buildAskHandler(getenv, inProcessHandler, apiKey, logger)

	// Build the governed narration posture from real adapter readiness, not
	// merely profile presence. This is the invariant that fixes the consistency
	// bug: ProviderConfigured is true if and only if the adapter was built.
	posture := buildNarrationPosture(getenv, ask.AdapterReady())

	// Inject the posture into the engine so narration is governed at call time.
	ask.SetPosture(posture)

	// Inject the posture into the status handler so GET
	// /api/v0/status/answer-narration reflects the same gate state.
	if statusHandler != nil {
		statusHandler.NarrationPosture = posture
	}

	ask.Handler.Mount(mux)
}
